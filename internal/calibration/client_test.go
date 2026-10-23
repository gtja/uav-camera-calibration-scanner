package calibration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"jingansi/uav-camera-calibration-scanner/internal/config"
)

func TestCreateTaskPostsAutoPhotographPayload(t *testing.T) {
	var got CreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calibration/create" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":{"task_id":"task-1","camera_sn":"CAM1","gimbal_type":"M30T_CAMERA","camera_mode":"photo","status":"pending"}}`))
	}))
	defer server.Close()

	client := New(config.CalibrationConfig{
		Enabled:        true,
		BaseURL:        server.URL,
		CreatePath:     "/calibration/create",
		TimeoutSeconds: 5,
		FPS:            1.5,
		CallbackURL:    "http://callback.test/calibration",
	})
	task, err := client.CreateTask(context.Background(), "/ja-media-storage/device/clip.mp4", map[string]any{
		"cameraSn":   "CAM1",
		"gimbalType": "M30T_CAMERA",
		"cameraMode": "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.TaskID != "task-1" || task.CameraMode != "photo" || task.Status != "pending" {
		t.Fatalf("unexpected task: %+v", task)
	}
	if got.VideoURL != "/ja-media-storage/device/clip.mp4" {
		t.Fatalf("unexpected video_url: %s", got.VideoURL)
	}
	if got.UAVParameters["cameraMode"] != "0" {
		t.Fatalf("cameraMode should be passed raw: %+v", got.UAVParameters)
	}
	if got.FPS != 1.5 || got.CallbackURL != "http://callback.test/calibration" {
		t.Fatalf("unexpected fps/callback: %+v", got)
	}
}

func TestCreateTaskDisabledIsNoop(t *testing.T) {
	client := New(config.CalibrationConfig{Enabled: false})
	task, err := client.CreateTask(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if task.TaskID != "" {
		t.Fatalf("disabled client should return empty task: %+v", task)
	}
}
