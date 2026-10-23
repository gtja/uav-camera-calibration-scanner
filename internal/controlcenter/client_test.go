package controlcenter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"jingansi/uav-camera-calibration-scanner/internal/config"
)

func TestListDevicesUsesAdminHeadersAndMapsRows(t *testing.T) {
	requestPages := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/device/list" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("username"); got != "admin" {
			t.Fatalf("username header = %q, want admin", got)
		}
		if got := r.Header.Get("groupId"); got != "1" {
			t.Fatalf("groupId header = %q, want 1", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		page := int(body["page"].(float64))
		requestPages = append(requestPages, page)
		w.Header().Set("Content-Type", "application/json")
		rows := []map[string]any{}
		switch page {
		case 1:
			rows = append(rows, map[string]any{
				"productKey": "00000000002",
				"deviceId":   "34020000006010310100",
				"deviceName": "test drone",
				"deviceType": "S220PRO_ONE",
				"properties": map[string]any{
					"sn":         "GDU9PZ2P220HD26A0053",
					"gimbalType": "S220_CAMERA",
					"liveStatus": []any{
						map[string]any{"video_id": "video-a"},
					},
				},
			})
		case 2:
			rows = append(rows, map[string]any{
				"productKey": "ignored-product-key",
				"deviceId":   "ignored-device",
			})
		default:
			t.Fatalf("unexpected page request: %d", page)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    "SUCCESS",
			"message": "ok",
			"data": map[string]any{
				"rows":  rows,
				"total": 2,
			},
		})
	}))
	defer server.Close()

	client := New(config.ControlCenterConfig{
		Enabled:              true,
		BaseURL:              server.URL,
		DeviceListPath:       "/openapi/device/list",
		Username:             "admin",
		GroupID:              "1",
		PageSize:             1,
		CandidateProductKeys: []string{"00000000002"},
	})
	devices, err := client.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("device count = %d, want 1: %+v", len(devices), devices)
	}
	device := devices[0]
	if device.DeviceID != "34020000006010310100" || device.ProductKey != "00000000002" {
		t.Fatalf("unexpected device identity: %+v", device)
	}
	if device.SN != "GDU9PZ2P220HD26A0053" || device.Model != "S220PRO_ONE" || device.VideoID != "video-a" {
		t.Fatalf("unexpected mapped device: %+v", device)
	}
	if device.Tags["GIMBAL_TYPE"] != "S220_CAMERA" {
		t.Fatalf("missing GIMBAL_TYPE tag: %+v", device.Tags)
	}
	if len(requestPages) != 2 || requestPages[0] != 1 || requestPages[1] != 2 {
		t.Fatalf("unexpected requested pages: %+v", requestPages)
	}
}
