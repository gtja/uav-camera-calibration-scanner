package controlcenter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/model"
)

type Client struct {
	baseURL string
	cfg     config.ControlCenterConfig
	http    *http.Client
}

func New(cfg config.ControlCenterConfig) *Client {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		cfg:     cfg,
		http:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.cfg.Enabled
}

func (c *Client) ListDevices(ctx context.Context) ([]model.Device, error) {
	if !c.Enabled() {
		return nil, errors.New("controlCenter is disabled")
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return nil, errors.New("controlCenter baseUrl is empty")
	}
	pageSize := c.cfg.PageSize
	if pageSize <= 0 {
		pageSize = 1000
	}
	productKeys := stringSet(c.cfg.CandidateProductKeys)
	var out []model.Device
	seen := map[string]struct{}{}
	total := -1
	for page := 1; ; page++ {
		resp, err := c.listPage(ctx, page, pageSize)
		if err != nil {
			return nil, err
		}
		if total < 0 {
			total = resp.Total
		}
		for _, row := range resp.Rows {
			device := deviceFromRow(row)
			if strings.TrimSpace(device.DeviceID) == "" {
				continue
			}
			if len(productKeys) > 0 {
				if _, ok := productKeys[device.ProductKey]; !ok {
					continue
				}
			}
			if _, ok := seen[device.DeviceID]; ok {
				continue
			}
			seen[device.DeviceID] = struct{}{}
			out = append(out, device)
		}
		if len(resp.Rows) == 0 {
			break
		}
		if total >= 0 && page*pageSize >= total {
			break
		}
		if len(resp.Rows) < pageSize {
			break
		}
	}
	return out, nil
}

func (c *Client) listPage(ctx context.Context, page, size int) (pageData, error) {
	body := map[string]any{}
	for k, v := range c.cfg.DeviceListRequest {
		body[k] = v
	}
	body["page"] = page
	body["size"] = size
	var env envelope
	if err := c.post(ctx, c.cfg.DeviceListPath, body, &env); err != nil {
		return pageData{}, err
	}
	if !env.ok() {
		return pageData{}, fmt.Errorf("controlCenter list devices failed: code=%v message=%s", env.Code, env.Message)
	}
	return decodePage(env.Data)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	endpoint, err := joinURL(c.baseURL, path)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.cfg.Username) != "" {
		req.Header.Set("username", c.cfg.Username)
	}
	if strings.TrimSpace(c.cfg.GroupID) != "" {
		req.Header.Set("groupId", c.cfg.GroupID)
	}
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("controlCenter %s returned %s: %s", endpoint, resp.Status, string(data))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode controlCenter response: %w", err)
	}
	return nil
}

type envelope struct {
	Code    any             `json:"code"`
	Message string          `json:"message"`
	Success *bool           `json:"success"`
	Msg     string          `json:"msg"`
	Data    json.RawMessage `json:"data"`
}

func (e envelope) ok() bool {
	if e.Success != nil {
		return *e.Success
	}
	if text, ok := e.Code.(string); ok {
		return strings.EqualFold(text, "SUCCESS") || text == "200"
	}
	if number, ok := e.Code.(float64); ok {
		return int(number) == 200
	}
	return false
}

type pageData struct {
	Rows  []map[string]any
	Total int
}

func decodePage(raw json.RawMessage) (pageData, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return pageData{}, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err == nil {
		return pageData{Rows: rows, Total: len(rows)}, nil
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(raw, &page); err != nil {
		return pageData{}, errors.New("controlCenter data is not a page object")
	}
	for _, key := range []string{"rows", "list", "records", "items"} {
		if value, ok := page[key]; ok {
			if err := json.Unmarshal(value, &rows); err != nil {
				return pageData{}, fmt.Errorf("decode controlCenter data.%s: %w", key, err)
			}
			return pageData{Rows: rows, Total: pageTotal(page, len(rows))}, nil
		}
	}
	return pageData{}, errors.New("controlCenter page has no rows/list/records/items")
}

func pageTotal(page map[string]json.RawMessage, fallback int) int {
	for _, key := range []string{"total", "count", "totalCount"} {
		if raw, ok := page[key]; ok {
			var number int
			if err := json.Unmarshal(raw, &number); err == nil {
				return number
			}
			var text string
			if err := json.Unmarshal(raw, &text); err == nil {
				if parsed, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
					return parsed
				}
			}
		}
	}
	return fallback
}

