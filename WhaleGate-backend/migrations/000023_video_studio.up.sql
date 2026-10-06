-- 视频工作台：视频脚本创作与视频生成。
--
-- 沿用绘图工作台的两点经验：
--   1. 生成结果统一转存到存储后端，上游临时链接几小时内失效，转存后作品库才能长期回看；
--   2. 生成结果要能计费——视频没有 token 维度口径，以「秒」为计量单位。
--
-- 视频生成是异步任务（提交 → 轮询 → 取内容），因此作品表带任务状态与上游任务 ID，
-- 与图片的一次性同步返回区分开。

-- ---------------------------------------------------------------- model_ratios

ALTER TABLE model_ratios ADD COLUMN IF NOT EXISTS video_ratio DOUBLE PRECISION NOT NULL DEFAULT 0;

COMMENT ON COLUMN model_ratios.video_ratio IS '视频单价：点 / 秒，0 表示继承兜底单价';

-- ---------------------------------------------------------------- call_logs

ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS video_seconds INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN call_logs.video_seconds IS '本次调用生成的视频秒数；非视频调用为 0';

-- ---------------------------------------------------------------- prompt_templates

-- 词库复用同一张表：kind 区分绘图提示词与视频脚本模板。
ALTER TABLE prompt_templates ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'image';

COMMENT ON COLUMN prompt_templates.kind IS '词库类型：image 绘图提示词 / video 视频脚本模板';

CREATE INDEX IF NOT EXISTS idx_prompt_templates_kind ON prompt_templates (kind);

-- ---------------------------------------------------------------- video_scripts

