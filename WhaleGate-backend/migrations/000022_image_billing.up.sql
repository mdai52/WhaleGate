-- 生图按张计费。
--
-- 图片生成没有 token 维度的口径，此前只落调用日志不计费，
-- 工作台与 /v1/images/generations 两个入口都是免费的。
-- 这里补齐三个缺口：
--   1. model_ratios 增加生图单价（点 / 张），沿用已有的通配符兜底与逐模型覆盖；
--   2. call_logs 记录本次生成的张数，便于按张核对扣费；
--   3. image_works 记录本次生成实际扣除的点数与是否发生在自用模式下。
--
-- 生图单价为 0 表示"未单独配置"，按兜底价计费；因此管理员把某模型的单价
-- 设为 0 等价于继承兜底，而不是免费——要全局免费应把
-- gateway.default_image_ratio 配成 0。

-- ---------------------------------------------------------------- model_ratios

ALTER TABLE model_ratios ADD COLUMN IF NOT EXISTS image_ratio DOUBLE PRECISION NOT NULL DEFAULT 0;

COMMENT ON COLUMN model_ratios.image_ratio IS '生图倍率：点 / 张，1 点 = 0.001 元，0 表示继承兜底单价';

-- ---------------------------------------------------------------- call_logs

ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS image_count INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN call_logs.image_count IS '本次调用生成的图片张数；非生图调用为 0';

-- ---------------------------------------------------------------- image_works

ALTER TABLE image_works ADD COLUMN IF NOT EXISTS points BIGINT NOT NULL DEFAULT 0;
ALTER TABLE image_works ADD COLUMN IF NOT EXISTS self_use BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN image_works.points IS '本次生成实际扣除的点数；自用模式下记录应扣点数但不扣减余额';
COMMENT ON COLUMN image_works.self_use IS '该次生成发生在自用模式下（免费）';
