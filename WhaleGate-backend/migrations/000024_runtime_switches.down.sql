-- 回滚运行时开关设置（回到仅 config.yaml 控制）。

DELETE FROM app_settings WHERE setting_key IN (
  'param_passthrough',
  'allow_registration',
  'video_max_seconds'
);
