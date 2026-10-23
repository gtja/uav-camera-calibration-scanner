alter table uav_downward_photo_clip
  add column calibration_task_id varchar(128) null after clip_url,
  add column calibration_status varchar(32) null after calibration_task_id,
  add column calibration_camera_sn varchar(128) null after calibration_status,
  add column calibration_camera_mode varchar(32) null after calibration_camera_sn,
  add column calibration_error varchar(1024) null after calibration_camera_mode,
  add key idx_calibration_status (calibration_status);
