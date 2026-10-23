package dbapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/model"
)

type Client struct {
	baseURL string
	cfg     config.DBAPIConfig
	http    *http.Client
	loc     *time.Location
}

func New(cfg config.DBAPIConfig, loc *time.Location) *Client {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		cfg:     cfg,
		http:    &http.Client{Timeout: timeout},
		loc:     loc,
	}
}

func (c *Client) ListDevices(ctx context.Context) ([]model.Device, error) {
	if strings.TrimSpace(c.cfg.DeviceListPath) == "" {
		return nil, errors.New("dbapi deviceListPath is empty and no static devices configured")
	}
	var resp envelope
	if err := c.post(ctx, c.cfg.DeviceListPath, c.cfg.DeviceListRequest, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("dbapi list devices failed: %s", resp.Msg)
	}
	rows, err := asObjectSlice(resp.Data)
	if err != nil {
		return nil, err
	}
	devices := make([]model.Device, 0, len(rows))
	for _, row := range rows {
		deviceID := firstString(row, "deviceId", "device_id", "id")
		if deviceID == "" {
			continue
		}
		device := model.Device{
			DeviceID:   deviceID,
			ProductKey: firstString(row, "productKey", "product_key"),
			DeviceName: firstString(row, "deviceName", "device_name", "name"),
			SN:         firstString(row, "sn", "deviceSn", "device_sn"),
			Model:      firstString(row, "model", "deviceModel", "device_model", "deviceType", "device_type"),
			Tags:       parseTags(row),
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func (c *Client) FetchSamples(ctx context.Context, device model.Device, start, end time.Time) ([]model.Sample, error) {
	var resp envelope
	body := map[string]any{
		"deviceId":  device.DeviceID,
		"startTime": formatDBTime(start),
		"endTime":   formatDBTime(end),
	}
	if err := c.post(ctx, c.cfg.HistoryPath, body, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("dbapi history failed for %s: %s", device.DeviceID, resp.Msg)
	}
	rows, err := asObjectSlice(resp.Data)
	if err != nil {
		return nil, err
	}
	samples := make([]model.Sample, 0, len(rows))
	for _, row := range rows {
		sample, ok := c.sampleFromRow(device.DeviceID, row)
		if ok {
			samples = append(samples, sample)
		}
	}
	sort.Slice(samples, func(i, j int) bool {
		return samples[i].SampleTime.Before(samples[j].SampleTime)
	})
	return samples, nil
}

func (c *Client) FetchPropertiesAt(ctx context.Context, device model.Device, at time.Time) (model.PropertySnapshot, error) {
	path := c.cfg.SnapshotPath
	if strings.TrimSpace(path) == "" {
		path = c.cfg.HistoryPath
	}
	var resp envelope
	body := map[string]any{
		"deviceId":  device.DeviceID,
		"startTime": formatDBTime(at),
		"endTime":   formatDBTime(at.Add(time.Second)),
	}
	if err := c.post(ctx, path, body, &resp); err != nil {
		return model.PropertySnapshot{}, err
	}
	if !resp.Success {
		return model.PropertySnapshot{}, fmt.Errorf("dbapi snapshot failed for %s: %s", device.DeviceID, resp.Msg)
	}
	rows, err := asObjectSlice(resp.Data)
	if err != nil {
		return model.PropertySnapshot{}, err
	}
	if len(rows) == 0 {
		return model.PropertySnapshot{}, fmt.Errorf("dbapi snapshot empty for %s at %s", device.DeviceID, at.Format(time.RFC3339))
	}
	bestIdx := -1
	bestDistance := time.Duration(1<<63 - 1)
	for i, row := range rows {
		ts, ok := c.rowTime(row)
		if !ok {
			continue
		}
		distance := at.Sub(ts)
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			bestDistance = distance
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		bestIdx = 0
	}
	raw, ok := rawProperties(rows[bestIdx])
	if !ok {
		return model.PropertySnapshot{}, fmt.Errorf("dbapi snapshot row has no properties for %s", device.DeviceID)
	}
	ts, _ := c.rowTime(rows[bestIdx])
	if ts.IsZero() {
		ts = at
	}
	return model.PropertySnapshot{
		DeviceID:   device.DeviceID,
		SnapshotAt: ts,
		Properties: raw,
		Source:     "dbapi",
	}, nil
}

func (c *Client) sampleFromRow(deviceID string, row map[string]any) (model.Sample, bool) {
	ts, ok := c.rowTime(row)
	if !ok {
		return model.Sample{}, false
	}
	raw, ok := rawProperties(row)
	if !ok {
		return model.Sample{}, false
	}
	propsRaw := scopedProperties(raw, deviceID)
	props := map[string]any{}
	if err := json.Unmarshal(propsRaw, &props); err != nil {
		return model.Sample{}, false
	}
	rowDeviceID := firstString(row, "deviceId", "device_id", "id")
	if rowDeviceID == "" {
		rowDeviceID = deviceID
	}
	lat, lon, hasPosition := position(props)
	return model.Sample{
		DeviceID:        rowDeviceID,
		SampleTime:      ts,
		CameraMode:      propertyString(props, "cameraMode"),
		GimbalType:      firstNonEmpty(propertyString(props, "gimbalType"), propertyString(props, "gimbal_type")),
		GimbalPitch:     propertyFloat(props, "gimbalPitch"),
		LensType:        propertyString(props, "lensType"),
		FlightStatus:    propertyString(props, "flightStatus"),
		TaskStatus:      propertyString(props, "taskStatus"),
		Height:          firstFloat(props, "height", "altitude", "laserAltitude"),
		FlyTime:         propertyInt64(props, "flyTime"),
		HorizontalSpeed: propertyFloat(props, "horizontalSpeed"),
		Latitude:        lat,
		Longitude:       lon,
		HasPosition:     hasPosition,
		VideoID:         firstNonEmpty(propertyString(props, "videoId"), liveStatusVideoID(props)),
		PayloadIndex:    propertyString(props, "payloadIndex"),
		RawProperties:   raw,
	}, true
}

func (c *Client) rowTime(row map[string]any) (time.Time, bool) {
	value := firstString(row, "acquireTimestampFormat", "acquire_timestamp_format", "sampleTime", "sample_time")
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if ts, err := time.ParseInLocation(layout, value, c.loc); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	if c.baseURL == "" {
		return errors.New("dbapi baseUrl is empty")
	}
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
		return fmt.Errorf("dbapi %s returned %s: %s", endpoint, resp.Status, string(data))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode dbapi response: %w", err)
	}
	return nil
}

type envelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Code    any             `json:"code"`
	Data    json.RawMessage `json:"data"`
}

func asObjectSlice(raw json.RawMessage) ([]map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err == nil {
		return rows, nil
	}
	var single map[string]any
	if err := json.Unmarshal(raw, &single); err == nil {
		return []map[string]any{single}, nil
	}
	return nil, errors.New("dbapi data is not an object array")
}

func rawProperties(row map[string]any) (json.RawMessage, bool) {
	value, ok := row["properties"]
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case string:
		return json.RawMessage(typed), typed != ""
	case map[string]any, []any:
		data, err := json.Marshal(typed)
		return data, err == nil
	default:
		data, err := json.Marshal(typed)
		return data, err == nil && string(data) != "null"
	}
}

