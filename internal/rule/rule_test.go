package rule

import (
	"testing"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/model"
)

func TestFindWindowsReturnsEarliestContinuousWindow(t *testing.T) {
	start := time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC)
	var samples []model.Sample
	for i := 0; i <= 24; i += 2 {
		sample := validSample(start.Add(time.Duration(i) * time.Second))
		sample.Longitude += float64(i) * 0.00006
		samples = append(samples, sample)
	}

	windows := FindWindows(samples, testConfig())
	if len(windows) == 0 {
		t.Fatal("expected at least one window")
	}
	if !windows[0].Start.Equal(start) {
		t.Fatalf("unexpected start: %s", windows[0].Start)
	}
	if !windows[0].End.Equal(start.Add(20 * time.Second)) {
		t.Fatalf("unexpected end: %s", windows[0].End)
	}
}

func TestFindWindowsBreaksOnLargeGap(t *testing.T) {
	start := time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC)
	samples := []model.Sample{
		movingSample(start, 0),
		movingSample(start.Add(2*time.Second), 2),
		movingSample(start.Add(8*time.Second), 8),
		movingSample(start.Add(10*time.Second), 10),
		movingSample(start.Add(12*time.Second), 12),
	}

	windows := FindWindows(samples, testConfig())
	if len(windows) != 0 {
		t.Fatalf("expected no window across a large gap, got %d", len(windows))
	}
}

func TestFindWindowsRejectsHovering(t *testing.T) {
	start := time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC)
	var samples []model.Sample
	for i := 0; i <= 24; i += 2 {
		samples = append(samples, validSample(start.Add(time.Duration(i)*time.Second)))
	}

	windows := FindWindows(samples, testConfig())
	if len(windows) != 0 {
		t.Fatalf("expected no window for hovering samples, got %d", len(windows))
	}
}

func TestFindWindowsRejectsMissingPosition(t *testing.T) {
	start := time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC)
	var samples []model.Sample
	for i := 0; i <= 24; i += 2 {
		sample := movingSample(start.Add(time.Duration(i)*time.Second), i)
		sample.HasPosition = false
		samples = append(samples, sample)
	}

	windows := FindWindows(samples, testConfig())
	if len(windows) != 0 {
		t.Fatalf("expected no window without position samples, got %d", len(windows))
	}
}

func TestFindWindowsCanSkipEarlyHoveringInsideSegment(t *testing.T) {
	start := time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC)
	var samples []model.Sample
	for i := 0; i <= 42; i += 2 {
		sample := validSample(start.Add(time.Duration(i) * time.Second))
		if i > 20 {
			sample.Longitude += float64(i-20) * 0.00012
		}
		samples = append(samples, sample)
	}

	windows := FindWindows(samples, testConfig())
	if len(windows) == 0 {
		t.Fatal("expected a later moving window")
	}
	want := start.Add(10 * time.Second)
	if !windows[0].Start.Equal(want) {
		t.Fatalf("unexpected first moving window: got %s want %s", windows[0].Start, want)
	}
}

func TestFlightStatusIsIgnoredWhenInAir(t *testing.T) {
	sample := validSample(time.Now())
	sample.FlightStatus = "standby"
	if !Valid(sample, testConfig()) {
		t.Fatal("standby flightStatus should be ignored when height and flyTime prove the UAV is in the air")
	}

	sample.FlightStatus = ""
	if !Valid(sample, testConfig()) {
		t.Fatal("missing flightStatus should be ignored when height and flyTime prove the UAV is in the air")
	}
}

func TestBelowMinHeightDoesNotPass(t *testing.T) {
	sample := validSample(time.Now())
	sample.Height = 4.9
	if Valid(sample, testConfig()) {
		t.Fatal("sample below min height should not be valid")
	}
}

func TestMissingFlyTimeDoesNotPass(t *testing.T) {
	sample := validSample(time.Now())
	sample.FlyTime = 0
	if Valid(sample, testConfig()) {
		t.Fatal("sample without flyTime should not be valid")
	}
}

func TestLegacyFlightStatusGateCanStillBeEnabled(t *testing.T) {
	sample := validSample(time.Now())
	sample.FlightStatus = "standby"
	cfg := testConfig()
	cfg.RequireFlightStatus = true
	if Valid(sample, cfg) {
		t.Fatal("standby sample should not be valid when legacy flightStatus gate is enabled")
	}
}

func TestTargetGimbalTypesGateSamples(t *testing.T) {
	sample := validSample(time.Now())
	sample.GimbalType = "M30T Camera"
	cfg := testConfig()
	cfg.TargetGimbalTypes = map[string]struct{}{"M30T_CAMERA": {}}
	if !Valid(sample, cfg) {
		t.Fatal("gimbal type variant should match normalized target")
	}

	sample.GimbalType = "S220_CAMERA"
	if Valid(sample, cfg) {
		t.Fatal("sample with non-target gimbalType should not be valid")
	}
}

func validSample(ts time.Time) model.Sample {
	return model.Sample{
		DeviceID:     "device-1",
		SampleTime:   ts,
		CameraMode:   "0",
		GimbalType:   "M30T_CAMERA",
		GimbalPitch:  -60,
		LensType:     "wide",
		FlightStatus: "inFlight",
		Height:       80,
		FlyTime:      100,
		Latitude:     30,
		Longitude:    120,
		HasPosition:  true,
	}
}

func movingSample(ts time.Time, seconds int) model.Sample {
	sample := validSample(ts)
	sample.Longitude += float64(seconds) * 0.00006
	return sample
}

func testConfig() Config {
	return Config{
		Duration:            20 * time.Second,
		MaxSampleGap:        3 * time.Second,
		CameraMode:          "0",
		LensType:            "wide",
		GimbalPitchMin:      -90,
		GimbalPitchMax:      -10,
		RequireFlightStatus: false,
		MinHeightMeters:     5,
		RequireFlyTime:      true,
		MinTravelMeters:     100,
	}
}
