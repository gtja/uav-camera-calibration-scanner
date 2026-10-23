package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/model"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Timezone      string              `json:"timezone" yaml:"timezone"`
	RuleVersion   string              `json:"ruleVersion" yaml:"ruleVersion"`
	TargetModels  []string            `json:"targetModels" yaml:"targetModels"`
	Logging       LoggingConfig       `json:"logging" yaml:"logging"`
	Schedule      ScheduleConfig      `json:"schedule" yaml:"schedule"`
	Resilience    ResilienceConfig    `json:"resilience" yaml:"resilience"`
	Scan          ScanConfig          `json:"scan" yaml:"scan"`
	Rule          RuleConfig          `json:"rule" yaml:"rule"`
	ControlCenter ControlCenterConfig `json:"controlCenter" yaml:"controlCenter"`
	DBAPI         DBAPIConfig         `json:"dbapi" yaml:"dbapi"`
	VOD           VODConfig           `json:"vod" yaml:"vod"`
	Calibration   CalibrationConfig   `json:"calibration" yaml:"calibration"`
	Artifact      ArtifactConfig      `json:"artifact" yaml:"artifact"`
	Result        ResultConfig        `json:"result" yaml:"result"`
	Devices       []model.Device      `json:"devices" yaml:"devices"`
}

type LoggingConfig struct {
	Level  string `json:"level" yaml:"level"`
	Format string `json:"format" yaml:"format"`
}

type ScheduleConfig struct {
	DailyAt    string `json:"dailyAt" yaml:"dailyAt"`
	RunOnStart bool   `json:"runOnStart" yaml:"runOnStart"`
}

type ResilienceConfig struct {
	RetryAttempts           int    `json:"retryAttempts" yaml:"retryAttempts"`
	RetryBaseDelayMillis    int    `json:"retryBaseDelayMillis" yaml:"retryBaseDelayMillis"`
	CircuitFailureThreshold int    `json:"circuitFailureThreshold" yaml:"circuitFailureThreshold"`
	CircuitOpenSeconds      int    `json:"circuitOpenSeconds" yaml:"circuitOpenSeconds"`
	RunTimeoutMinutes       int    `json:"runTimeoutMinutes" yaml:"runTimeoutMinutes"`
	LockFile                string `json:"lockFile" yaml:"lockFile"`
}

type ScanConfig struct {
	StartTime              string `json:"startTime" yaml:"startTime"`
	EndTime                string `json:"endTime" yaml:"endTime"`
	RecordNoMatch          bool   `json:"recordNoMatch" yaml:"recordNoMatch"`
	MaxDevices             int    `json:"maxDevices" yaml:"maxDevices"`
	QueryChunkMinutes      int    `json:"queryChunkMinutes" yaml:"queryChunkMinutes"`
	PreciseDevicePrefilter bool   `json:"preciseDevicePrefilter" yaml:"preciseDevicePrefilter"`
}

type RuleConfig struct {
	DurationSeconds     int      `json:"durationSeconds" yaml:"durationSeconds"`
	MaxSampleGapSeconds int      `json:"maxSampleGapSeconds" yaml:"maxSampleGapSeconds"`
	CameraMode          string   `json:"cameraMode" yaml:"cameraMode"`
	LensType            string   `json:"lensType" yaml:"lensType"`
	TargetGimbalTypes   []string `json:"targetGimbalTypes" yaml:"targetGimbalTypes"`
	GimbalPitchMin      float64  `json:"gimbalPitchMin" yaml:"gimbalPitchMin"`
	GimbalPitchMax      float64  `json:"gimbalPitchMax" yaml:"gimbalPitchMax"`
	RequireFlightStatus bool     `json:"requireFlightStatus" yaml:"requireFlightStatus"`
	MinHeightMeters     float64  `json:"minHeightMeters" yaml:"minHeightMeters"`
	RequireFlyTime      bool     `json:"requireFlyTime" yaml:"requireFlyTime"`
	MinTravelMeters     float64  `json:"minTravelMeters" yaml:"minTravelMeters"`
}