func deviceFromRow(row map[string]any) model.Device {
	props := objectValue(row["properties"])
	tags := parseTags(row)
	putTag(tags, "DEVICE_MODEL", firstNonEmpty(
		firstString(row, "deviceModel", "device_model", "model"),
		firstString(props, "deviceModel", "device_model"),
	))
	putTag(tags, "GIMBAL_TYPE", firstNonEmpty(
		firstString(row, "gimbalType", "gimbal_type"),
		firstString(props, "gimbalType", "gimbal_type"),
	))
	putTag(tags, "STATUS", firstString(row, "status", "deviceStatus", "device_status"))
	putTag(tags, "DEVICE_TYPE", firstString(row, "deviceType", "device_type"))
	return model.Device{
		ProductKey: firstString(row, "productKey", "product_key"),
		DeviceID:   firstString(row, "deviceId", "device_id", "id"),
		DeviceName: firstString(row, "deviceName", "device_name", "name"),
		SN: firstNonEmpty(
			firstString(row, "sn", "deviceSn", "device_sn"),
			firstString(props, "sn", "cameraSn", "camera_sn"),
		),
		Model: firstNonEmpty(
			firstString(row, "model", "deviceModel", "device_model", "deviceType", "device_type"),
			firstString(props, "deviceModel", "device_model", "gimbalType", "gimbal_type"),
		),
		Tags: tags,
		VideoID: firstNonEmpty(
			firstString(row, "videoId", "video_id"),
			liveStatusVideoID(props),
		),
		PayloadIndex: firstString(props, "payloadIndex", "payload_index"),
	}
}

func objectValue(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		var out map[string]any
		if err := json.Unmarshal([]byte(typed), &out); err == nil {
			return out
		}
	}
	return map[string]any{}
}

func parseTags(row map[string]any) map[string]string {
	tags := map[string]string{}
	for _, key := range []string{"tags", "deviceTags"} {
		raw, ok := row[key]
		if !ok {
			continue
		}
		switch typed := raw.(type) {
		case map[string]any:
			for k, v := range typed {
				putTag(tags, k, fmt.Sprint(v))
			}
		case []any:
			for _, item := range typed {
				obj, ok := item.(map[string]any)
				if !ok {
					continue
				}
				tagKey := firstString(obj, "tag", "key", "tagName", "name")
				tagValue := firstString(obj, "tagValue", "value")
				putTag(tags, tagKey, tagValue)
			}
		}
	}
	return tags
}

func putTag(tags map[string]string, key, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key != "" && value != "" {
		tags[key] = value
	}
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := obj[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return typed
			}
		case float64:
			if typed == float64(int64(typed)) {
				return strconv.FormatInt(int64(typed), 10)
			}
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(typed)
		default:
			text := strings.TrimSpace(fmt.Sprint(typed))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func liveStatusVideoID(props map[string]any) string {
	raw, ok := props["liveStatus"]
	if !ok {
		return ""
	}
	items, ok := raw.([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		videoID := firstString(obj, "video_id", "videoId")
		if videoID != "" {
			return videoID
		}
	}
	return ""
}

func stringSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}
	return out
}

func joinURL(base, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("controlCenter path is empty")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	if rel.IsAbs() {
		return rel.String(), nil
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	relPath := strings.TrimLeft(rel.Path, "/")
	parsed.Path = basePath + "/" + relPath
	parsed.RawQuery = rel.RawQuery
	return parsed.String(), nil
}
