package config

import "testing"

func TestLoadYAMLConfig(t *testing.T) {
	cfg, err := Load("../../configs/config.yml")
	if err != nil {
		t.Fatalf("Load(config.yml) error = %v", err)
	}
	if cfg.Schedule.DailyAt != "03:00:00" {
		t.Fatalf("Schedule.DailyAt = %q, want 03:00:00", cfg.Schedule.DailyAt)
	}
	if cfg.Resilience.LockFile != "out/docker/run/uav-camera-calibration-scanner.lock" {
		t.Fatalf("Resilience.LockFile = %q", cfg.Resilience.LockFile)
	}
	if cfg.VOD.ClipDurationSeconds != 40 {
		t.Fatalf("VOD.ClipDurationSeconds = %d, want 40", cfg.VOD.ClipDurationSeconds)
	}
	if cfg.DBAPI.DeviceListRequest == nil {
		t.Fatalf("DBAPI.DeviceListRequest = nil, want empty map")
	}
	if !cfg.ControlCenter.Enabled || cfg.ControlCenter.Username != "admin" || cfg.ControlCenter.GroupID != "1" {
		t.Fatalf("unexpected control-center config: %+v", cfg.ControlCenter)
	}
	models := cfg.TargetModelSet()
	if len(models) != 2 {
		t.Fatalf("TargetModelSet() = %+v, want only M30T and M30TD", models)
	}
	for _, model := range []string{"M30T", "M30TD"} {
		if _, ok := models[model]; !ok {
			t.Fatalf("TargetModelSet() missing %s: %+v", model, models)
		}
	}
	targets := cfg.TargetGimbalTypeSet()
	if len(targets) != 2 {
		t.Fatalf("TargetGimbalTypeSet() = %+v, want only M30T_CAMERA and M30TD_CAMERA", targets)
	}
	if _, ok := targets["M30T_CAMERA"]; !ok {
		t.Fatalf("TargetGimbalTypeSet() missing M30T_CAMERA: %+v", targets)
	}
	if _, ok := targets["M30TD_CAMERA"]; !ok {
		t.Fatalf("TargetGimbalTypeSet() missing M30TD_CAMERA: %+v", targets)
	}
}

func TestNormalizeGimbalType(t *testing.T) {
	for _, value := range []string{"M30T_CAMERA", "M30T Camera", " m30t-camera "} {
		if got := NormalizeGimbalType(value); got != "M30T_CAMERA" {
			t.Fatalf("NormalizeGimbalType(%q) = %q, want M30T_CAMERA", value, got)
		}
	}
}
