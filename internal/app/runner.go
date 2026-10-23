package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/artifact"
	"jingansi/uav-camera-calibration-scanner/internal/calibration"
	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/controlcenter"
	"jingansi/uav-camera-calibration-scanner/internal/dbapi"
	"jingansi/uav-camera-calibration-scanner/internal/model"
	"jingansi/uav-camera-calibration-scanner/internal/resilience"
	"jingansi/uav-camera-calibration-scanner/internal/result"
	"jingansi/uav-camera-calibration-scanner/internal/rule"
	"jingansi/uav-camera-calibration-scanner/internal/vod"
)

type Runner struct {
	cfg       config.Config
	loc       *time.Location
	control   *controlcenter.Client
	dbapi     *dbapi.Client
	vod       *vod.Client
	calib     *calibration.Client
	artifacts *artifact.LocalStore
	results   result.Sink
	history   result.CalibrationHistory
	retry     resilience.RetryConfig
	breakers  map[string]*resilience.CircuitBreaker
}

type querySpan struct {
	QueryStart  time.Time
	QueryEnd    time.Time
	AcceptStart time.Time
	AcceptEnd   time.Time
}

func NewRunner(cfg config.Config, sink result.Sink) (*Runner, error) {
	loc, err := cfg.Location()
	if err != nil {
		return nil, err
	}
	breakerOpenFor := time.Duration(cfg.Resilience.CircuitOpenSeconds) * time.Second
	breakerThreshold := cfg.Resilience.CircuitFailureThreshold
	history, _ := sink.(result.CalibrationHistory)
	return &Runner{
		cfg:       cfg,
		loc:       loc,
		control:   controlcenter.New(cfg.ControlCenter),
		dbapi:     dbapi.New(cfg.DBAPI, loc),
		vod:       vod.New(cfg.VOD),
		calib:     calibration.New(cfg.Calibration),
		artifacts: artifact.NewLocalStore(cfg.Artifact),
		results:   sink,
		history:   history,
		retry: resilience.RetryConfig{
			Attempts:  cfg.Resilience.RetryAttempts,
			BaseDelay: time.Duration(cfg.Resilience.RetryBaseDelayMillis) * time.Millisecond,
		},
		breakers: map[string]*resilience.CircuitBreaker{
			"controlCenter": resilience.NewCircuitBreaker("controlCenter", breakerThreshold, breakerOpenFor),
			"dbapi":         resilience.NewCircuitBreaker("dbapi", breakerThreshold, breakerOpenFor),
			"vod":           resilience.NewCircuitBreaker("vod", breakerThreshold, breakerOpenFor),
			"calibration":   resilience.NewCircuitBreaker("calibration", breakerThreshold, breakerOpenFor),
		},
	}, nil
}

