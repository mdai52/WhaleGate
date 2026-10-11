# 鲸闸 WhaleGate · 设计说明书

> 版本：v1.0　　最后更新：2026-09-28
> 适用代码基线：`main` 分支（含 000016 迁移）
> 阅读对象：后端 / 前端工程师、运维、架构评审

---

## 1. 项目定位与设计目标

### 1.1 一句话定位

鲸闸 WhaleGate 是一个**统一 AI 大模型 API 网关**：对外只提供一份 OpenAI / Gemini 兼容协议入口，
对内聚合多家上游渠道（OpenAI 兼容协议、Gemini 原生协议、国内模型厂商），
统一承担**协议转换、模型路由、负载均衡、失败重试、计量计费与用量审计**。

### 1.2 设计目标

| 目标 | 说明 | 验收口径 |
| --- | --- | --- |
| 协议透明 | 客户端只认 OpenAI 协议，不关心上游是哪家 | OpenAI SDK / Gemini SDK 均可直连，无需改代码 |
| 接入零配置 | 只填上游地址与密钥即可用 | 自动探测协议类型、拉取模型列表与能力 |
| 计费可信 | 每一次调用都有 token 与点数账目 | 调用日志含 `usage_confidence`，缺失时降级估算 |
| 多租户安全 | 用户、密钥、额度、凭证互不可见 | 密钥摘要化存储，响应体不回传任何密钥字段 |
| 可运维 | 从安装到升级不需要读源码 | Web 安装向导、Docker 一键起、迁移自动化 |

### 1.3 明确非目标（本版不做）

- 不做模型推理本身，只做转发与计费
- 不做多活跨机房调度（单实例 + 外部 Postgres/Redis 即可横向扩展）
- 不做提示词审核与内容安全网关（预留钩子位置，当前不实现）
- 不做账单支付结算，只做额度记账（1 元 = 1000 点的内部账本）

---

## 2. 总体架构

### 2.1 分层视图

```
┌──────────────────────────────────────────────────────────────┐
│                        客户端 / SDK                           │
│   OpenAI SDK   ·   Gemini SDK   ·   cURL   ·   第三方应用      │
└───────────────┬──────────────────────────────┬───────────────┘
                │ OpenAI 协议                   │ Gemini 原生协议
┌───────────────▼──────────────────────────────▼───────────────┐
│ 接入层  Access Layer                                          │
│  协议入口路由 · API Key 鉴权 · 分组限流 · 请求体限制 · 安全头   │
└───────────────┬──────────────────────────────────────────────┘
                │
┌───────────────▼──────────────────────────────────────────────┐
│ 归一化层  Normalize Layer                                     │
│  请求 → UnifiedRequest      响应 → UnifiedResponse            │
│  非标扩展收敛（images / inlineData）· 思维链计量 · 参数归一     │
└───────────────┬──────────────────────────────────────────────┘
                │
┌───────────────▼──────────────────────────────────────────────┐
│ 路由层  Routing Layer                                         │
│  模型名 → 渠道解析 · 别名 fork · 参数注入 · 能力协商           │
│  负载均衡 · 失败重试 · 自动禁用与恢复                         │
└───────────────┬──────────────────────────────────────────────┘
                │
┌───────────────▼──────────────────────────────────────────────┐
│ 适配层  Adapter Layer                                         │
│  OpenAI 适配器 · Gemini 适配器 · Anthropic 适配器               │
│  + 厂商档案（vendor）：端点 / 鉴权 / 字段命名 / 错误外壳         │
└───────────────┬──────────────────────────────────────────────┘
                │
┌───────────────▼──────────────────────────────────────────────┐
│ 上游渠道  Upstream Providers                                  │
│  OpenAI · Gemini · DeepSeek · 通义 · 豆包 · 智谱 · Kimi · …   │
└──────────────────────────────────────────────────────────────┘

横切能力：计量计费 · 调用日志 · 审计日志 · 配置与密钥托管
```

### 2.2 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go 1.26、Gin、GORM、go-redis v9、zap、golang-jwt v5、golang-migrate |
| 存储 | PostgreSQL 16（业务数据）、Redis 7（缓存 / 限流 / 会话 / 预扣额度） |
| 前端 | Vue 3 + Vite + TypeScript + Ant Design Vue 4 + Pinia + ECharts |
| 工程 | Makefile、Docker、docker-compose、CNB / GitHub Actions |

### 2.3 目录结构

```
.
├── WhaleGate-backend/
│   ├── cmd/whalegate          # 入口（含 -migrate 子命令）
│   ├── configs/               # config.example.yaml
│   ├── migrations/            # 版本化 SQL 迁移（当前至 000016）
│   └── internal/
│       ├── app/               # 装配与生命周期
│       ├── config/            # viper + WG_ 环境变量覆盖
│       ├── constant/          # 全局常量
│       ├── handler/           # HTTP 处理器（relay / channel / user / …）
│       ├── middleware/        # 鉴权 / 限流 / 日志 / 恢复
│       ├── model/             # GORM 数据模型
│       ├── pkg/               # 基础包
│       │   ├── gateway/       # 协议适配（spec / openai / gemini / anthropic / vendor 厂商档案）
│       │   ├── ratelimit/     # 限流器
│       │   ├── crypto/ jwt/   # 加解密与令牌
│       │   ├── cache/ database/ logger/ response/ apierr/
│       │   └── mcp/ skills/   # 工具与技能
│       ├── router/            # 路由注册
│       └── service/           # 业务逻辑
├── WhaleGate-frontend/
│   └── src/{views,layouts,components,stores,api,router,styles}
└── docs/                      # 设计文档与数据字典
```

