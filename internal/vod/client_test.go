package vod

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/model"
)

func TestCreateClipUsesHLS2MP4RequestAndLocation(t *testing.T) {
	start := time.Date(2026, 7, 7, 16, 0, 32, 0, time.UTC)
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vod/hls2mp4" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"code":"SUCCESS","data":{"location":"/vod/download?objectId=clip.mp4"}}`))
	}))
	defer server.Close()

	client := New(config.VODConfig{
		BaseURL:        server.URL,
		ClipPath:       "/vod/hls2mp4",
		TimeoutSeconds: 5,
		DryRun:         false,
	})
	url, err := client.CreateClip(context.Background(), model.Device{DeviceID: "device-1"}, model.Window{
		Start: start,
		End:   start.Add(20 * time.Second),
	}, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if url != "/vod/download?objectId=clip.mp4" {
		t.Fatalf("unexpected url: %s", url)
	}
	if got["deviceId"] != "device-1" {
		t.Fatalf("unexpected deviceId: %+v", got)
	}
	if got["startTime"] != float64(start.UnixMilli()) || got["endTime"] != float64(start.Add(20*time.Second).UnixMilli()) {
		t.Fatalf("unexpected time fields: %+v", got)
	}
	if _, ok := got["begin"]; ok {
		t.Fatalf("hls2mp4 request should not include begin: %+v", got)
	}
}

func TestExtractURLFindsLocation(t *testing.T) {
	url := extractURL([]byte(`{"code":"SUCCESS","data":{"location":"http://example.test/clip.mp4"}}`))
	if url != "http://example.test/clip.mp4" {
		t.Fatalf("unexpected url: %q", url)
	}
}
