-- 绘图工作台存储后端配置：默认本地磁盘，目录留空时回退到 studio.data_dir 配置。
INSERT INTO app_settings (setting_key, value) VALUES
  ('studio_storage_driver', 'local'),
  ('studio_local_root', ''),
  ('studio_local_base_url', ''),
  ('studio_s3_endpoint', ''),
  ('studio_s3_region', ''),
  ('studio_s3_bucket', ''),
  ('studio_s3_prefix', ''),
  ('studio_s3_access_key', ''),
  ('studio_s3_secret_key', ''),
  ('studio_s3_public_base_url', ''),
  ('studio_s3_force_path_style', 'false')
ON CONFLICT (setting_key) DO NOTHING;