---

## 3. 模块设计

### 3.1 接入层

**职责**：所有外部请求的入口把关，在这里完成鉴权、限流、体积限制，避免无效请求打到上游。

| 关注点 | 设计 |
| --- | --- |
| 鉴权 | `Authorization: Bearer sk-xxx`；库中只存 SHA-256 摘要，校验时哈希比对 |
| 密钥策略 | API Key 级策略（`key_policies`）：可用模型、额度、有效期 |
| 限流 | Redis 原子计数；QPM 与并发按「密钥级 > 分组级 > 全局默认」逐层回落（详见 3.6） |
| 请求体上限 | `http.MaxBytesReader`，8MB |
| 安全响应头 | CSP / X-Frame-Options / nosniff / Referrer-Policy / Permissions-Policy / COOP，HTTPS 下附加 HSTS |
| 真实客户端 IP | 支持 `server.trusted_proxies` 与 `server.real_ip_header`，兼容 `X-Forwarded-For` / `X-Real-IP` |
| 请求协议识别 | 依据 `X-Forwarded-Proto` 或 TLS 状态判定 http/https，落库到 `call_logs.request_scheme` |

### 3.2 归一化层（协议适配核心）

**唯一内部契约**：`internal/pkg/gateway/spec` 中的 `UnifiedRequest` / `UnifiedResponse`。
任何协议的请求先转成它，任何渠道的响应也先转成它，因此 **不同协议的上游之间可以任意互转**。
上游为 Anthropic（含 Claude Code 网关）时，工具调用参数由 `input_json_delta` 分片下发，
需按请求持有有状态的 `StreamDecoder` 跨事件累积后再整体下发。

**渠道协议（出站）**由 `channels.type` 决定。判据是「上游是否说 OpenAI 协议」：
说的一律用 `openai`，端点、鉴权与字段命名的差异由厂商档案补齐；
只有 Gemini、Anthropic 这类报文结构本身不同的上游才单列协议。

| 类型 | 适用上游 |
| --- | --- |
| `openai` | 绝大多数厂商与聚合平台（含 Azure、百炼、千帆、Ollama 的官方兼容端点） |
| `gemini` | Google Gemini 原生 |
| `anthropic` / `claude-code` | Anthropic Messages API 与 Claude Code 网关 |

客户端协议（入站）由请求路径决定，与协议类型在统一格式上解耦。

#### 3.2.0 厂商档案（vendor）

同为 `openai` 类型的各家仍有细节差异。这些差异存在 **`vendor_profiles` 表**里
（管理后台 → 厂商适配；迁移 000027 灌入 25 家已知厂商作为默认值），
启动时灌入 `vendor` 包的内存存储，后台改动保存后立即重载。
按上游地址识别厂商后自动生效，**不要求管理员额外配置**：

| 维度 | 差异 |
| --- | --- |
| 端点与鉴权 | Azure 部署名路径 + `api-key` 头 + `api-version`；百炼自动补 `compatible-mode/v1`；Ollama 自动补 `/v1` |
| 思维链字段 | `reasoning_content`（DeepSeek / 通义 / 智谱 / 硅基流动）、`reasoning`（OpenRouter / Groq）、`thinking`（Ollama） |
| 输出上限 | `max_tokens` 或 `max_completion_tokens`（OpenAI o 系列 / gpt-5） |
| usage 口径 | `prompt/completion_tokens` 或 `input/output_tokens`（百炼） |
| 响应兜底 | 千帆老版端点的 `result` 正文与 `error_code/error_msg` |
| 错误外壳 | `error.message` / `message` / `error_code+error_msg`（千帆） / `code+message`（百炼、讯飞） |
| 流式 usage | 不认 `stream_options` 的厂商不下发该字段，改用估算口径计量 |
| 视频端点 | Sora 风格 / 智谱 / 百炼万相 / 火山方舟 |
| 生图端点 | OpenAI 形态；百炼自动补全 `compatible-mode/v1` 并把尺寸换成 `1024*1024` 写法 |

识别只影响「差异字段怎么填」，猜错时退回通用 OpenAI 形态，不改变请求语义。

#### 3.2.1 非标扩展归一

上游返回的生图结果可能不在标准 `content` 字段：

| 上游形态 | 归一目标 | 输出侧还原 |
| --- | --- | --- |
| `message.images[]`（data URI，OpenRouter 风格） | `UnifiedResponse.Images` | OpenAI 输出保留 `message.images[]` |
| Gemini `inlineData` parts | `UnifiedResponse.Images` | Gemini 输出转为 `inlineData` parts |

#### 3.2.2 思维链计量

`usage` 统一归一为三项：`prompt` / `completion` / `reasoning`。
**计费口径：按 `prompt + completion`（completion 已含 reasoning）合计扣减**，
避免思维链 token 漏计（实测部分上游把 reasoning 单独放在 `completion_tokens_details`）。

#### 3.2.3 流式转换

SSE 逐块转换，**禁止攒齐整包再吐**。OpenAI SSE 与 Gemini `streamGenerateContent`
的 JSON 数组流之间做增量映射，保证首字延迟（TTFT）不被转换逻辑吃掉。

### 3.3 路由层

#### 3.3.1 渠道模型

渠道（Channel）是上游接入的唯一抽象，承载：地址、加密后的密钥、协议类型、启用的模型列表、
权重 / 优先级、健康状态、参数能力声明（`param_schema`）、模型名映射（`model_map`）、
渠道级参数注入（`request_override`）、模型别名（`model_alias`）。

#### 3.3.2 自动探测