func scopedProperties(raw json.RawMessage, deviceID string) json.RawMessage {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return raw
	}
	if scoped, ok := root[deviceID]; ok {
		return scoped
	}
	return raw
}

func parseTags(row map[string]any) map[string]string {
	tags := map[string]string{}
	if raw, ok := row["tags"]; ok {
		switch typed := raw.(type) {
		case map[string]any:
			for k, v := range typed {
				tags[k] = fmt.Sprint(v)
			}
		case []any:
			for _, item := range typed {
				obj, ok := item.(map[string]any)
				if !ok {
					continue
				}
				key := firstString(obj, "tag", "key", "tagName", "name")
				value := firstString(obj, "tagValue", "value")
				if key != "" {
					tags[key] = value
				}
			}
		}
	}
	for _, key := range []string{"deviceModel", "device_model", "deviceType", "device_type", "tagValue", "tag_value"} {
		if value := firstString(row, key); value != "" {
			tags[key] = value
		}
	}
	return tags
}

func propertyString(props map[string]any, key string) string {
	value, ok := props[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func propertyFloat(props map[string]any, key string) float64 {
	value, ok := props[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		parsed, _ := strconv.ParseFloat(fmt.Sprint(typed), 64)
		return parsed
	}
}

func firstFloat(props map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if _, ok := props[key]; ok {
			return propertyFloat(props, key)
		}
	}
	return 0
}

func position(props map[string]any) (float64, float64, bool) {
	lat, okLat := firstPresentFloat(props, "latitude", "lat", "flightLatitude", "aircraftLatitude", "uavLatitude")
	lon, okLon := firstPresentFloat(props, "longitude", "lng", "lon", "flightLongitude", "aircraftLongitude", "uavLongitude")
	if !okLat || !okLon {
		return 0, 0, false
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, false
	}
	return lat, lon, true
}

func firstPresentFloat(props map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if _, ok := props[key]; ok {
			return propertyFloat(props, key), true
		}
	}
	return 0, false
}

func propertyInt64(props map[string]any, key string) int64 {
	value, ok := props[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseInt(fmt.Sprint(typed), 10, 64)
		return parsed
	}
}

func liveStatusVideoID(props map[string]any) string {
	raw, ok := props["liveStatus"].([]any)
	if !ok || len(raw) == 0 {
		return ""
	}
	first, ok := raw[0].(map[string]any)
	if !ok {
		return ""
	}
	return firstString(first, "video_id", "videoId")
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			text := strings.TrimSpace(fmt.Sprint(value))
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

func formatDBTime(ts time.Time) string {
	return ts.Format("2006-01-02 15:04:05")
}

func joinURL(base, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty dbapi path")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(path, "/")
	return parsed.String(), nil
}
