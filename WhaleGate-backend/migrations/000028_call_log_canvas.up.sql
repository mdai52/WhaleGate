-- 调用日志补充画布导出的计量字段。
--
-- 画布导出是本机渲染，没有上游也就没有 token，但它确实占用 CPU 与内存，
-- 需要在调用历史里可查：像素数对应渲染开销，字节数对应存储开销。

ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS canvas_count integer NOT NULL DEFAULT 0;
ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS canvas_pixels bigint NOT NULL DEFAULT 0;
ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS canvas_bytes bigint NOT NULL DEFAULT 0;

COMMENT ON COLUMN call_logs.canvas_count IS '本次调用导出的画布张数；非画布导出为 0';
COMMENT ON COLUMN call_logs.canvas_pixels IS '导出图的总像素数，画布导出的计量单位（对应渲染开销）';
COMMENT ON COLUMN call_logs.canvas_bytes IS '导出文件字节数';