type DBAPIConfig struct {
	BaseURL           string            `json:"baseUrl" yaml:"baseUrl"`
	DeviceListPath    string            `json:"deviceListPath" yaml:"deviceListPath"`
	HistoryPath       string            `json:"historyPath" yaml:"historyPath"`
	SnapshotPath      string            `json:"snapshotPath" yaml:"snapshotPath"`
	SampleFile        string            `json:"sampleFile" yaml:"sampleFile"`
	TimeoutSeconds    int               `json:"timeoutSeconds" yaml:"timeoutSeconds"`
	Headers           map[string]string `json:"headers" yaml:"headers"`
	DeviceListRequest map[string]any    `json:"deviceListRequest" yaml:"deviceListRequest"`
}

type ControlCenterConfig struct {
	Enabled              bool              `json:"enabled" yaml:"enabled"`
	BaseURL              string            `json:"baseUrl" yaml:"baseUrl"`
	DeviceListPath       string            `json:"deviceListPath" yaml:"deviceListPath"`
	TimeoutSeconds       int               `json:"timeoutSeconds" yaml:"timeoutSeconds"`
	Username             string            `json:"username" yaml:"username"`
	GroupID              string            `json:"groupId" yaml:"groupId"`
	PageSize             int               `json:"pageSize" yaml:"pageSize"`
	Headers              map[string]string `json:"headers" yaml:"headers"`
	DeviceListRequest    map[string]any    `json:"deviceListRequest" yaml:"deviceListRequest"`
	CandidateProductKeys []string          `json:"candidateProductKeys" yaml:"candidateProductKeys"`
}

type VODConfig struct {
	BaseURL             string            `json:"baseUrl" yaml:"baseUrl"`
	ClipPath            string            `json:"clipPath" yaml:"clipPath"`
	ClipDurationSeconds int               `json:"clipDurationSeconds" yaml:"clipDurationSeconds"`
	TimeoutSeconds      int               `json:"timeoutSeconds" yaml:"timeoutSeconds"`
	Headers             map[string]string `json:"headers" yaml:"headers"`
	DryRun              bool              `json:"dryRun" yaml:"dryRun"`
}

type CalibrationConfig struct {
	Enabled        bool    `json:"enabled" yaml:"enabled"`
	BaseURL        string  `json:"baseUrl" yaml:"baseUrl"`
	CreatePath     string  `json:"createPath" yaml:"createPath"`
	TimeoutSeconds int     `json:"timeoutSeconds" yaml:"timeoutSeconds"`
	FPS            float64 `json:"fps" yaml:"fps"`
	CallbackURL    string  `json:"callbackUrl" yaml:"callbackUrl"`
	DedupEnabled   bool    `json:"dedupEnabled" yaml:"dedupEnabled"`
	DedupDays      int     `json:"dedupDays" yaml:"dedupDays"`
}

type ArtifactConfig struct {
	LocalDir  string `json:"localDir" yaml:"localDir"`
	URLPrefix string `json:"urlPrefix" yaml:"urlPrefix"`
}

type ResultConfig struct {
	JSONLPath  string `json:"jsonlPath" yaml:"jsonlPath"`
	MySQLDSN   string `json:"mysqlDsn" yaml:"mysqlDsn"`
	MySQLTable string `json:"mysqlTable" yaml:"mysqlTable"`
}

