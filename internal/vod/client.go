package vod

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
	"jingansi/uav-camera-calibration-scanner/internal/model"
)

type Client struct {
	cfg  config.VODConfig
	http *http.Client
}

type ClipRequest struct {
	DeviceID        string `json:"deviceId"`
	Begin           int64  `json:"begin"`
	DurationSeconds int    `json:"durationSeconds"`
	OutputPrefix    string `json:"outputPrefix,omitempty"`
}

type HLS2MP4Request struct {
	DeviceID  string `json:"deviceId"`
	StartTime int64  `json:"startTime"`
	EndTime   int64  `json:"endTime"`
}

func New(cfg config.VODConfig) *Client {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: timeout}}
}

func (c *Client) CreateClip(ctx context.Context, device model.Device, window model.Window, outputPrefix string) (string, error) {
	req := ClipRequest{
		DeviceID:        device.DeviceID,
		Begin:           window.Start.UnixMilli(),
		DurationSeconds: int(window.End.Sub(window.Start).Seconds()),
		OutputPrefix:    outputPrefix,
	}
	if c.cfg.DryRun {
		return fmt.Sprintf("dryrun://vod/%s/%d-%ds.mp4", device.DeviceID, req.Begin, req.DurationSeconds), nil
	}
	if strings.TrimSpace(c.cfg.BaseURL) == "" {
		return "", errors.New("vod baseUrl is empty")
	}
	endpoint, err := joinURL(c.cfg.BaseURL, c.cfg.ClipPath)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(c.requestBody(req, window))
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.cfg.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("vod clip returned %s: %s", resp.Status, string(data))
	}
	clipURL := extractURL(data)
	if clipURL == "" {
		return "", fmt.Errorf("vod clip response has no url: %s", string(data))
	}
	return clipURL, nil
}

func (c *Client) requestBody(req ClipRequest, window model.Window) any {
	if strings.Contains(strings.ToLower(c.cfg.ClipPath), "hls2mp4") {
		return HLS2MP4Request{
			DeviceID:  req.DeviceID,
			StartTime: window.Start.UnixMilli(),
			EndTime:   window.End.UnixMilli(),
		}
	}
	return req
}

func extractURL(data []byte) string {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return ""
	}
	return findURL(root)
}

func findURL(value any) string {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "http://") || strings.HasPrefix(typed, "https://") || strings.HasPrefix(typed, "/") {
			return typed
		}
	case map[string]any:
		for _, key := range []string{"clipUrl", "clipURL", "url", "downloadUrl", "objectUrl", "location"} {
			if url, ok := typed[key].(string); ok && url != "" {
				return url
			}
		}
		for _, key := range []string{"data", "result"} {
			if nested, ok := typed[key]; ok {
				if url := findURL(nested); url != "" {
					return url
				}
			}
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
