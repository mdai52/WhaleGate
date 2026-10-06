-- 回滚生图按张计费：移除生图单价、调用日志张数与作品计费字段。

ALTER TABLE image_works DROP COLUMN IF EXISTS points;
ALTER TABLE image_works DROP COLUMN IF EXISTS self_use;

ALTER TABLE call_logs DROP COLUMN IF EXISTS image_count;

ALTER TABLE model_ratios DROP COLUMN IF EXISTS image_ratio;
