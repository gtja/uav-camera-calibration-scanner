package dbapi

import (
	"encoding/json"
	"testing"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
)

func TestSampleFromRowParsesNestedDeviceProperties(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	client := New(config.DBAPIConfig{}, loc)
	properties := map[string]any{
		"34010000006011180900": map[string]any{
			"cameraMode":      "0",
			"gimbalType":      "M30T_CAMERA",
			"gimbalPitch":     -60.2,
			"lensType":        "wide",
			"flightStatus":    "inFlight",
			"taskStatus":      "standby",
			"height":          80.5,
			"flyTime":         429911,
			"horizontalSpeed": "0.2",
			"latitude":        30.272959296840085,
			"longitude":       119.95987771375943,
			"liveStatus": []any{
				map[string]any{"video_id": "live"},
			},
		},
	}
	raw, err := json.Marshal(properties)
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{
		"deviceId":               "34010000006011180900",
		"acquireTimestampFormat": "2026-07-06 16:36:31",
		"properties":             string(raw),
	}

	sample, ok := client.sampleFromRow("34010000006011180900", row)
	if !ok {
		t.Fatal("expected sample")
	}
	if sample.CameraMode != "0" || sample.LensType != "wide" || sample.FlightStatus != "inFlight" {
		t.Fatalf("unexpected sample: %+v", sample)
	}
	if sample.GimbalType != "M30T_CAMERA" {
		t.Fatalf("unexpected gimbal type: %q", sample.GimbalType)
	}
	if sample.GimbalPitch != -60.2 {
		t.Fatalf("unexpected pitch: %f", sample.GimbalPitch)
	}
	if sample.Height != 80.5 || sample.FlyTime != 429911 || sample.HorizontalSpeed != 0.2 {
		t.Fatalf("unexpected in-air fields: %+v", sample)
	}
	if !sample.HasPosition || sample.Latitude != 30.272959296840085 || sample.Longitude != 119.95987771375943 {
		t.Fatalf("unexpected position fields: %+v", sample)
	}
	if sample.VideoID != "live" {
		t.Fatalf("unexpected video id: %q", sample.VideoID)
	}
}