func Default() Config {
	return Config{
		Timezone:     "Asia/Shanghai",
		RuleVersion:  "downward-photo-v3",
		TargetModels: []string{"M30T", "M30TD"},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Schedule: ScheduleConfig{
			DailyAt:    "03:00:00",
			RunOnStart: false,
		},
		Resilience: ResilienceConfig{
			RetryAttempts:           2,
			RetryBaseDelayMillis:    500,
			CircuitFailureThreshold: 3,
			CircuitOpenSeconds:      300,
			RunTimeoutMinutes:       120,
			LockFile:                "/tmp/uav-camera-calibration-scanner.lock",
		},
		Scan: ScanConfig{
			StartTime:         "09:00:00",
			EndTime:           "17:00:00",
			RecordNoMatch:     true,
			QueryChunkMinutes: 30,
		},
		Rule: RuleConfig{
			DurationSeconds:     20,
			MaxSampleGapSeconds: 3,
			CameraMode:          "0",
			LensType:            "wide",
			GimbalPitchMin:      -90,
			GimbalPitchMax:      -10,
			RequireFlightStatus: false,
			MinHeightMeters:     5,
			RequireFlyTime:      true,
			MinTravelMeters:     100,
		},
		DBAPI: DBAPIConfig{
			HistoryPath:    "/deviceAttrInfoBack",
			SnapshotPath:   "/uavDeviceAttrBackV2",
			TimeoutSeconds: 60,
		},
		ControlCenter: ControlCenterConfig{
			DeviceListPath: "/openapi/device/list",
			TimeoutSeconds: 30,
			Username:       "admin",
			GroupID:        "1",
			PageSize:       1000,
		},
		VOD: VODConfig{
			ClipPath:            "/vod/hls2mp4",
			ClipDurationSeconds: 20,
			TimeoutSeconds:      120,
			DryRun:              true,
		},
		Calibration: CalibrationConfig{
			CreatePath:     "/calibration/create",
			TimeoutSeconds: 30,
			FPS:            1.0,
			DedupEnabled:   true,
			DedupDays:      7,
		},
		Artifact: ArtifactConfig{
			LocalDir: "out/artifacts",
		},
		Result: ResultConfig{
			JSONLPath:  "out/results.jsonl",
			MySQLTable: "uav_downward_photo_clip",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := decodeConfig(path, data, &cfg); err != nil {
		return cfg, err
	}
	applyDefaults(&cfg)
	return cfg, nil
}

func decodeConfig(path string, data []byte, cfg *Config) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return yaml.Unmarshal(data, cfg)
	case ".json", "":
		return json.Unmarshal(data, cfg)
	default:
		if err := json.Unmarshal(data, cfg); err == nil {
			return nil
		}
		return yaml.Unmarshal(data, cfg)
	}
}

