-- 回滚画布导出的计量字段。

ALTER TABLE call_logs DROP COLUMN IF EXISTS canvas_bytes;
ALTER TABLE call_logs DROP COLUMN IF EXISTS canvas_pixels;
ALTER TABLE call_logs DROP COLUMN IF EXISTS canvas_count;
