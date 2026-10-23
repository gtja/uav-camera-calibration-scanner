alter table uav_downward_photo_clip
  add key idx_device_calibration_recent (device_id, status, created_at);