func applyDefaults(cfg *Config) {
	def := Default()
	if cfg.Timezone == "" {
		cfg.Timezone = def.Timezone
	}
	if cfg.RuleVersion == "" {
		cfg.RuleVersion = def.RuleVersion
	}
	if len(cfg.TargetModels) == 0 {
		cfg.TargetModels = def.TargetModels
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = def.Logging.Level
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = def.Logging.Format
	}
	if cfg.Schedule.DailyAt == "" {
		cfg.Schedule.DailyAt = def.Schedule.DailyAt
	}
	if cfg.Resilience.RetryAttempts == 0 {
		cfg.Resilience.RetryAttempts = def.Resilience.RetryAttempts
	}
	if cfg.Resilience.RetryBaseDelayMillis == 0 {
		cfg.Resilience.RetryBaseDelayMillis = def.Resilience.RetryBaseDelayMillis
	}
	if cfg.Resilience.CircuitFailureThreshold == 0 {
		cfg.Resilience.CircuitFailureThreshold = def.Resilience.CircuitFailureThreshold
	}
	if cfg.Resilience.CircuitOpenSeconds == 0 {
		cfg.Resilience.CircuitOpenSeconds = def.Resilience.CircuitOpenSeconds
	}
	if cfg.Resilience.RunTimeoutMinutes == 0 {
		cfg.Resilience.RunTimeoutMinutes = def.Resilience.RunTimeoutMinutes
	}
	if cfg.Resilience.LockFile == "" {
		cfg.Resilience.LockFile = def.Resilience.LockFile
	}
	if cfg.Scan.StartTime == "" {
		cfg.Scan.StartTime = def.Scan.StartTime
	}
	if cfg.Scan.EndTime == "" {
		cfg.Scan.EndTime = def.Scan.EndTime
	}
	if cfg.Scan.QueryChunkMinutes == 0 {
		cfg.Scan.QueryChunkMinutes = def.Scan.QueryChunkMinutes
	}
	if cfg.Rule.DurationSeconds == 0 {
		cfg.Rule.DurationSeconds = def.Rule.DurationSeconds
	}
	if cfg.Rule.MaxSampleGapSeconds == 0 {
		cfg.Rule.MaxSampleGapSeconds = def.Rule.MaxSampleGapSeconds
	}
	if cfg.Rule.CameraMode == "" {
		cfg.Rule.CameraMode = def.Rule.CameraMode
	}
	if cfg.Rule.LensType == "" {
		cfg.Rule.LensType = def.Rule.LensType
	}
	if cfg.Rule.GimbalPitchMin == 0 {
		cfg.Rule.GimbalPitchMin = def.Rule.GimbalPitchMin
	}
	if cfg.Rule.GimbalPitchMax == 0 {
		cfg.Rule.GimbalPitchMax = def.Rule.GimbalPitchMax
	}
	if cfg.Rule.MinHeightMeters == 0 {
		cfg.Rule.MinHeightMeters = def.Rule.MinHeightMeters
	}
	if cfg.Rule.MinTravelMeters == 0 {
		cfg.Rule.MinTravelMeters = def.Rule.MinTravelMeters
	}
	if cfg.DBAPI.HistoryPath == "" {
		cfg.DBAPI.HistoryPath = def.DBAPI.HistoryPath
	}
	if cfg.DBAPI.SnapshotPath == "" {
		cfg.DBAPI.SnapshotPath = def.DBAPI.SnapshotPath
	}
	if cfg.DBAPI.TimeoutSeconds == 0 {
		cfg.DBAPI.TimeoutSeconds = def.DBAPI.TimeoutSeconds
	}
	if cfg.ControlCenter.DeviceListPath == "" {
		cfg.ControlCenter.DeviceListPath = def.ControlCenter.DeviceListPath
	}
	if cfg.ControlCenter.TimeoutSeconds == 0 {
		cfg.ControlCenter.TimeoutSeconds = def.ControlCenter.TimeoutSeconds
	}
	if cfg.ControlCenter.Username == "" {
		cfg.ControlCenter.Username = def.ControlCenter.Username
	}
	if cfg.ControlCenter.GroupID == "" {
		cfg.ControlCenter.GroupID = def.ControlCenter.GroupID
	}
	if cfg.ControlCenter.PageSize == 0 {
		cfg.ControlCenter.PageSize = def.ControlCenter.PageSize
	}
	if cfg.VOD.ClipPath == "" {
		cfg.VOD.ClipPath = def.VOD.ClipPath
	}
	if cfg.VOD.ClipDurationSeconds == 0 {
		cfg.VOD.ClipDurationSeconds = def.VOD.ClipDurationSeconds
	}
	if cfg.VOD.TimeoutSeconds == 0 {
		cfg.VOD.TimeoutSeconds = def.VOD.TimeoutSeconds
	}
	if cfg.Calibration.CreatePath == "" {
		cfg.Calibration.CreatePath = def.Calibration.CreatePath
	}
	if cfg.Calibration.TimeoutSeconds == 0 {
		cfg.Calibration.TimeoutSeconds = def.Calibration.TimeoutSeconds
	}
	if cfg.Calibration.FPS == 0 {
		cfg.Calibration.FPS = def.Calibration.FPS
	}
	if cfg.Calibration.DedupDays == 0 {
		cfg.Calibration.DedupDays = def.Calibration.DedupDays
	}
	if cfg.Artifact.LocalDir == "" {
		cfg.Artifact.LocalDir = def.Artifact.LocalDir
	}
	if cfg.Result.JSONLPath == "" && cfg.Result.MySQLDSN == "" {
		cfg.Result.JSONLPath = def.Result.JSONLPath
	}
	if cfg.Result.MySQLTable == "" {
		cfg.Result.MySQLTable = def.Result.MySQLTable
	}
}

func (c Config) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", c.Timezone, err)
	}
	return loc, nil
}

func (c Config) TargetModelSet() map[string]struct{} {
	out := make(map[string]struct{}, len(c.TargetModels))
	for _, value := range c.TargetModels {
		normalized := strings.ToUpper(strings.TrimSpace(value))
		if normalized != "" {
			out[normalized] = struct{}{}
		}
	}
	return out
}

func (c Config) TargetGimbalTypeSet() map[string]struct{} {
	out := make(map[string]struct{}, len(c.Rule.TargetGimbalTypes))
	for _, value := range c.Rule.TargetGimbalTypes {
		normalized := NormalizeGimbalType(value)
		if normalized != "" {
			out[normalized] = struct{}{}
		}
	}
	return out
}

func NormalizeGimbalType(value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	normalized = strings.NewReplacer(" ", "_", "-", "_").Replace(normalized)
	for strings.Contains(normalized, "__") {
		normalized = strings.ReplaceAll(normalized, "__", "_")
	}
	return strings.Trim(normalized, "_")
}
