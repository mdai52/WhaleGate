-- 画布工作台：画布与导出任务。

CREATE TABLE IF NOT EXISTS canvases (
  id          bigserial PRIMARY KEY,
  user_id     bigint       NOT NULL,
  name        varchar(128) NOT NULL DEFAULT '未命名画布',
  width       integer      NOT NULL DEFAULT 0,
  height      integer      NOT NULL DEFAULT 0,
  background  varchar(32)  NOT NULL DEFAULT '#FFFFFF',
  payload     text         NOT NULL DEFAULT '{"nodes":[]}',
  version     integer      NOT NULL DEFAULT 1,
  created_at  timestamptz  NOT NULL DEFAULT now(),
  updated_at  timestamptz  NOT NULL DEFAULT now()
);

COMMENT ON TABLE canvases IS '画布工作台：用户创建的画布（内容以 JSON 文本存于 payload）';
COMMENT ON COLUMN canvases.payload IS '画布内容 JSON：{nodes:[...]}，节点类型 rect/ellipse/line/text/image';
COMMENT ON COLUMN canvases.version IS '每次保存自增，前端可用它判断是否需要重新拉取';

CREATE INDEX IF NOT EXISTS idx_canvases_user ON canvases (user_id, updated_at DESC);

-- ---------------------------------------------------------------- canvas_exports

CREATE TABLE IF NOT EXISTS canvas_exports (
  id            bigserial PRIMARY KEY,
  canvas_id     bigint       NOT NULL,
  user_id       bigint       NOT NULL,
  format        varchar(16)  NOT NULL DEFAULT 'png',
  scale         integer      NOT NULL DEFAULT 1,
  status        smallint     NOT NULL DEFAULT 0,
  progress      integer      NOT NULL DEFAULT 0,
  storage_kind  varchar(16)  NOT NULL DEFAULT '',
  storage_key   varchar(512) NOT NULL DEFAULT '',
  bytes         bigint       NOT NULL DEFAULT 0,
  width         integer      NOT NULL DEFAULT 0,
  height        integer      NOT NULL DEFAULT 0,
  error_message text         NOT NULL DEFAULT '',
  latency_ms    integer      NOT NULL DEFAULT 0,
  queued_at     timestamptz  NOT NULL DEFAULT now(),
  started_at    timestamptz,
  finished_at   timestamptz
);

COMMENT ON TABLE canvas_exports IS '画布工作台：画布导出任务（异步渲染）';
COMMENT ON COLUMN canvas_exports.status IS '0 排队中 / 1 渲染中 / 2 成功 / 3 失败';
COMMENT ON COLUMN canvas_exports.progress IS '0-100 的渲染进度，用于前端展示';
COMMENT ON COLUMN canvas_exports.storage_key IS '导出结果在 studio 存储中的对象键';
COMMENT ON COLUMN canvas_exports.bytes IS '导出文件字节数';

CREATE INDEX IF NOT EXISTS idx_canvas_exports_user ON canvas_exports (user_id, queued_at DESC);
CREATE INDEX IF NOT EXISTS idx_canvas_exports_status ON canvas_exports (status);