func (r *Runner) RunOnce(ctx context.Context, scanDate string) error {
	start, end, err := r.scanWindow(scanDate)
	if err != nil {
		return err
	}
	slog.Info("scan window", "start", start.Format(time.RFC3339), "end", end.Format(time.RFC3339))

	devices, err := r.loadTargetDevices(ctx)
	if err != nil {
		return err
	}
	if r.cfg.Scan.MaxDevices > 0 && len(devices) > r.cfg.Scan.MaxDevices {
		devices = devices[:r.cfg.Scan.MaxDevices]
	}
	slog.Info("target devices loaded", "count", len(devices))

	var errs []error
	for _, device := range devices {
		skip, message, err := r.shouldSkipRecentCalibration(ctx, device)
		if err != nil {
			slog.Error("recent calibration check failed", "deviceId", device.DeviceID, "error", err)
			if saveErr := r.saveDeviceStatus(ctx, device, start, model.StatusFailed, err.Error()); saveErr != nil {
				errs = append(errs, errors.Join(err, saveErr))
			} else {
				errs = append(errs, err)
			}
			continue
		}
		if skip {
			slog.Info("device skipped because recent calibration exists", "deviceId", device.DeviceID, "reason", message)
			if err := r.saveDeviceStatus(ctx, device, start, model.StatusSkippedRecentCalibration, message); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if err := r.scanDevice(ctx, device, start, end); err != nil {
			slog.Error("scan device failed", "deviceId", device.DeviceID, "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) shouldSkipRecentCalibration(ctx context.Context, device model.Device) (bool, string, error) {
	if r.calib == nil || !r.calib.Enabled() || !r.cfg.Calibration.DedupEnabled {
		return false, "", nil
	}
	days := r.cfg.Calibration.DedupDays
	if days <= 0 {
		return false, "", nil
	}
	if r.history == nil {
		return false, "", errors.New("calibration dedup is enabled but result history is not queryable")
	}
	since := time.Now().In(r.loc).AddDate(0, 0, -days)
	recent, ok, err := r.history.RecentCalibration(ctx, device.DeviceID, since)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", nil
	}
	message := fmt.Sprintf("recent calibration exists within %d days: task=%s status=%s createdAt=%s clipStart=%s",
		days,
		recent.CalibrationTaskID,
		recent.CalibrationStatus,
		recent.CreatedAt.In(r.loc).Format(time.RFC3339),
		recent.ClipStart.In(r.loc).Format(time.RFC3339),
	)
	return true, message, nil
}

func (r *Runner) scanDevice(ctx context.Context, device model.Device, start, end time.Time) error {
	sawSamples := false
	var lastErr error
	for _, span := range r.querySpans(start, end) {
		samples, err := r.fetchSamples(ctx, device, span.QueryStart, span.QueryEnd)
		if err != nil {
			return r.saveDeviceStatus(ctx, device, start, model.StatusFailed, err.Error())
		}
		if len(samples) == 0 {
			continue
		}
		sawSamples = true
		windows := rule.FindWindows(samples, r.ruleConfig())
		for _, window := range windows {
			if window.Start.Before(span.AcceptStart) || !window.Start.Before(span.AcceptEnd) {
				continue
			}
			record, err := r.buildSuccessRecord(ctx, device, start, window)
			if err != nil {
				lastErr = err
				continue
			}
			if err := r.results.Save(ctx, record); err != nil {
				return err
			}
			slog.Info("device matched", "deviceId", device.DeviceID, "clipStart", window.Start.Format(time.RFC3339))
			return nil
		}
	}
	if lastErr != nil {
		return r.saveDeviceStatus(ctx, device, start, model.StatusFailed, lastErr.Error())
	}
	if !sawSamples {
		return r.saveDeviceStatus(ctx, device, start, model.StatusNoTelemetry, "no telemetry samples")
	}
	if r.cfg.Scan.RecordNoMatch {
		return r.saveDeviceStatus(ctx, device, start, model.StatusNoMatch, "no qualifying 20s window")
	}
	return nil
}

func (r *Runner) querySpans(start, end time.Time) []querySpan {
	if !end.After(start) {
		return nil
	}
	chunk := time.Duration(r.cfg.Scan.QueryChunkMinutes) * time.Minute
	total := end.Sub(start)
	if chunk <= 0 || chunk >= total {
		return []querySpan{{
			QueryStart:  start,
			QueryEnd:    end,
			AcceptStart: start,
			AcceptEnd:   end,
		}}
	}
	overlap := r.ruleDuration() + r.maxSampleGap()
	var spans []querySpan
	for acceptStart := start; acceptStart.Before(end); acceptStart = acceptStart.Add(chunk) {
		acceptEnd := acceptStart.Add(chunk)
		if acceptEnd.After(end) {
			acceptEnd = end
		}
		queryEnd := acceptEnd.Add(overlap)
		if queryEnd.After(end) {
			queryEnd = end
		}
		spans = append(spans, querySpan{
			QueryStart:  acceptStart,
			QueryEnd:    queryEnd,
			AcceptStart: acceptStart,
			AcceptEnd:   acceptEnd,
		})
	}
	return spans
}

func (r *Runner) fetchSamples(ctx context.Context, device model.Device, start, end time.Time) ([]model.Sample, error) {
	if strings.TrimSpace(r.cfg.DBAPI.SampleFile) != "" {
		return r.loadSampleFile(ctx, device.DeviceID, start, end)
	}
	var samples []model.Sample
	err := r.externalCall(ctx, "dbapi", func(ctx context.Context) error {
		var err error
		samples, err = r.dbapi.FetchSamples(ctx, device, start, end)
		return err
	})
	return samples, err
}

func (r *Runner) buildSuccessRecord(ctx context.Context, device model.Device, scanStart time.Time, window model.Window) (model.ScanResult, error) {
	clipWindow := window
	clipWindow.End = r.clipEnd(window)
	var snapshot model.PropertySnapshot
	if strings.TrimSpace(r.cfg.DBAPI.SampleFile) != "" && len(window.FirstSample.RawProperties) > 0 {
		snapshot = fallbackSnapshot(device, window)
	} else {
		err := r.externalCall(ctx, "dbapi", func(ctx context.Context) error {
			var err error
			snapshot, err = r.dbapi.FetchPropertiesAt(ctx, device, window.Start)
			return err
		})
		if err != nil || len(snapshot.Properties) == 0 {
			if len(window.FirstSample.RawProperties) == 0 {
				return model.ScanResult{}, fmt.Errorf("fetch properties at clip_start: %w", err)
			}
			snapshot = fallbackSnapshot(device, window)
		}
	}
	outputPrefix := fmt.Sprintf("uav-downward-photo/%s/%s/%s-%s",
		window.Start.Format("20060102"),
		device.DeviceID,
		window.Start.Format("150405"),
		clipWindow.End.Format("150405"),
	)
	propertyURL, err := r.artifacts.WriteProperties(ctx, device, clipWindow, snapshot, r.cfg.RuleVersion)
	if err != nil {
		return model.ScanResult{}, err
	}
	var clipURL string
	err = r.externalCall(ctx, "vod", func(ctx context.Context) error {
		var err error
		clipURL, err = r.vod.CreateClip(ctx, device, clipWindow, outputPrefix)
		return err
	})
	if err != nil {
		return model.ScanResult{}, err
	}
	status := model.StatusSuccess
	var calibrationTaskID string
	var calibrationStatus string
	var calibrationCameraSN string
	var calibrationCameraMode string
	var calibrationError string
	var errorMessage string
	if r.calib.Enabled() {
		params, err := calibrationParameters(snapshot.Properties, device.DeviceID)
		if err != nil {
			status = model.StatusFailed
			calibrationStatus = "FAILED"
			calibrationError = err.Error()
			errorMessage = err.Error()
		} else {
			var task calibration.Task
			err := r.externalCall(ctx, "calibration", func(ctx context.Context) error {
				var err error
				task, err = r.calib.CreateTask(ctx, clipURL, params)
				return err
			})
			if err != nil {
				status = model.StatusFailed
				calibrationStatus = "FAILED"
				calibrationError = err.Error()
				errorMessage = err.Error()
			} else {
				calibrationTaskID = task.TaskID
				calibrationStatus = firstNonEmpty(task.Status, "SUBMITTED")
				calibrationCameraSN = task.CameraSN
				calibrationCameraMode = task.CameraMode
			}
		}
	}
	now := time.Now().In(r.loc)
	modelValue, _ := device.TargetModel(r.cfg.TargetModelSet())
	videoID := firstNonEmpty(window.FirstSample.VideoID, device.VideoID)
	payloadIndex := firstNonEmpty(window.FirstSample.PayloadIndex, device.PayloadIndex)
	return model.ScanResult{
		ScanDate:                   scanStart.Format("2006-01-02"),
		ProductKey:                 device.ProductKey,
		DeviceID:                   device.DeviceID,
		SN:                         device.SN,
		DeviceModel:                modelValue,
		VideoID:                    videoID,
		PayloadIndex:               payloadIndex,
		ClipStart:                  window.Start,
		ClipEnd:                    clipWindow.End,
		ClipURL:                    clipURL,
		CalibrationTaskID:          calibrationTaskID,
		CalibrationStatus:          calibrationStatus,
		CalibrationCameraSN:        calibrationCameraSN,
		CalibrationCameraMode:      calibrationCameraMode,
		CalibrationError:           calibrationError,
		ControlCenterPropertiesURL: propertyURL,
		PropertySnapshotTime:       snapshot.SnapshotAt,
		ConditionSummary:           window.Summary,
		RuleVersion:                r.cfg.RuleVersion,
		Status:                     status,
		ErrorMessage:               errorMessage,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}, nil
}

func fallbackSnapshot(device model.Device, window model.Window) model.PropertySnapshot {
	return model.PropertySnapshot{
		DeviceID:   device.DeviceID,
		SnapshotAt: window.FirstSample.SampleTime,
		Properties: window.FirstSample.RawProperties,
		Source:     "history-sample-fallback",
	}
}

func (r *Runner) clipEnd(window model.Window) time.Time {
	if r.cfg.VOD.ClipDurationSeconds <= 0 {
		return window.End
	}
	configuredEnd := window.Start.Add(time.Duration(r.cfg.VOD.ClipDurationSeconds) * time.Second)
	if configuredEnd.Before(window.End) {
		return window.End
	}
	return configuredEnd
}

func (r *Runner) saveDeviceStatus(ctx context.Context, device model.Device, scanStart time.Time, status, message string) error {
	if r.results == nil {
		return nil
	}
	now := time.Now().In(r.loc)
	modelValue, _ := device.TargetModel(r.cfg.TargetModelSet())
	record := model.ScanResult{
		ScanDate:     scanStart.Format("2006-01-02"),
		ProductKey:   device.ProductKey,
		DeviceID:     device.DeviceID,
		SN:           device.SN,
		DeviceModel:  modelValue,
		RuleVersion:  r.cfg.RuleVersion,
		Status:       status,
		ErrorMessage: message,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return r.results.Save(ctx, record)
}

func (r *Runner) externalCall(ctx context.Context, dependency string, fn func(context.Context) error) error {
	breaker := r.breakers[dependency]
	if breaker == nil {
		return fn(ctx)
	}
	return breaker.Execute(ctx, r.retry, fn)
}

func (r *Runner) loadTargetDevices(ctx context.Context) ([]model.Device, error) {
	devices, source, err := r.loadDeviceCandidates(ctx)
	if err != nil {
		return nil, err
	}
	slog.Info("device candidates loaded", "source", source, "count", len(devices))
	return r.filterTargetDevices(devices)
}

func (r *Runner) loadDeviceCandidates(ctx context.Context) ([]model.Device, string, error) {
	if r.control != nil && r.control.Enabled() {
		var remoteDevices []model.Device
		err := r.externalCall(ctx, "controlCenter", func(ctx context.Context) error {
			var err error
			remoteDevices, err = r.control.ListDevices(ctx)
			return err
		})
		if err == nil && len(remoteDevices) > 0 {
			return remoteDevices, "controlCenter", nil
		}
		if err != nil {
			slog.Warn("control-center device list failed, falling back", "error", err)
		} else {
			slog.Warn("control-center device list returned no devices, falling back")
		}
	}
	if strings.TrimSpace(r.cfg.DBAPI.DeviceListPath) != "" {
		var remoteDevices []model.Device
		err := r.externalCall(ctx, "dbapi", func(ctx context.Context) error {
			var err error
			remoteDevices, err = r.dbapi.ListDevices(ctx)
			return err
		})
		if err == nil && len(remoteDevices) > 0 {
			return remoteDevices, "dbapi", nil
		}
		if err != nil {
			slog.Warn("dbapi device list failed, falling back", "error", err)
		} else {
			slog.Warn("dbapi device list returned no devices, falling back")
		}
	}
	if len(r.cfg.Devices) > 0 {
		return r.cfg.Devices, "static", nil
	}
	return nil, "", errors.New("no device source configured: enable controlCenter, configure dbapi.deviceListPath, or set static devices")
}

func (r *Runner) filterTargetDevices(devices []model.Device) ([]model.Device, error) {
	targets := r.cfg.TargetModelSet()
	targetGimbalTypes := r.cfg.TargetGimbalTypeSet()
	filtered := make([]model.Device, 0, len(devices))
	for _, device := range devices {
		if strings.TrimSpace(device.DeviceID) == "" {
			continue
		}
		if _, ok := device.TargetModel(targets); ok {
			filtered = append(filtered, device)
			continue
		}
		if r.cfg.Scan.PreciseDevicePrefilter && deviceLooksLikeTarget(device, targets, targetGimbalTypes) {
			filtered = append(filtered, device)
			continue
		}
		if len(targetGimbalTypes) > 0 {
			if !r.cfg.Scan.PreciseDevicePrefilter {
				filtered = append(filtered, device)
				continue
			}
		}
	}
	if len(filtered) == 0 {
		return nil, errors.New("no target devices found by targetModels or rule.targetGimbalTypes")
	}
	return filtered, nil
}

func deviceLooksLikeTarget(device model.Device, targetModels, targetGimbalTypes map[string]struct{}) bool {
	if _, ok := device.TargetModel(targetModels); ok {
		return true
	}
	if matchesTargetGimbalSignal(device.Model, targetGimbalTypes) {
		return true
	}
	for _, key := range []string{"GIMBAL_TYPE", "DEVICE_MODEL", "DEVICE_TYPE"} {
		if matchesTargetGimbalSignal(device.Tags[key], targetGimbalTypes) {
			return true
		}
	}
	return matchesTargetName(device.DeviceName, targetModels, targetGimbalTypes)
}

func matchesTargetGimbalSignal(value string, targets map[string]struct{}) bool {
	normalized := config.NormalizeGimbalType(value)
	if normalized == "" {
		return false
	}
	_, ok := targets[normalized]
	return ok
}

func matchesTargetName(name string, targetModels, targetGimbalTypes map[string]struct{}) bool {
	normalizedName := config.NormalizeGimbalType(name)
	if normalizedName == "" {
		return false
	}
	for target := range targetModels {
		if target != "" && strings.Contains(normalizedName, config.NormalizeGimbalType(target)) {
			return true
		}
	}
	for target := range targetGimbalTypes {
		token := strings.TrimSuffix(target, "_CAMERA")
		if token != "" && strings.Contains(normalizedName, token) {
			return true
		}
	}
	return false
}

func (r *Runner) scanWindow(scanDate string) (time.Time, time.Time, error) {
	var day time.Time
	var err error
	if strings.TrimSpace(scanDate) == "" {
		now := time.Now().In(r.loc)
		day = time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, r.loc)
	} else {
		day, err = time.ParseInLocation("2006-01-02", scanDate, r.loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse scan date: %w", err)
		}
	}
	start, err := combineDateAndClock(day, r.cfg.Scan.StartTime, r.loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := combineDateAndClock(day, r.cfg.Scan.EndTime, r.loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, errors.New("scan end time must be after start time")
	}
	return start, end, nil
}

func (r *Runner) ruleConfig() rule.Config {
	return rule.Config{
		Duration:            r.ruleDuration(),
		MaxSampleGap:        r.maxSampleGap(),
		CameraMode:          r.cfg.Rule.CameraMode,
		LensType:            r.cfg.Rule.LensType,
		TargetGimbalTypes:   r.cfg.TargetGimbalTypeSet(),
		GimbalPitchMin:      r.cfg.Rule.GimbalPitchMin,
		GimbalPitchMax:      r.cfg.Rule.GimbalPitchMax,
		RequireFlightStatus: r.cfg.Rule.RequireFlightStatus,
		MinHeightMeters:     r.cfg.Rule.MinHeightMeters,
		RequireFlyTime:      r.cfg.Rule.RequireFlyTime,
		MinTravelMeters:     r.cfg.Rule.MinTravelMeters,
	}
}

func (r *Runner) ruleDuration() time.Duration {
	if r.cfg.Rule.DurationSeconds <= 0 {
		return 20 * time.Second
	}
	return time.Duration(r.cfg.Rule.DurationSeconds) * time.Second
}

func (r *Runner) maxSampleGap() time.Duration {
	if r.cfg.Rule.MaxSampleGapSeconds <= 0 {
		return 3 * time.Second
	}
	return time.Duration(r.cfg.Rule.MaxSampleGapSeconds) * time.Second
}

func combineDateAndClock(day time.Time, clock string, loc *time.Location) (time.Time, error) {
	parsed, err := time.ParseInLocation("15:04:05", clock, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse scan clock %q: %w", clock, err)
	}
	return time.Date(day.Year(), day.Month(), day.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, loc), nil
}

type sampleFileRow struct {
	SampleTime      string          `json:"sampleTime"`
	CameraMode      string          `json:"cameraMode"`
	GimbalPitch     float64         `json:"gimbalPitch"`
	LensType        string          `json:"lensType"`
	FlightStatus    string          `json:"flightStatus"`
	TaskStatus      string          `json:"taskStatus"`
	CameraSn        string          `json:"cameraSn"`
	SN              string          `json:"sn"`
	GimbalType      string          `json:"gimbalType"`
	Height          float64         `json:"height"`
	FlyTime         int64           `json:"flyTime"`
	HorizontalSpeed float64         `json:"horizontalSpeed"`
	Latitude        float64         `json:"latitude"`
	Longitude       float64         `json:"longitude"`
	VideoID         string          `json:"videoId"`
	PayloadIndex    string          `json:"payloadIndex"`
	RawProperties   json.RawMessage `json:"properties"`
}

func (r *Runner) loadSampleFile(ctx context.Context, deviceID string, start, end time.Time) ([]model.Sample, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	data, err := os.ReadFile(r.cfg.DBAPI.SampleFile)
	if err != nil {
		return nil, err
	}
	var byDevice map[string][]sampleFileRow
	if err := json.Unmarshal(data, &byDevice); err != nil {
		return nil, err
	}
	rows := byDevice[deviceID]
	samples := make([]model.Sample, 0, len(rows))
	for _, row := range rows {
		ts, err := parseSampleTime(row.SampleTime, r.loc)
		if err != nil {
			return nil, err
		}
		if ts.Before(start) || !ts.Before(end) {
			continue
		}
		raw := row.RawProperties
		if len(raw) == 0 {
			raw, _ = json.Marshal(map[string]any{
				deviceID: map[string]any{
					"cameraMode":      row.CameraMode,
					"gimbalPitch":     row.GimbalPitch,
					"lensType":        row.LensType,
					"flightStatus":    row.FlightStatus,
					"taskStatus":      row.TaskStatus,
					"cameraSn":        row.CameraSn,
					"sn":              row.SN,
					"gimbalType":      row.GimbalType,
					"height":          row.Height,
					"flyTime":         row.FlyTime,
					"horizontalSpeed": row.HorizontalSpeed,
					"latitude":        row.Latitude,
					"longitude":       row.Longitude,
				},
			})
		}
		hasPosition := row.Latitude >= -90 && row.Latitude <= 90 && row.Longitude >= -180 && row.Longitude <= 180 && (row.Latitude != 0 || row.Longitude != 0)
		samples = append(samples, model.Sample{
			DeviceID:        deviceID,
			SampleTime:      ts,
			CameraMode:      row.CameraMode,
			GimbalType:      row.GimbalType,
			GimbalPitch:     row.GimbalPitch,
			LensType:        row.LensType,
			FlightStatus:    row.FlightStatus,
			TaskStatus:      row.TaskStatus,
			Height:          row.Height,
			FlyTime:         row.FlyTime,
			HorizontalSpeed: row.HorizontalSpeed,
			Latitude:        row.Latitude,
			Longitude:       row.Longitude,
			HasPosition:     hasPosition,
			VideoID:         row.VideoID,
			PayloadIndex:    row.PayloadIndex,
			RawProperties:   raw,
		})
	}
	return samples, nil
}

func parseSampleTime(value string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
	} {
		if ts, err := time.ParseInLocation(layout, value, loc); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse sample time %q", value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func calibrationParameters(raw json.RawMessage, deviceID string) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("decode calibration properties: %w", err)
	}
	if scoped, ok := root[deviceID]; ok {
		if obj, ok := scoped.(map[string]any); ok {
			return obj, nil
		}
		data, err := json.Marshal(scoped)
		if err != nil {
			return nil, err
		}
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err == nil {
			return obj, nil
		}
	}
	return root, nil
}