只填「上游地址 + 密钥」点「自动探测模型」：探测服务按
OpenAI 兼容（`/models`）→ Anthropic 原生（`/v1/models` + `x-api-key`）→
Claude Code 网关（`/v1/models` + `Bearer`）→ Codex → Gemini 原生（`?key=`）顺序逐个尝试，
命中后拉取模型列表并推断能力（是否支持流式 / 工具调用 / 视觉 / 生图），写入渠道记录。
探测到的原生协议会自动归一化到网关支持的渠道类型：Anthropic / Claude Code 保留原生协议，Codex 归为 OpenAI 兼容。
编辑渠道时可复用已保存密钥完成探测，无需重新输入。

#### 3.3.3 模型别名与参数注入

别名机制把一个上游模型 fork 成多个对外模型名，每个别名可带独立覆盖参数：

```json
{
  "gpt-image-1k": { "model": "gpt-4o", "override": { "tools": [{ "type": "image_generation", "size": "1024x1024" }] } },
  "gpt-image-2k": { "model": "gpt-4o", "override": { "tools": [{ "type": "image_generation", "size": "2048x2048" }] } }
}
```

合并优先级：**别名级 `override` > 渠道级 `request_override` > 客户端原始参数**；
深度合并，**数组整体替换**（不逐元素合并，避免工具声明被意外拼接）。

#### 3.3.4 自定义参数自动识别与分配

四步流水线：

1. **参数名归一**：`topK` / `top-k` / `max_output_tokens` → `top_k` / `max_tokens`
2. **能力协商**：按渠道 `param_schema.supported` 过滤；未声明则用协议默认集，可用 `exclude` 排除
3. **协议映射**：`temperature` → Gemini `generationConfig.temperature`；`top_k` → `topK`；`response_format` → `responseMimeType`
4. **回显与留痕**：不支持参数丢弃，结果经 `X-WG-Params-Applied` / `X-WG-Params-Dropped` 回显并写入调用日志

```json
{ "supported": ["temperature", "top_p", "top_k", "repetition_penalty", "max_tokens", "stop", "seed"] }
```

#### 3.3.5 负载均衡 / 重试 / 熔断

- 多渠道同模型时按权重选择（round-robin / 加权）
- 上游 5xx / 超时触发重试，重试换渠道（不重复打同一渠道）
- 连续失败达阈值自动禁用渠道并告警，冷却期后半开探测恢复

### 3.4 计量计费

| 环节 | 设计 |
| --- | --- |
| 倍率表 | `model_ratios`：token 单位「点 / 1K tokens」，生图单位「点 / 张」，1 点 = 0.001 元，`*` 为兜底倍率 |
| 预扣 | `ReserveQuota`：Redis Lua 脚本原子预扣，先占额度再发请求，防并发穿透。生图按**请求张数**预扣，token 调用按估算用量预扣 |
| 结算 | `Settle`：按真实 usage 多退少补；请求失败**全额回补**。生图按**实际出图张数**结算 |
| 余额不足 | 直接拒绝，业务码 `40201` |
| 用量置信度 | `usage_confidence`：`reported`（上游上报）/ `estimated`（按请求参数估算） |
| 日志落库 | `call_logs` 异步批量写入：模型、渠道、耗时、首字延迟、token、点数、IP、协议 |

### 3.5 用户与密钥

- 角色：`admin` / `user`；**管理员只能由初始化流程创建**，注册永不提权
- 密钥：创建时明文只展示一次；库中存 SHA-256 摘要 + 脱敏展示值；支持吊销与删除
- 密钥统计：创建时间、最近使用、累计请求、累计 token（供用户自查异常消耗）
- 账号安全：`must_change_password` 标记存在时，除改密与登出外所有接口返回 403

### 3.6 分组与限流

> 目标：管理员可**按分组**统一配置倍率与限流策略，密钥创建时选择分组即可继承。

#### 3.6.1 实体设计

```
api_keys.group_tag ──► key_groups（分组）
                        ├─ 倍率系数（在模型倍率算出的点数上相乘）
                        ├─ QPM / 并发上限（0 = 该层不设限）
                        └─ 状态（启用 / 停用）
                        └──► 默认分组 default（迁移预置，不可删除）
```

**归属与优先级**

- 分组记录在 `api_keys.group_tag`（分组标识），一个密钥至多一个分组；
- 限流生效值按 **密钥级 > 分组级 > 全局默认** 逐层回落，每层的 `0` 都表示「该层不设限」；
- 倍率：`最终点数 = 按模型倍率算出的点数 × 分组倍率系数`。分组系数与按模型的倍率保持正交，
  管理员只需给分组一个系数，不必为每个分组重复维护整张模型倍率表。

**为什么用标识而不是外键**

`group_tag` 在引入分组之前就已存在（自由文本标签），沿用它意味着**无需回填任何存量数据**。
语义上也更贴切：分组是「策略预设」而非「权限域」，弱引用比外键级联更合适——
分组被删除或禁用时，其下密钥自动回落为中性策略（系数 1、不额外限流），
而不是因为约束失败让线上调用中断。

#### 3.6.2 表设计

```sql
CREATE TABLE key_groups (
    id               BIGSERIAL PRIMARY KEY,
    name             VARCHAR(64)      NOT NULL,            -- 分组标识，密钥上记录的就是它
    display_name     VARCHAR(64)      NOT NULL DEFAULT '',
    description      VARCHAR(255)     NOT NULL DEFAULT '',
    ratio_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,  -- 倍率系数，1 = 不加价
    qpm              INTEGER          NOT NULL DEFAULT 0,  -- 0 = 该层不设限
    concurrency      INTEGER          NOT NULL DEFAULT 0,
    enabled          BOOLEAN          NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    deleted_at       TIMESTAMPTZ                           -- 软删除
);

-- 仅约束未删除记录，避免删除后同名分组无法重建
CREATE UNIQUE INDEX idx_key_groups_name_active ON key_groups (name) WHERE deleted_at IS NULL;
```

