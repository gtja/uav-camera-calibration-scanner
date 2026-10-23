package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/model"
	"jingansi/uav-camera-calibration-scanner/internal/result"
)

func TestClipEndUsesVODClipDuration(t *testing.T) {
	start := time.Date(2026, 7, 7, 16, 0, 30, 0, time.UTC)
	runner := Runner{cfg: config.Config{VOD: config.VODConfig{ClipDurationSeconds: 40}}}
	window := model.Window{
		Start: start,
		End:   start.Add(20 * time.Second),
	}

	got := runner.clipEnd(window)
	want := start.Add(40 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("unexpected clip end: got %s want %s", got, want)
	}
}

func TestClipEndNeverShortensRuleWindow(t *testing.T) {
	start := time.Date(2026, 7, 7, 16, 0, 30, 0, time.UTC)
	runner := Runner{cfg: config.Config{VOD: config.VODConfig{ClipDurationSeconds: 10}}}
	window := model.Window{
		Start: start,
		End:   start.Add(20 * time.Second),
	}

	got := runner.clipEnd(window)
	if !got.Equal(window.End) {
		t.Fatalf("clip end should not shorten rule window: got %s want %s", got, window.End)
	}
}

func TestLoadTargetDevicesKeepsCandidatesWhenGimbalTypeFilterEnabled(t *testing.T) {
	runner := Runner{cfg: config.Config{
		TargetModels: []string{"M30T"},
		Rule:         config.RuleConfig{TargetGimbalTypes: []string{"S220_CAMERA"}},
		Devices: []model.Device{{
			DeviceID: "device-1",
			Model:    "UNKNOWN",
		}},
	}}

	devices, err := runner.loadTargetDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DeviceID != "device-1" {
		t.Fatalf("unexpected devices: %+v", devices)
	}
}

func TestLoadTargetDevicesPrecisePrefilterUsesControlCenterSignals(t *testing.T) {
	runner := Runner{cfg: config.Config{
		TargetModels: []string{"M3TD", "M4TD", "MATRICE_4T"},
		Scan:         config.ScanConfig{PreciseDevicePrefilter: true},
		Rule:         config.RuleConfig{TargetGimbalTypes: []string{"M3TD_CAMERA", "M4TD Camera", "MATRICE_4T"}},
		Devices: []model.Device{
			{
				DeviceID:   "m3td-by-name",
				DeviceName: "机场2-M3TD",
			},
			{
				DeviceID: "m4td-by-gimbal",
				Tags:     map[string]string{"GIMBAL_TYPE": "M4TD Camera"},
			},
			{
				DeviceID:   "m4td-by-matrice-name",
				DeviceName: "大疆Matrice 4T-电信",
			},
			{
				DeviceID: "s220",
				Tags:     map[string]string{"GIMBAL_TYPE": "S220_CAMERA"},
			},
			{
				DeviceID:   "unknown",
				DeviceName: "普通测试设备",
			},
		},
	}}

	devices, err := runner.loadTargetDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(devices))
	for _, device := range devices {
		got = append(got, device.DeviceID)
	}
	want := []string{"m3td-by-name", "m4td-by-gimbal", "m4td-by-matrice-name"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected devices: got %v want %v", got, want)
	}
}

type recentCalibrationSink struct {
	recent result.RecentCalibration
	found  bool
	saved  []model.ScanResult
}

func (s *recentCalibrationSink) Save(_ context.Context, record model.ScanResult) error {
	s.saved = append(s.saved, record)
	return nil
}

func (s *recentCalibrationSink) RecentCalibration(_ context.Context, _ string, _ time.Time) (result.RecentCalibration, bool, error) {
	return s.recent, s.found, nil
}

func TestRunOnceSkipsRecentlyCalibratedDeviceBeforeScanning(t *testing.T) {
	cfg := config.Default()
	cfg.TargetModels = []string{"M30T"}
	cfg.Calibration.Enabled = true
	cfg.Calibration.DedupEnabled = true
	cfg.Calibration.DedupDays = 7
	cfg.DBAPI.DeviceListPath = ""
	cfg.Devices = []model.Device{{
		DeviceID: "device-1",
		Model:    "M30T",
	}}
	sink := &recentCalibrationSink{
		found: true,
		recent: result.RecentCalibration{
			DeviceID:          "device-1",
			ClipStart:         time.Date(2026, 7, 8, 14, 0, 0, 0, time.UTC),
			CalibrationTaskID: "task-1",
			CalibrationStatus: "SUBMITTED",
			CreatedAt:         time.Now(),
		},
	}
	runner, err := NewRunner(cfg, sink)
	if err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(context.Background(), "2026-07-08"); err != nil {
		t.Fatal(err)
	}
	if len(sink.saved) != 1 {
		t.Fatalf("saved records = %d, want 1", len(sink.saved))
	}
	record := sink.saved[0]
	if record.Status != model.StatusSkippedRecentCalibration {
		t.Fatalf("status = %s, want %s", record.Status, model.StatusSkippedRecentCalibration)
	}
	if !strings.Contains(record.ErrorMessage, "task=task-1") {
		t.Fatalf("unexpected skip message: %q", record.ErrorMessage)
	}
}

func TestCalibrationParametersUsesScopedDeviceProperties(t *testing.T) {
	raw := []byte(`{"device-1":{"cameraSn":"CAM1","sn":"SN1","gimbalType":"M30T_CAMERA","cameraMode":"0"},"other":{"cameraMode":"1"}}`)

	got, err := calibrationParameters(raw, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if got["cameraSn"] != "CAM1" || got["gimbalType"] != "M30T_CAMERA" || got["cameraMode"] != "0" {
		t.Fatalf("unexpected calibration parameters: %+v", got)
	}
}

func TestQuerySpansUseChunksWithRuleOverlap(t *testing.T) {
	start := time.Date(2026, 7, 7, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	runner := Runner{cfg: config.Config{
		Scan: config.ScanConfig{QueryChunkMinutes: 30},
		Rule: config.RuleConfig{DurationSeconds: 20, MaxSampleGapSeconds: 3},
	}}

	spans := runner.querySpans(start, end)
	if len(spans) != 2 {
		t.Fatalf("expected two spans, got %d", len(spans))
	}
	if !spans[0].QueryStart.Equal(start) || !spans[0].AcceptStart.Equal(start) {
		t.Fatalf("unexpected first span start: %+v", spans[0])
	}
	if !spans[0].AcceptEnd.Equal(start.Add(30 * time.Minute)) {
		t.Fatalf("unexpected first accept end: %s", spans[0].AcceptEnd)
	}
	if !spans[0].QueryEnd.Equal(start.Add(30*time.Minute + 23*time.Second)) {
		t.Fatalf("unexpected first query end: %s", spans[0].QueryEnd)
	}
	if !spans[1].QueryStart.Equal(start.Add(30*time.Minute)) || !spans[1].QueryEnd.Equal(end) {
		t.Fatalf("unexpected second span: %+v", spans[1])
	}
}
