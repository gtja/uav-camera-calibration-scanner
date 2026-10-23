package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/model"
)

type LocalStore struct {
	cfg config.ArtifactConfig
}

type propertyEnvelope struct {
	Meta         map[string]any  `json:"meta"`
	FlightParams flightParams    `json:"flightParams"`
	Properties   json.RawMessage `json:"properties"`
}

type flightParams struct {
	CameraSn   string `json:"cameraSn,omitempty"`
	SN         string `json:"sn,omitempty"`
	GimbalType string `json:"gimbalType"`
	CameraMode any    `json:"cameraMode"`
}

func NewLocalStore(cfg config.ArtifactConfig) *LocalStore {
	return &LocalStore{cfg: cfg}
}

func (s *LocalStore) WriteProperties(ctx context.Context, device model.Device, window model.Window, snapshot model.PropertySnapshot, ruleVersion string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	day := window.Start.Format("20060102")
	begin := window.Start.Format("150405")
	end := window.End.Format("150405")
	rel := filepath.Join("uav-downward-photo", day, device.DeviceID, fmt.Sprintf("%s-%s", begin, end), "control-center-properties.json")
	abs := filepath.Join(s.cfg.LocalDir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	params, err := deriveFlightParams(snapshot.Properties, device.DeviceID)
	if err != nil {
		return "", err
	}
	envelope := propertyEnvelope{
		Meta: map[string]any{
			"deviceId":     device.DeviceID,
			"productKey":   device.ProductKey,
			"videoId":      firstNonEmpty(window.FirstSample.VideoID, device.VideoID),
			"payloadIndex": firstNonEmpty(window.FirstSample.PayloadIndex, device.PayloadIndex),
			"clipStart":    window.Start.Format(time.RFC3339Nano),
			"clipEnd":      window.End.Format(time.RFC3339Nano),
			"snapshotAt":   snapshot.SnapshotAt.Format(time.RFC3339Nano),
			"source":       snapshot.Source,
			"ruleVersion":  ruleVersion,
		},
		FlightParams: params,
		Properties:   snapshot.Properties,
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", err
	}
	if strings.TrimSpace(s.cfg.URLPrefix) != "" {
		return strings.TrimRight(s.cfg.URLPrefix, "/") + "/" + filepath.ToSlash(rel), nil
	}
	return abs, nil
}

func deriveFlightParams(raw json.RawMessage, deviceID string) (flightParams, error) {
	props, err := propertyObject(raw, deviceID)
	if err != nil {
		return flightParams{}, err
	}
	params := flightParams{
		CameraSn:   firstJSONString(props, "cameraSn", "cameraSN", "camera_sn"),
		SN:         firstJSONString(props, "sn", "SN"),
		GimbalType: firstJSONString(props, "gimbalType", "gimbal_type"),
	}
	rawMode, ok := firstJSONValue(props, "cameraMode", "camera_mode")
	if !ok {
		return flightParams{}, fmt.Errorf("missing cameraMode")
	}
	params.CameraMode = rawMode
	if params.CameraSn == "" && params.SN == "" {
		return flightParams{}, fmt.Errorf("missing cameraSn or sn")
	}
	if params.GimbalType == "" {
		return flightParams{}, fmt.Errorf("missing gimbalType")
	}
	return params, nil
}

func propertyObject(raw json.RawMessage, deviceID string) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("decode properties: %w", err)
	}
	if scoped, ok := root[deviceID]; ok {
		if obj, ok := scoped.(map[string]any); ok {
			return obj, nil
		}
		data, err := json.Marshal(scoped)
		if err != nil {
			return nil, err
		}
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err == nil {
			return obj, nil
		}
	}
	return root, nil
}

func firstJSONString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func firstJSONValue(row map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			continue
		}
		return value, true
	}
	return nil, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