迁移同时预置 `default` 分组，使密钥表单开箱即有可选项。

#### 3.6.3 执行位置（硬约束）

```
鉴权 → 【分组解析】 → 【限流：QPM + 并发】 → 额度预扣 → 路由选择 → 转发上游 → 结算 → 释放并发
```

**限流必须在路由与转发之前**：放到转发之后，被拒请求已经消耗了上游调用与真金白银。

#### 3.6.4 缓存与生效时延

分组表按 **60 秒进程内缓存**读取（与倍率表同策略）。

**刻意不把分组策略折进 `KeyIdentity`**：密钥缓存最长 24 小时，
折进去会让管理员调整分组倍率或限流后长时间不生效。

#### 3.6.5 管理接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/groups` | 启用中的分组（登录用户可读，供密钥表单选择并展示其策略） |
| GET | `/api/v1/admin/groups` | 全部分组（含已禁用） |
| POST | `/api/v1/admin/groups` | 新建分组；同名软删除记录会被恢复，避免标识被历史数据占用 |
| PUT | `/api/v1/admin/groups/:id` | 更新策略与开关（标识是密钥的引用键，创建后不可修改） |
| DELETE | `/api/v1/admin/groups/:id` | 删除分组；内置 `default` 拒绝删除 |

新建 / 修改密钥时会校验分组标识存在，避免写入拼错的标识导致策略静默不生效；
存量密钥不受影响，仍按中性策略运行。

#### 3.6.6 尚未实现（后续可扩展）

早期设计中还包含以下内容，本版未实现：

- `group_models`：分组级模型可用性与**按模型**的倍率覆盖（当前只有统一系数）
- RPM / 每日请求数 / 单日 token 上限，以及超限后的排队降级（当前只有 QPM 与并发）
- `users.group_id` 用户级分组（当前分组只挂在密钥上，按密钥粒度已足够表达用途）
- Redis Lua 原子化多维计数（当前复用既有的 QPM 滑动窗口与并发计数）

### 3.7 安全体系

| 层面 | 措施 |
| --- | --- |
| 凭据存储 | 密码 bcrypt；API Key SHA-256 摘要；上游密钥 / OAuth 令牌 / WebAuthn 公钥 / 注入参数 AES-256-GCM 加密落库 |
| 免密登录 | Passkey（WebAuthn）：会话存 Redis、5 分钟有效且用完即焚；GitHub OAuth：一次性票据 60 秒有效，令牌不出现在地址栏 |
| 防爆破 | 认证接口按 IP 限流（默认 20 次/分钟）；连续失败达阈值锁定账号（默认 5 次 / 15 分钟），成功清零 |
| 传输 | 支持服务端 TLS 监听，启用后强制安全响应头与 HSTS |
| 脱敏 | 列表接口邮箱脱敏 `a***@corp.com`；查询参数 `api_key` / `token` / `password` 日志里记为 `***` |
| 审计 | `audit_logs` 记录登录、改密、密钥、渠道、额度、倍率、凭证、绑定等操作（含 IP / UA / trace_id），异步批量落库 |
| 错误处理 | 内部错误明细只进日志，客户端只拿统一错误码与文案，防信息泄露 |

### 3.8 工具与扩展

- **MCP 服务**：支持 stdio / HTTP 两类 MCP Server 配置、工具发现与路由、代执行开关、调试调用
- **技能管理**：扫描 SKILL.md 目录导入技能，支持「全文注入」与「仅索引」两种模式与启停
- 两者共用「最大工具轮次」上限，防止工具调用无限循环

### 3.9 可观测性

| 数据源 | 内容 | 用途 |
| --- | --- | --- |
| `call_logs` | 每次转发：模型、渠道、状态、token、点数、图片张数、耗时、首字延迟、IP、协议、参数应用明细 | 排障、对账、用户自查 |
| `audit_logs` | 管理动作留痕 | 安全审计 |
| 日志导出 | `GET /api/v1/admin/logs/export` 导出 CSV（含协议与客户端 IP 列） | 离线分析 |
| 健康检查 | `/healthz`（存活）、`/readyz`（依赖就绪） | 容器编排探针 |

### 3.10 安装与引导

用户表为空时提供两种初始化路径：

1. **Web 安装向导**：访问任意页面跳 `/install`，填管理员账号密码即完成初始化并自动登录
2. **容器自动创建**：首次启动生成 `admin` + 24 位随机强密码，只打印一次，强制首登改密

`POST /api/v1/system/install` 仅在未初始化时可用，**避免被用于提权**。

### 3.11 OAuth 凭证接入

管理员在「认证文件」页通过 OAuth 绑定上游订阅账号（Claude Code、Codex、Gemini 等），
内置供应商的 `client_id` 取自各厂商 CLI 的公开内嵌值，配置文件可覆盖。

- **覆盖语义**：`oauth.providers` 中与内置 ID 相同的条目做**字段级覆盖**——
  以内置定义打底，只替换显式填写的字段。接入自建 OAuth 客户端时只需写
  `client_id` / `client_secret` / `redirect_uri`，不必重复抄写端点与 scopes。
