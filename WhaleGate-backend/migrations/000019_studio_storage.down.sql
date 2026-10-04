DELETE FROM app_settings WHERE setting_key IN (
  'studio_storage_driver',
  'studio_local_root',
  'studio_local_base_url',
  'studio_s3_endpoint',
  'studio_s3_region',
  'studio_s3_bucket',
  'studio_s3_prefix',
  'studio_s3_access_key',
  'studio_s3_secret_key',
  'studio_s3_public_base_url',
  'studio_s3_force_path_style'
);
