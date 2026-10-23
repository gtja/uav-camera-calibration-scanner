package result

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/model"
)

type Sink interface {
	Save(context.Context, model.ScanResult) error
}

type RecentCalibration struct {
	DeviceID          string
	ClipStart         time.Time
	CalibrationTaskID string
	CalibrationStatus string
	CreatedAt         time.Time
}

type CalibrationHistory interface {
	RecentCalibration(context.Context, string, time.Time) (RecentCalibration, bool, error)
}

type CompositeSink struct {
	sinks []Sink
}

func NewComposite(sinks ...Sink) *CompositeSink {
	filtered := make([]Sink, 0, len(sinks))
	for _, sink := range sinks {
		if sink != nil {
			filtered = append(filtered, sink)
		}
	}
	return &CompositeSink{sinks: filtered}
}

func (s *CompositeSink) Save(ctx context.Context, record model.ScanResult) error {
	for _, sink := range s.sinks {
		if err := sink.Save(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (s *CompositeSink) RecentCalibration(ctx context.Context, deviceID string, since time.Time) (RecentCalibration, bool, error) {
	var best RecentCalibration
	found := false
	for _, sink := range s.sinks {
		history, ok := sink.(CalibrationHistory)
		if !ok {
			continue
		}
		recent, ok, err := history.RecentCalibration(ctx, deviceID, since)
		if err != nil {
			return RecentCalibration{}, false, err
		}
		if !ok {
			continue
		}
		if !found || recent.CreatedAt.After(best.CreatedAt) {
			best = recent
			found = true
		}
	}
	return best, found, nil
}

type JSONLSink struct {
	path string
}

func NewJSONLSink(path string) *JSONLSink {
	if path == "" {
		return nil
	}
	return &JSONLSink{path: path}
}

func (s *JSONLSink) Save(ctx context.Context, record model.ScanResult) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *JSONLSink) RecentCalibration(ctx context.Context, deviceID string, since time.Time) (RecentCalibration, bool, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return RecentCalibration{}, false, nil
	}
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return RecentCalibration{}, false, nil
	}
	if err != nil {
		return RecentCalibration{}, false, err
	}
	defer file.Close()

	var best RecentCalibration
	found := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return RecentCalibration{}, false, ctx.Err()
		default:
		}
		var record model.ScanResult
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return RecentCalibration{}, false, err
		}
		if !isRecentCalibrationRecord(record, deviceID, since) {
			continue
		}
		recent := RecentCalibration{
			DeviceID:          record.DeviceID,
			ClipStart:         record.ClipStart,
			CalibrationTaskID: record.CalibrationTaskID,
			CalibrationStatus: record.CalibrationStatus,
			CreatedAt:         record.CreatedAt,
		}
		if !found || recent.CreatedAt.After(best.CreatedAt) {
			best = recent
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		return RecentCalibration{}, false, err
	}
	return best, found, nil
}

func isRecentCalibrationRecord(record model.ScanResult, deviceID string, since time.Time) bool {
	return record.DeviceID == deviceID &&
		record.Status == model.StatusSuccess &&
		strings.TrimSpace(record.CalibrationTaskID) != "" &&
		!record.CreatedAt.Before(since)
}