- **redirect_uri 优先级**：供应商配置 > 前端传入 > `auth.frontend_base`。
  该地址必须与 OAuth 应用中登记的回调地址**完全一致**，否则上游直接拒绝。
  之所以让配置优先，是因为有些供应商（Google）只接受登记过的地址，
  而内置 `client_id` 的回调地址由厂商维护，无法登记本网关地址——
  此时必须自建 OAuth 客户端并显式指定 `redirect_uri`。
- **client_secret**：Google 等供应商的令牌端点强制要求；配置文件明文存放，
  需收紧文件权限且不得入库。

### 3.12 AI 绘图工作台

图片生成是同步一次性返回，没有流式与工具调用，因此**不复用** `UnifiedRequest` / `UnifiedResponse`，
另立一对轻量类型：

```
ImageRequest  { model, prompt, negative_prompt, n, size, quality, style, response_format, user, extra }
ImageResponse { created, model, images[], usage }
```

- **上游协议映射**：OpenAI 兼容渠道 `POST {base}/images/generations`；
  Gemini 原生渠道 `generateContent` + `generationConfig.responseModalities = ["TEXT","IMAGE"]`，
  结果从 `parts[].inlineData` 提取。Anthropic 协议无图片生成能力，选路时跳过并计入排除集
  （不计渠道失败，避免把生图模型挂错协议导致渠道被熔断）。
- **chat 接口回退**：聚合平台（OpenRouter 风格）常把部分模型的生图能力只开在 chat 接口下，
  传 `modalities: ["image","text"]` 后结果落在 `message.images[]`（data URI）。
  原生端点返回 400/404/405/415/422 这类"端点不认该模型"的错误时回退到 chat 接口重试一次；
  鉴权、限流、服务端故障换接口同样失败，不触发回退。
  这条路径完全复用既有的 `spec.Message.Images` 归一能力，无需新的解析代码。
  代价是 chat 生图一次只出一张，且不支持尺寸/质量等标准字段（只能靠 `Extra` 透传）。
- **图片转存**：上游返回的 `b64_json` 与临时 `url` 统一转存到配置的存储后端，
  表中只存对象键与后端类型。上游链接通常数小时失效，转存后作品库才能长期回看；
  单张转存失败时降级保留上游地址，不影响其余图片。
- **存储可插拔**：`internal/pkg/storage` 定义 `Storage` 接口，两种实现——
  本地磁盘（单机）与 S3 兼容对象存储（多实例 / 容器化，覆盖 AWS S3、Cloudflare R2、
  阿里云 OSS、腾讯 COS、自建 MinIO）。配置由管理员在后台维护，存于 `app_settings`，
  保存后即时重建后端，**无需重启**。
  后端实例按类型常驻于 `studioStorage`：作品记录保存了写入时的后端类型，
  因此**切换存储后历史作品仍按原后端回读**，不要求做数据迁移。
- **回读方式**：配置了公开访问前缀（本地目录的静态地址 / 对象存储的 CDN 或公开域名）
  时返回直链，前端直连存储，不占用网关带宽；否则经
  `GET /api/v1/studio/works/{id}/images/{index}` 按登录态与作品归属校验后回读，
  前端取回 blob 转临时地址渲染（`<img>` 无法携带 Authorization）。
- **词库**：`prompt_templates` 按 10 个分类维护精选模板，迁移内置种子数据，支持分类筛选与关键词检索。
- **可选模型**：由启用渠道的暴露模型汇总，口径是「模型目录声明 image 能力」或「模型名命中生图关键词」，
  并排除不支持生图的渠道协议。
- **计费**：图片生成没有 token 维度口径，因此以**张数**计量：
  `model_ratios.image_ratio` 为单价（点 / 张，`*` 与 `gateway.default_image_ratio` 兜底），
  `call_logs.image_count` 记录实际出图张数。
  与 chat 转发同走「预扣 → 多退少补」：按请求张数 `ReserveQuota` 预扣，
  成功按实际张数 `Settle` 结算，失败全额回补；余额不足在请求发出前即被拒绝（`40201`）。
  工作台与 `/v1/images/generations` 共用 `service.GenerateImage` 内的一份口径。

### 3.13 视频工作台（脚本创作 + 视频生成）

视频比图片多两个难点：**异步**（提交只是排队）与**上游形态不统一**。设计取舍如下：

- **异步任务**：`POST /videos` 提交拿到任务 ID → `GET /videos/{id}` 轮询状态 →
  `GET /videos/{id}/content` 取内容。提交接口只返回「已排队」，后台协程推进到终态，
  前端轮询作品列表看进度；超过 `studio.video_max_wait` 仍未终态的任务在查询时统一标记失败，
  避免进程重启后留下永远转圈的条目。
- **状态归一**：各家文案（`pending` / `in_progress` / `completed` / `failed` …）
  归一到 `queued` / `processing` / `succeeded` / `failed` 四档；
  `seconds` 可能是数字也可能是字符串（Sora 允许 `"8"`），统一转换。
- **协议边界**：只认 OpenAI 兼容的 `/v1/videos`。Gemini 的 Veo 走 `predictLongRunning`
  这套独立接口，Anthropic 不提供视频生成，明确报错胜过让上游返回 404。
