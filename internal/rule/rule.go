package rule

import (
	"math"
	"sort"
	"strings"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/model"
)

type Config struct {
	Duration            time.Duration
	MaxSampleGap        time.Duration
	CameraMode          string
	LensType            string
	TargetGimbalTypes   map[string]struct{}
	GimbalPitchMin      float64
	GimbalPitchMax      float64
	RequireFlightStatus bool
	MinHeightMeters     float64
	RequireFlyTime      bool
	MinTravelMeters     float64
}

func FindWindows(samples []model.Sample, cfg Config) []model.Window {
	if len(samples) == 0 {
		return nil
	}
	if cfg.Duration <= 0 {
		cfg.Duration = 20 * time.Second
	}
	if cfg.MaxSampleGap <= 0 {
		cfg.MaxSampleGap = 3 * time.Second
	}
	ordered := append([]model.Sample(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].SampleTime.Before(ordered[j].SampleTime)
	})

	var windows []model.Window
	var segment []model.Sample
	flush := func() {
		windows = append(windows, segmentWindows(segment, cfg)...)
		segment = nil
	}

	for _, sample := range ordered {
		if !Valid(sample, cfg) {
			flush()
			continue
		}
		if len(segment) > 0 {
			gap := sample.SampleTime.Sub(segment[len(segment)-1].SampleTime)
			if gap > cfg.MaxSampleGap {
				flush()
			}
		}
		segment = append(segment, sample)
	}
	flush()
	return windows
}

func Valid(sample model.Sample, cfg Config) bool {
	if strings.TrimSpace(sample.CameraMode) != strings.TrimSpace(cfg.CameraMode) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(sample.LensType), strings.TrimSpace(cfg.LensType)) {
		return false
	}
	if len(cfg.TargetGimbalTypes) > 0 {
		if _, ok := cfg.TargetGimbalTypes[config.NormalizeGimbalType(sample.GimbalType)]; !ok {
			return false
		}
	}
	if sample.GimbalPitch < cfg.GimbalPitchMin || sample.GimbalPitch > cfg.GimbalPitchMax {
		return false
	}
	if cfg.RequireFlightStatus {
		flightStatus := strings.TrimSpace(sample.FlightStatus)
		if strings.EqualFold(flightStatus, "standby") || flightStatus == "" {
			return false
		}
	}
	if sample.Height < cfg.MinHeightMeters {
		return false
	}
	if cfg.RequireFlyTime && sample.FlyTime <= 0 {
		return false
	}
	return true
}

func segmentWindows(segment []model.Sample, cfg Config) []model.Window {
	if len(segment) == 0 {
		return nil
	}
	var windows []model.Window
	endIdx := 0
	for startIdx := range segment {
		first := segment[startIdx]
		if segment[len(segment)-1].SampleTime.Sub(first.SampleTime) < cfg.Duration {
			break
		}
		if endIdx < startIdx {
			endIdx = startIdx
		}
		clipEnd := first.SampleTime.Add(cfg.Duration)
		for endIdx+1 < len(segment) && !segment[endIdx+1].SampleTime.After(clipEnd) {
			endIdx++
		}
		windowSamples := segment[startIdx : endIdx+1]
		if window, ok := buildWindow(windowSamples, cfg); ok {
			windows = append(windows, window)
		}
	}
	return windows
}

func buildWindow(windowSamples []model.Sample, cfg Config) (model.Window, bool) {
	if len(windowSamples) == 0 {
		return model.Window{}, false
	}
	first := windowSamples[0]
	clipEnd := first.SampleTime.Add(cfg.Duration)
	travelMeters, ok := windowTravelMeters(windowSamples)
	if cfg.MinTravelMeters > 0 && (!ok || travelMeters < cfg.MinTravelMeters) {
		return model.Window{}, false
	}
	last := windowSamples[len(windowSamples)-1]
	return model.Window{
		Start:       first.SampleTime,
		End:         clipEnd,
		DeviceID:    first.DeviceID,
		SampleCount: len(windowSamples),
		FirstSample: first,
		LastSample:  last,
		Summary: map[string]interface{}{
			"cameraMode":          cfg.CameraMode,
			"lensType":            cfg.LensType,
			"targetGimbalTypes":   sortedKeys(cfg.TargetGimbalTypes),
			"gimbalType":          first.GimbalType,
			"gimbalPitchMin":      cfg.GimbalPitchMin,
			"gimbalPitchMax":      cfg.GimbalPitchMax,
			"minHeightMeters":     cfg.MinHeightMeters,
			"requireFlyTime":      cfg.RequireFlyTime,
			"minTravelMeters":     cfg.MinTravelMeters,
			"travelMeters":        travelMeters,
			"maxSampleGapSeconds": cfg.MaxSampleGap.Seconds(),
			"durationSeconds":     cfg.Duration.Seconds(),
			"firstSampleTime":     first.SampleTime.Format(time.RFC3339Nano),
			"lastSampleTime":      last.SampleTime.Format(time.RFC3339Nano),
			"sampleCount":         len(windowSamples),
		},
	}, true
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func windowTravelMeters(samples []model.Sample) (float64, bool) {
	if len(samples) < 2 {
		return 0, false
	}
	for _, sample := range samples {
		if !sample.HasPosition {
			return 0, false
		}
	}
	first := samples[0]
	last := samples[len(samples)-1]
	if first.SampleTime.Equal(last.SampleTime) {
		return 0, false
	}
	return haversineMeters(first.Latitude, first.Longitude, last.Latitude, last.Longitude), true
}

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0
	toRad := func(deg float64) float64 {
		return deg * math.Pi / 180
	}
	phi1 := toRad(lat1)
	phi2 := toRad(lat2)
	dPhi := toRad(lat2 - lat1)
	dLambda := toRad(lon2 - lon1)
	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) + math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)
	return earthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
