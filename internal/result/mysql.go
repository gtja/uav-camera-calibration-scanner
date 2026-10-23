package result

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"jingansi/uav-camera-calibration-scanner/internal/model"
)

var tableNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type MySQLSink struct {
	db    *sql.DB
	table string
}

func NewMySQLSink(dsn, table string) (*MySQLSink, error) {
	if dsn == "" {
		return nil, nil
	}
	if !tableNamePattern.MatchString(table) {
		return nil, fmt.Errorf("unsafe mysql table name %q", table)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return &MySQLSink{db: db, table: table}, nil
}

func (s *MySQLSink) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *MySQLSink) Save(ctx context.Context, record model.ScanResult) error {
	summary, err := json.Marshal(record.ConditionSummary)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`insert into %s
(scan_date, product_key, device_id, sn, video_id, payload_index, clip_start, clip_end,
 clip_url, calibration_task_id, calibration_status, calibration_camera_sn,
 calibration_camera_mode, calibration_error, control_center_properties_url,
 property_snapshot_time, condition_summary, rule_version, status, error_message,
 created_at, updated_at)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on duplicate key update
 clip_url=values(clip_url),
 calibration_task_id=values(calibration_task_id),
 calibration_status=values(calibration_status),
 calibration_camera_sn=values(calibration_camera_sn),
 calibration_camera_mode=values(calibration_camera_mode),
 calibration_error=values(calibration_error),
 control_center_properties_url=values(control_center_properties_url),
 property_snapshot_time=values(property_snapshot_time),
 condition_summary=values(condition_summary),
 status=values(status),
 error_message=values(error_message),
 updated_at=values(updated_at)`, s.table)
	_, err = s.db.ExecContext(ctx, query,
		record.ScanDate,
		record.ProductKey,
		record.DeviceID,
		record.SN,
		record.VideoID,
		record.PayloadIndex,
		record.ClipStart,
		record.ClipEnd,
		record.ClipURL,
		record.CalibrationTaskID,
		record.CalibrationStatus,
		record.CalibrationCameraSN,
		record.CalibrationCameraMode,
		record.CalibrationError,
		record.ControlCenterPropertiesURL,
		record.PropertySnapshotTime,
		string(summary),
		record.RuleVersion,
		record.Status,
		record.ErrorMessage,
		record.CreatedAt,
		record.UpdatedAt,
	)
	return err
}

func (s *MySQLSink) RecentCalibration(ctx context.Context, deviceID string, since time.Time) (RecentCalibration, bool, error) {
	if s == nil || s.db == nil {
		return RecentCalibration{}, false, nil
	}
	query := fmt.Sprintf(`select device_id, clip_start, calibration_task_id, calibration_status, created_at
from %s
where device_id=?
  and status=?
  and calibration_task_id is not null
  and calibration_task_id <> ''
  and created_at >= ?
order by created_at desc
limit 1`, s.table)
	var recent RecentCalibration
	var calibrationTaskID sql.NullString
	var calibrationStatus sql.NullString
	err := s.db.QueryRowContext(ctx, query, deviceID, model.StatusSuccess, since).Scan(
		&recent.DeviceID,
		&recent.ClipStart,
		&calibrationTaskID,
		&calibrationStatus,
		&recent.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RecentCalibration{}, false, nil
	}
	if err != nil {
		return RecentCalibration{}, false, err
	}
	recent.CalibrationTaskID = calibrationTaskID.String
	recent.CalibrationStatus = calibrationStatus.String
	return recent, true, nil
}