- **脚本创作**：由对话模型产出结构化 JSON 分镜（画面 / 旁白 / 运镜 / 时长 / 英文画面提示词），
  超出目标总时长时按各镜头时长等比收敛；模型输出常带 ` ```json ` 代码块或前后缀，
  解析前先裁掉。脚本**按 token 计费**，与 chat 转发同源。
- **视频计费**：以**秒**计量（`model_ratios.video_ratio`，`gateway.default_video_ratio` 兜底）——
  生成 10 秒与 4 秒成本差一倍，按条计会让长视频亏、短视频贵。
  同样走「预扣 → 多退少补 → 失败回补」。

### 3.14 接入引导与参数调试

对接成本主要卡在两件事上：不知道「该填什么」，以及不知道「参数为什么没生效」。
因此把接入信息集中成一个独立入口，并提供可预演的参数调试：

- **接入指南**：地址与鉴权、多语言示例、自定义参数传参方式、AI 客户端填法、常见问题。
  客户端接入只收录确认兼容 OpenAI 协议的客户端，并直接给出「网关地址 + API Key + 模型名」
  三项可复制值；GUI 客户端菜单位置随版本变化，因此给要点而非死记路径。
- **参数调试**：`POST /api/v1/playground/params/preview` 走 `SelectChannel` 选出渠道后，
  用**与真实转发完全相同的** `BuildUpstreamRequest` + `ApplyParams` 计算分配结果，
  返回已应用 / 被丢弃 / 未标准化三组参数与最终上游请求体。
  因为不发起上游请求，所以不消耗额度、不计入调用日志，可以放心反复试。
- 被丢弃的参数会区分原因：渠道显式排除、协议无对应字段、能力未声明，
  并指向「渠道 → 参数能力声明」这一处可操作的修改位置。

> 边界说明：网关支持将 Anthropic 作为**上游**渠道，但未提供 Anthropic 原生**入站**入口
> （`/v1/messages`），因此 Claude Code 与 CCSwitch 这类要求 Anthropic Messages API 的
> 客户端目前无法直接指向本网关。

---

## 4. 数据模型清单

| 表 | 用途 | 关键字段 |
| --- | --- | --- |
| `users` | 用户 | username、password_hash、role、quota_points、must_change_password、status、group_id |
| `api_keys` | 访问密钥 | user_id、name、key_hash、key_prefix、last_used_at、total_requests、total_tokens、status、group_id |
| `channels` | 上游渠道 | name、base_url、api_key_encrypted、protocol、models、weight、status、param_schema、model_map、model_alias、request_override、last_error |
| `model_ratios` | 模型倍率 | model、prompt_ratio / completion_ratio（点 / 1K tokens）、image_ratio（点 / 张），`*` 兜底 |
| `model_catalog` | 模型目录 | 渠道探测得到的模型与能力缓存 |
| `call_logs` | 调用日志 | user_id、key_id、channel_id、model、status、prompt/completion/reasoning tokens、points、image_count、latency_ms、ttft_ms、usage_confidence、client_ip、request_scheme |
| `audit_logs` | 审计日志 | actor、action、target、ip、ua、trace_id |
| `credentials` | OAuth 认证文件 | provider、payload_encrypted、quota、expires_at |
| `identity_bindings` | 第三方绑定 | user_id、provider、external_id |
| `passkey_credentials` | 通行密钥 | user_id、credential_id、public_key、sign_count |
| `skills` | 技能 | name、path、mode（full/index）、enabled |
| `mcp_servers` | MCP 服务 | name、transport、command/url、enabled、allow_exec |
| `settings` | 系统设置 | key、value（自用模式、技能与 MCP 开关、最大工具轮次等） |
| `prompt_templates` | 绘图提示词词库 | category、title、summary、prompt、tags、model_hint、aspect_ratio、sort、enabled |
| `image_works` | 绘图作品 | user_id、api_key_id、channel_id、model、prompt、params、images、status、image_count、latency_ms、points、self_use、source |
| **`groups`** | **分组（新增）** | name、rpm_limit、rpd_limit、concurrency、daily_token_cap、over_limit、status、is_builtin |
| **`group_models`** | **分组模型配置（新增）** | group_id、model、enabled、ratio |

字段级注释见 [`database-dictionary.md`](./database-dictionary.md)。

---

## 5. 接口设计

### 5.1 对外协议入口

| 协议 | 路径 | 说明 |
| --- | --- | --- |
| OpenAI 兼容 | `POST /v1/chat/completions` | 主入口，支持流式（SSE） |
| 文生图 | `POST /v1/images/generations` | OpenAI 兼容图片生成，同步返回 |
| 模型列表 | `GET /v1/models` | 汇总所有渠道（含别名）支持的模型 |
| Anthropic 原生 | `POST /v1/messages` | Claude Code / CCSwitch 等只认 Messages API 的客户端接入；入站按 Anthropic 协议解析，再按渠道协议转发 |
| Gemini 原生 | `POST /v1beta/models/*fullpath` | `generateContent` / `streamGenerateContent` |
| Gemini 原生（前缀别名） | `POST /gemini/v1beta/models/*fullpath` | 便于反向代理分流 |

### 5.2 用户侧管理接口

| 分组 | 接口 |
| --- | --- |
| 认证 | `POST /api/v1/auth/register`、`login`、`logout`、`change-password`、`ticket` |
| 账号 | `GET /api/v1/me`、`PUT /api/v1/me` |
| 密钥 | `GET/POST /api/v1/keys`、`POST /api/v1/keys/:id/revoke`、`DELETE /api/v1/keys/:id` |
| 用量 | `GET /api/v1/usage/logs`、`GET /api/v1/usage/summary` |
| 绑定 | `GET /api/v1/account/bindings`、Passkey 注册 begin/finish、GitHub authorize/callback |
| 绘图 | `GET /api/v1/studio/models`、`GET /api/v1/studio/prompts`、`POST /api/v1/studio/images`、`GET /api/v1/studio/works`、`DELETE /api/v1/studio/works/:id`、`GET /api/v1/studio/works/:id/images/:index` |
| 参数调试 | `GET /api/v1/playground/models`、`POST /api/v1/playground/params/preview` |
| 分组 | `GET /api/v1/groups`（启用中的分组，供密钥表单选择） |
| 系统 | `GET /api/v1/system/info`、`GET /api/v1/system/status`、`POST /api/v1/system/install` |

### 5.3 管理侧接口

| 分组 | 接口 |
| --- | --- |
| 用户 | `GET/POST /api/v1/admin/users`、`PATCH /users/:id/status`、`POST /users/:id/quota` |
| 渠道 | `POST /admin/channels/detect`、`GET /admin/channels/model-catalog`、CRUD、`POST /:id/test` |
| 分组 | `GET/POST /admin/groups`、`PUT/DELETE /admin/groups/:id`（见 3.6.5） |
| 倍率 | `GET /admin/ratios`（总览：渠道模型 ∪ 倍率表，含继承项）、`POST /admin/ratios`（新增 / 更新）、`DELETE /admin/ratios/:id` |
| 日志 | `GET /admin/logs`、`GET /admin/logs/export`、`GET /admin/audit-logs` |
| 设置 | `GET/PUT /admin/settings`、`POST /admin/skills/scan` 等 |
| 工具 | MCP 服务 CRUD、`POST /admin/mcp/:id/connect`、`GET /admin/mcp/tools`、`POST /admin/mcp/tools/call` |
| 凭证 | `GET /admin/credentials/providers`、start / complete / poll、`PATCH /:id/status`、`/name`、`GET /:id/export`、`POST /:id/refresh` |

### 5.4 统一响应格式

```json
{ "code": 0, "message": "ok", "data": {}, "trace_id": "8f14e45f-…", "timestamp": 1730000000 }
```

`code = 0` 为成功；HTTP 状态码与业务码保持语义一致（业务码 / 100）。错误码集中在 `internal/pkg/apierr/code.go`。
新增分组相关错误码：`40404` 分组内模型不可用、`42901` 触发限流（响应头带 `Retry-After`）。

---

## 6. 关键流程

### 6.1 转发主流程

```
1. 客户端 → POST /v1/chat/completions（Bearer sk-xxx）
2. 鉴权：SHA-256(key) 比对 api_keys，解析 user 与分组
3. 限流：Redis Lua 判定 rpm / rpd / 并发 / 日 token
   ├─ 通过 → 预占并发计数
   ├─ 超限 & reject → 42901 + Retry-After，结束
   └─ 超限 & queue → 入等待队列，最多 queue_timeout 秒
4. 额度预扣：ReserveQuota（Lua 原子）
   └─ 不足 → 40201，结束（释放并发）
5. 模型可用性：group_models 过滤 + 倍率解析（分组 > 全局 > 兜底）
6. 路由：模型名 → 渠道（加权选择，跳过禁用渠道）
7. 参数装配：归一 → 能力协商 → 协议映射 → 别名/渠道 override 合并
8. 转发：OpenAI / Gemini / Anthropic 适配器；流式则逐块转换 SSE
9. 响应归一 → UnifiedResponse → 按入口协议输出
10. 结算 Settle（多退少补，失败全额回补）
11. 落库：call_logs 异步批量写入；释放并发计数
```

### 6.2 渠道自动探测

```
填地址 + 密钥 → 依次尝试 /v1/models（OpenAI）与 /v1beta/models（Gemini）
→ 命中即记录 protocol → 拉取模型列表 → 推断能力（流式 / 工具 / 视觉 / 生图）
→ 写入 channels + model_catalog → 管理员勾选要开放的模型
```

### 6.3 预扣-回补

```
ReserveQuota(预估点数) ──► 上游调用 ──► Settle(真实点数)
                                │
                                └─ 失败/超时 ──► Refund(全额回补)
```

预估点数按「输入 token 估算 + 最大输出 token」上限计算，宁可多扣后退，不可少扣。

---

## 7. 前端设计

### 7.1 信息架构

```
登录 / 安装向导
└── 主布局（BasicLayout：侧边导航 + 内容区）
    ├── 用户门户
    │   ├── 概览       余额 / 累计消耗 / 近 N 天请求与 Token / 用量趋势（ECharts）/ 快速接入示例
    │   ├── 接入指南   地址与鉴权、多语言示例、自定义参数传参详解、AI 客户端接入、常见问题
    │   ├── 参数调试   选模型与参数，预览分配结果（已应用 / 被丢弃及原因 / 未标准化）与最终上游请求体
    │   ├── 密钥管理   创建（明文仅一次）、列表、吊销、删除、累计用量
    │   ├── 调用历史   按模型 / 状态 / 时间筛选；token、点数、耗时、首字延迟、置信度
    │   └── 账号与安全 改密、Passkey、GitHub 绑定
    ├── 创意
    │   └── AI 绘图     左侧提示词词库（分类筛选 + 检索 + 一键套用）/ 右侧创作面板（模型、提示词、
    │                   负向词、张数、尺寸、质量）/ 作品库标签页（网格回看、复用、下载、删除）
    └── 管理后台（仅管理员）
        ├── 用户管理   创建、启停、按元调整额度（1 元 = 1000 点）
        ├── 渠道管理   增删改查、连通性测试、自动探测模型、别名 / 注入 / 能力声明
        ├── 分组管理   分组列表 + 分组详情（限流策略 + 模型可用性与倍率覆盖）★新增
        ├── 倍率配置   以渠道模型为基准的倍率总览，未配置项回显兜底倍率，可编辑 / 恢复默认
        ├── 全局日志   多维检索、参数应用与丢弃明细、CSV 导出
        ├── 凭证       OAuth 认证文件导入 / 导出 / 配额
        ├── 工具       MCP 服务、技能管理
        └── 系统设置   自用模式、技能与 MCP 开关、最大工具轮次、管理员改密
```

### 7.2 交互与视觉规范（UI 设计稿依据）

| 项 | 规范 |
| --- | --- |
| 语言 | 简体中文；英文仅保留技术名词（model、token、RPM 等） |
| 主色 | 深海蓝 `#0B3D66`（品牌色，取自「鲸闸」Logo） |
| 辅助色 | 状态绿 `#31A24C`、告警黄 `#F7B928`、危险红 `#E41E3F`、成功浅底 `#E6F4EA`、停用灰 `#8595A4` |
| 中性色 | 画布 `#F1F4F7`、卡片 `#FFFFFF`、发丝线 `#DEE3E9`、正文 `#1C1E21`、次要 `#444950`、提示 `#8595A4` |
| 圆角 | 卡片 24px、输入框 10px、按钮全圆角（pill） |
| 间距 | 4 / 8 / 12 / 16 / 24 / 32 / 40 |
| 字体 | 中文 Noto Sans SC；数字与英文可用 Inter |
| 密度 | 管理后台偏紧凑（表格行高 48-56px），用户门户偏舒展 |
| 反馈 | 每个数据面必须有加载 / 空 / 错误 / 成功四态；危险操作二次确认；限流触发给出明确文案与重试时间 |
| 响应式 | 断点 1280 / 992 / 768；窄屏侧边栏收起为抽屉，表格列渐进折叠 |

---

## 8. 非功能设计

| 维度 | 目标与手段 |
| --- | --- |
| 性能 | 转发层无阻塞，流式转换零缓冲；日志异步批量写；限流与预扣均为单次 Redis Lua 往返 |
| 可用性 | 健康探针 `/healthz` `/readyz`；渠道失败自动禁用 + 冷却半开；上游超时可重试换渠道 |
| 一致性 | 计费金额以 Redis 原子操作为准，DB 落库为对账副本；额度不足宁可拒绝不可透支 |
| 可扩展 | 新增上游协议 = 新增一个 adapter 实现 UnifiedRequest/Response 映射，不动路由与计费 |
| 可观测 | 每次调用可凭 `trace_id` 串起访问日志、调用日志与审计日志 |
| 安全 | 加密落库、脱敏回显、审计留痕、防爆破、最小权限（注册不提权） |

---

## 9. 部署与运维

- **Docker Compose（推荐）**：`cp config.example.yaml config.yaml` → 改数据库/Redis/加密密钥/JWT 密钥 → `docker compose up -d` → 看日志取初始密码或用安装向导
- **Docker 单容器**：外接 Postgres / Redis，全部配置走 `WG_` 前缀环境变量
- **源码构建**：前端 `npm run build` → 后端 `go build` → `-migrate` 后启动
- **反向代理**：Nginx 必须 `proxy_buffering off; proxy_cache off; proxy_read_timeout 300s; chunked_transfer_encoding on;`，否则 SSE 被攒包
- **备份**：`pg_dump` 定时导出；升级前先备份，迁移在启动时自动执行

---

## 10. 演进规划

| 阶段 | 内容 | 优先级 |
| --- | --- | --- |
| 已交付 | 用户与密钥、协议适配（OpenAI/Gemini/Anthropic 互转）、渠道自动探测（含 Anthropic / Claude Code 原生协议）、别名与参数注入、计量计费、调用日志、MCP 与技能、Passkey / GitHub 绑定、TLS、**AI 绘图工作台（文生图 + 词库 + 作品库）** | — |
| 本次设计 | **分组与分组级限流、分组级模型可用性与倍率覆盖** | P0 |
| 下一步 | 图生图（`/v1/images/edits` 与参考图上传）、对外 `/v1/videos` 入口、词库后台维护、渠道级熔断指标看板、按用户维度的图表下钻、告警通知（Webhook / 企业微信） | P1 |
| 中期 | 提示词内容安全钩子、请求重放与灰度分流、模型成本估算与预算告警 | P2 |
| 长期 | 多实例横向扩展与就近调度、插件化 adapter 市场、企业级 SSO（OIDC / SAML） | P3 |

---

## 11. 附录

### 11.1 环境变量

所有配置项均可被 `WG_` 前缀环境变量覆盖，例如：

```bash
WG_DATABASE_HOST=postgres WG_SECURITY_JWT_SECRET=xxx ./bin/whalegate
```

常用：`WG_DATABASE_*`、`WG_REDIS_*`、`WG_SECURITY_JWT_SECRET`、`WG_SECRET_ENCRYPTION_KEY`、
`WG_SERVER_TLS_*`、`WG_SERVER_TRUSTED_PROXIES`、`WG_AUTH_GITHUB_*`、`WG_AUTH_WEBAUTHN_*`。

### 11.2 术语

| 术语 | 含义 |
| --- | --- |
| 渠道 Channel | 一个上游服务接入点（地址 + 密钥 + 协议） |
| 别名 Alias | 把同一上游模型 fork 成多个对外模型名，各自可带覆盖参数 |
| 能力协商 | 按渠道声明的支持参数集过滤客户端参数 |
| 分组 Group | 限流与模型授权的最小管理单元 |
| 点 Point | 内部计费单位，1 点 = 0.001 元，1000 点 = 1 元 |
| usage_confidence | token 用量的可信度标记（上游上报 / 本地估算） |

---

_本文档描述目标设计；带「新增」标记的部分对应 `000017_groups` 迁移与分组管理模块，属本次设计范围。_
