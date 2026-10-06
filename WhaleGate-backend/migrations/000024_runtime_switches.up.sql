-- 系统设置补齐：把几个只能改 config.yaml、重启才生效的运行时开关搬进数据库。
--
-- 迁移只负责播种默认值（取自原有配置默认），管理员此后在「系统设置」页修改，
-- 即时生效、无需重启；config.yaml 里的同名配置不再参与这些开关的裁决。

INSERT INTO app_settings (setting_key, value) VALUES
  -- 自定义参数透传：能力表之外的参数原样转发给上游（与原 config 默认一致）
  ('param_passthrough', 'true'),
  -- 是否开放注册：false 时仅管理员可创建用户
  ('allow_registration', 'true'),
  -- 视频单次生成最大秒数（防超长任务打爆上游与额度）
  ('video_max_seconds', '20')
ON CONFLICT (setting_key) DO NOTHING;

COMMENT ON COLUMN app_settings.setting_key IS '全局运行时设置键；接入与工作台开关见 000024 迁移说明';