CREATE TABLE IF NOT EXISTS video_scripts (
  id           bigserial PRIMARY KEY,
  user_id      bigint      NOT NULL,
  api_key_id   bigint      NOT NULL DEFAULT 0,
  channel_id   bigint      NOT NULL DEFAULT 0,
  model        varchar(128) NOT NULL DEFAULT '',
  title        varchar(255) NOT NULL DEFAULT '',
  brief        text        NOT NULL DEFAULT '',
  params       text        NOT NULL DEFAULT '{}',
  script       text        NOT NULL DEFAULT '[]',
  scene_count  integer     NOT NULL DEFAULT 0,
  total_seconds integer    NOT NULL DEFAULT 0,
  status       smallint    NOT NULL DEFAULT 1,
  error_message text       NOT NULL DEFAULT '',
  points       bigint      NOT NULL DEFAULT 0,
  self_use     boolean     NOT NULL DEFAULT FALSE,
  latency_ms   integer     NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE video_scripts IS '视频工作台：由大模型生成并保存的分镜脚本';
COMMENT ON COLUMN video_scripts.brief IS '创作需求：主题、风格、受众、时长、镜头数等';
COMMENT ON COLUMN video_scripts.params IS '生成参数 JSON：style / audience / duration / scenes';
COMMENT ON COLUMN video_scripts.script IS '分镜 JSON 数组：[{index,title,visual,narration,camera,seconds,prompt}]';
COMMENT ON COLUMN video_scripts.status IS '1 成功 / 2 失败';
COMMENT ON COLUMN video_scripts.points IS '本次脚本生成扣除的点数（按 token 计费）';

CREATE INDEX IF NOT EXISTS idx_video_scripts_user ON video_scripts (user_id, created_at DESC);

-- ---------------------------------------------------------------- video_works

CREATE TABLE IF NOT EXISTS video_works (
  id             bigserial PRIMARY KEY,
  user_id        bigint      NOT NULL,
  api_key_id     bigint      NOT NULL DEFAULT 0,
  channel_id     bigint      NOT NULL DEFAULT 0,
  model          varchar(128) NOT NULL DEFAULT '',
  prompt         text        NOT NULL DEFAULT '',
  negative_prompt text       NOT NULL DEFAULT '',
  params         text        NOT NULL DEFAULT '{}',
  videos         text        NOT NULL DEFAULT '[]',
  status         smallint    NOT NULL DEFAULT 0,
  upstream_model varchar(128) NOT NULL DEFAULT '',
  task_id        varchar(128) NOT NULL DEFAULT '',
  progress       integer     NOT NULL DEFAULT 0,
  duration_sec   integer     NOT NULL DEFAULT 0,
  error_message  text        NOT NULL DEFAULT '',
  points         bigint      NOT NULL DEFAULT 0,
  self_use       boolean     NOT NULL DEFAULT FALSE,
  source         varchar(16) NOT NULL DEFAULT 'studio',
  latency_ms     integer     NOT NULL DEFAULT 0,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE video_works IS '视频工作台：视频生成作品（异步任务）';
COMMENT ON COLUMN video_works.videos IS '结果 JSON 数组：[{storage,key,url,mime_type,duration_sec}]';
COMMENT ON COLUMN video_works.status IS '0 排队中 / 1 生成中 / 2 成功 / 3 失败';
COMMENT ON COLUMN video_works.task_id IS '上游任务 ID，用于轮询与取内容';
COMMENT ON COLUMN video_works.duration_sec IS '实际生成视频的秒数，按秒计费的计量单位';
COMMENT ON COLUMN video_works.points IS '本次生成扣除的点数；自用模式下记录应扣点数但不扣余额';

CREATE INDEX IF NOT EXISTS idx_video_works_user ON video_works (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_video_works_status ON video_works (status);

-- ---------------------------------------------------------------- 视频脚本词库种子数据

INSERT INTO prompt_templates (kind, category, title, summary, prompt, tags, model_hint, aspect_ratio, sort) VALUES
('video', 'promo', '产品宣传短片', '30 秒卖点导向的产品广告片',
 '主题：__主题__。风格：科技感、明快节奏。要求：开头 3 秒抛出核心卖点，中段用 2-3 个镜头展示使用场景与细节特写，结尾给品牌与行动号召。每个镜头标注画面内容、运镜方式、旁白文案与建议时长（秒），并给出可直接用于视频生成模型的英文画面提示词。',
 '["宣传","产品","广告"]'::text, '', '16:9', 100),
('video', 'promo', '品牌故事片', '情绪驱动的品牌形象短片',
 '主题：__主题__。风格：克制、电影感、暖调。要求：以一句品牌主张开场，用人物与环境镜头建立情绪，留白与旁白呼应，结尾收束到品牌标识。输出每个镜头的画面、运镜、旁白、时长与视频生成提示词。',
 '["品牌","故事","情感"]'::text, '', '16:9', 90),
('video', 'knowledge', '知识科普讲解', '把复杂概念讲清楚的分段讲解',
 '主题：__主题__。受众：普通观众。要求：先用生活化类比建立直觉，再分 2-3 步展开原理，每步配一个示意镜头；语言口语化、避免术语堆砌。输出画面、运镜、旁白、时长与视频生成提示词。',
 '["科普","讲解","教育"]'::text, '', '16:9', 95),
('video', 'knowledge', '数据可视化解读', '围绕数据与趋势的动态解读',
 '主题：__主题__。要求：镜头依次为数据全景、关键指标放大、对比变化、结论收束；旁白给出具体数值与结论。输出画面、运镜、旁白、时长与视频生成提示词。',
 '["数据","可视化","解读"]'::text, '', '16:9', 80),
('video', 'story', '剧情短片开篇', '有冲突与悬念的叙事开场',
 '主题：__主题__。要求：开场给一个具体人物与处境，30 秒内抛出冲突或悬念；镜头以中近景为主，结尾留钩子。输出画面、运镜、台词、时长与视频生成提示词。',
 '["剧情","叙事","短片"]'::text, '', '16:9', 90),
('video', 'story', '情感微电影', '以情绪弧线推进的短叙事',
 '主题：__主题__。要求：按「平静—扰动—转折—和解」推进，镜头随情绪由稳到动再回归平稳；旁白克制。输出画面、运镜、旁白、时长与视频生成提示词。',
 '["情感","微电影","叙事"]'::text, '', '2.39:1', 85),
('video', 'vlog', 'Vlog 开场与转场', '个人记录类内容的开场与转场设计',
 '主题：__主题__。要求：开场用第一人称视角建立代入感，中段安排 2 个转场（遮挡转场 / 运动转场），结尾自然收束。输出画面、运镜、口播文案、时长与视频生成提示词。',
 '["Vlog","记录","转场"]'::text, '', '9:16', 88),
('video', 'food', '美食探店短片', '突出食物质感与制作过程',
 '主题：__主题__。要求：近景特写食材纹理与热气，中段展示制作关键动作，结尾呈现成品与品尝反应；光线偏暖、景深虚化。输出画面、运镜、旁白、时长与视频生成提示词。',
 '["美食","探店","质感"]'::text, '', '9:16', 92),
('video', 'tutorial', '软件教程演示', '步骤清晰的操作演示脚本',
 '主题：__主题__。要求：按步骤拆分，每步一个镜头，先说要做什么再展示操作；关键点击用特写与高亮标注。输出画面、运镜、口播、时长与视频生成提示词。',
 '["教程","演示","操作"]'::text, '', '16:9', 86),
('video', 'teaser', '活动预告片', '强节奏的信息预告',
 '主题：__主题__。要求：节奏快、镜头短（每镜 1-2 秒），依次给出时间、地点、亮点、报名方式；文字与画面配合。输出画面、运镜、字幕、时长与视频生成提示词。',
 '["预告","活动","快节奏"]'::text, '', '9:16', 84)
ON CONFLICT DO NOTHING;
