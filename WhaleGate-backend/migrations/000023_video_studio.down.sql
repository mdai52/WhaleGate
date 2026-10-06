-- 回滚视频工作台。

DROP INDEX IF EXISTS idx_video_works_status;
DROP INDEX IF EXISTS idx_video_works_user;
DROP TABLE IF EXISTS video_works;

DROP INDEX IF EXISTS idx_video_scripts_user;
DROP TABLE IF EXISTS video_scripts;

DELETE FROM prompt_templates WHERE kind = 'video';
DROP INDEX IF EXISTS idx_prompt_templates_kind;
ALTER TABLE prompt_templates DROP COLUMN IF EXISTS kind;

ALTER TABLE call_logs DROP COLUMN IF EXISTS video_seconds;

ALTER TABLE model_ratios DROP COLUMN IF EXISTS video_ratio;
