package artifact

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/model"
)

func TestWritePropertiesAddsFlightParamsWithRawCameraMode(t *testing.T) {
	store := NewLocalStore(config.ArtifactConfig{LocalDir: t.TempDir()})
	start := time.Date(2026, 7, 7, 16, 0, 32, 0, time.Local)
	raw := json.RawMessage(`{"sn":"SN123","gimbalType":"M30T_CAMERA","cameraMode":"0"}`)

	path, err := store.WriteProperties(
		context.Background(),
		model.Device{DeviceID: "device-1", ProductKey: "product-1"},
		model.Window{
			Start:       start,
			End:         start.Add(20 * time.Second),
			FirstSample: model.Sample{VideoID: "live"},
		},
		model.PropertySnapshot{DeviceID: "device-1", SnapshotAt: start, Properties: raw, Source: "test"},
		"downward-photo-v2",
	)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope propertyEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.FlightParams.SN != "SN123" {
		t.Fatalf("unexpected sn: %+v", envelope.FlightParams)
	}
	if envelope.FlightParams.GimbalType != "M30T_CAMERA" {
		t.Fatalf("unexpected gimbalType: %+v", envelope.FlightParams)
	}
	if envelope.FlightParams.CameraMode != "0" {
		t.Fatalf("unexpected cameraMode: %+v", envelope.FlightParams)
	}
}

func TestDeriveFlightParamsKeepsNestedRawCameraMode(t *testing.T) {
	raw := json.RawMessage(`{"device-1":{"cameraSn":"CAM123","gimbalType":"M30T Camera","cameraMode":1}}`)

	params, err := deriveFlightParams(raw, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if params.CameraSn != "CAM123" || params.CameraMode != float64(1) {
		t.Fatalf("unexpected params: %+v", params)
	}
}

func TestDeriveFlightParamsRequiresCameraSerial(t *testing.T) {
	raw := json.RawMessage(`{"gimbalType":"M30T Camera","cameraMode":"0"}`)

	if _, err := deriveFlightParams(raw, "device-1"); err == nil {
		t.Fatal("expected missing serial error")
	}
}
