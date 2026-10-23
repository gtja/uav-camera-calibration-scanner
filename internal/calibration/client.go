package calibration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
)

type Client struct {
	cfg  config.CalibrationConfig
	http *http.Client
}

type CreateRequest struct {
	VideoURL      string         `json:"video_url,omitempty"`
	UAVParameters map[string]any `json:"uav_parameters"`
	FPS           float64        `json:"fps,omitempty"`
	CallbackURL   string         `json:"callback_url,omitempty"`
}

type Task struct {
	TaskID     string `json:"taskId,omitempty"`
	CameraSN   string `json:"cameraSn,omitempty"`
	GimbalType string `json:"gimbalType,omitempty"`
	CameraMode string `json:"cameraMode,omitempty"`
	Status     string `json:"status,omitempty"`
}

func New(cfg config.CalibrationConfig) *Client {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: timeout}}
}

func (c *Client) Enabled() bool {
	return c != nil && c.cfg.Enabled
}

func (c *Client) CreateTask(ctx context.Context, clipURL string, params map[string]any) (Task, error) {
	if !c.Enabled() {
		return Task{}, nil
	}
	if strings.TrimSpace(c.cfg.BaseURL) == "" {
		return Task{}, errors.New("calibration baseUrl is empty")
	}
	if strings.TrimSpace(clipURL) == "" {
		return Task{}, errors.New("clipUrl is empty")
	}
	if len(params) == 0 {
		return Task{}, errors.New("uav_parameters is empty")
	}
	endpoint, err := joinURL(c.cfg.BaseURL, c.cfg.CreatePath)
	if err != nil {
		return Task{}, err
	}
	reqBody := CreateRequest{
		VideoURL:      clipURL,
		UAVParameters: params,
		FPS:           c.cfg.FPS,
		CallbackURL:   c.cfg.CallbackURL,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return Task{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Task{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Task{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Task{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Task{}, fmt.Errorf("calibration create returned %s: %s", resp.Status, string(data))
	}
	task, err := parseCreateResponse(data)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func parseCreateResponse(data []byte) (Task, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return Task{}, fmt.Errorf("decode calibration response: %w", err)
	}
	if code, ok := root["code"]; ok && fmt.Sprint(code) != "200" {
		return Task{}, fmt.Errorf("calibration create failed: %s", string(data))
	}
	dataObj, ok := root["data"].(map[string]any)
	if !ok {
		return Task{}, fmt.Errorf("calibration response has no data object: %s", string(data))
	}
	task := Task{
		TaskID:     firstString(dataObj, "task_id", "taskId"),
		CameraSN:   firstString(dataObj, "camera_sn", "cameraSn"),
		GimbalType: firstString(dataObj, "gimbal_type", "gimbalType"),
		CameraMode: firstString(dataObj, "camera_mode", "cameraMode"),
		Status:     firstString(dataObj, "status"),
	}
	if task.TaskID == "" {
		return Task{}, fmt.Errorf("calibration response has no task_id: %s", string(data))
	}
	return task, nil
}

func firstString(row map[string]any, keys ...string) string {
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

func joinURL(base, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(path, "/")
	return parsed.String(), nil
}
