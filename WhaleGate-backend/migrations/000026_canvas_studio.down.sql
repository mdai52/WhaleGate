-- 回滚画布工作台。

DROP INDEX IF EXISTS idx_canvas_exports_status;
DROP INDEX IF EXISTS idx_canvas_exports_user;
DROP TABLE IF EXISTS canvas_exports;

DROP INDEX IF EXISTS idx_canvases_user;
DROP TABLE IF EXISTS canvases;
