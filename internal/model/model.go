package model

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	StatusSuccess                  = "SUCCESS"
	StatusNoMatch                  = "NO_MATCH"
	StatusFailed                   = "FAILED_RETRYABLE"
	StatusNoTelemetry              = "NO_TELEMETRY"
	StatusSkippedRecentCalibration = "SKIPPED_RECENT_CALIBRATION"
)

type Device struct {
	ProductKey   string            `json:"productKey,omitempty" yaml:"productKey,omitempty"`
	DeviceID     string            `json:"deviceId" yaml:"deviceId"`
	DeviceName   string            `json:"deviceName,omitempty" yaml:"deviceName,omitempty"`
	SN           string            `json:"sn,omitempty" yaml:"sn,omitempty"`
	Model        string            `json:"model,omitempty" yaml:"model,omitempty"`
	Tags         map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
	VideoID      string            `json:"videoId,omitempty" yaml:"videoId,omitempty"`
	PayloadIndex string            `json:"payloadIndex,omitempty" yaml:"payloadIndex,omitempty"`
}

func (d Device) TargetModel(targets map[string]struct{}) (string, bool) {
	if value, ok := normalizeTarget(d.Model, targets); ok {
		return value, true
	}
	for _, value := range d.Tags {
		if normalized, ok := normalizeTarget(value, targets); ok {
			return normalized, true
		}
	}
	return "", false
}

func normalizeTarget(value string, targets map[string]struct{}) (string, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return "", false
	}
	_, ok := targets[normalized]
	return normalized, ok
}

type Sample struct {
	DeviceID        string          `json:"deviceId"`
	SampleTime      time.Time       `json:"sampleTime"`
	CameraMode      string          `json:"cameraMode"`
	GimbalType      string          `json:"gimbalType,omitempty"`
	GimbalPitch     float64         `json:"gimbalPitch"`
	LensType        string          `json:"lensType"`
	FlightStatus    string          `json:"flightStatus,omitempty"`
	TaskStatus      string          `json:"taskStatus,omitempty"`
	Height          float64         `json:"height,omitempty"`
	FlyTime         int64           `json:"flyTime,omitempty"`
	HorizontalSpeed float64         `json:"horizontalSpeed,omitempty"`
	Latitude        float64         `json:"latitude,omitempty"`
	Longitude       float64         `json:"longitude,omitempty"`
	HasPosition     bool            `json:"hasPosition,omitempty"`
	VideoID         string          `json:"videoId,omitempty"`
	PayloadIndex    string          `json:"payloadIndex,omitempty"`
	RawProperties   json.RawMessage `json:"rawProperties,omitempty"`
}

type Window struct {
	Start       time.Time              `json:"start"`
	End         time.Time              `json:"end"`
	DeviceID    string                 `json:"deviceId"`
	SampleCount int                    `json:"sampleCount"`
	FirstSample Sample                 `json:"firstSample"`
	LastSample  Sample                 `json:"lastSample"`
	Summary     map[string]interface{} `json:"summary"`
}

type PropertySnapshot struct {
	DeviceID   string          `json:"deviceId"`
	SnapshotAt time.Time       `json:"snapshotAt"`
	Properties json.RawMessage `json:"properties"`
	Source     string          `json:"source"`
}

type ScanResult struct {
	ScanDate                   string                 `json:"scanDate"`
	ProductKey                 string                 `json:"productKey,omitempty"`
	DeviceID                   string                 `json:"deviceId"`
	SN                         string                 `json:"sn,omitempty"`
	DeviceModel                string                 `json:"deviceModel,omitempty"`
	VideoID                    string                 `json:"videoId,omitempty"`
	PayloadIndex               string                 `json:"payloadIndex,omitempty"`
	ClipStart                  time.Time              `json:"clipStart"`
	ClipEnd                    time.Time              `json:"clipEnd"`
	ClipURL                    string                 `json:"clipUrl,omitempty"`
	CalibrationTaskID          string                 `json:"calibrationTaskId,omitempty"`
	CalibrationStatus          string                 `json:"calibrationStatus,omitempty"`
	CalibrationCameraSN        string                 `json:"calibrationCameraSn,omitempty"`
	CalibrationCameraMode      string                 `json:"calibrationCameraMode,omitempty"`
	CalibrationError           string                 `json:"calibrationError,omitempty"`
	ControlCenterPropertiesURL string                 `json:"controlCenterPropertiesUrl,omitempty"`
	PropertySnapshotTime       time.Time              `json:"propertySnapshotTime,omitempty"`
	ConditionSummary           map[string]interface{} `json:"conditionSummary,omitempty"`
	RuleVersion                string                 `json:"ruleVersion"`
	Status                     string                 `json:"status"`
	ErrorMessage               string                 `json:"errorMessage,omitempty"`
	CreatedAt                  time.Time              `json:"createdAt"`
	UpdatedAt                  time.Time              `json:"updatedAt"`
}
