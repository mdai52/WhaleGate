<div align="center">

<img src="WhaleGate-frontend/public/logo-mark.png" width="96" alt="WhaleGate logo" />

# 鲸闸 WhaleGate

</div>

统一的 AI 大模型 API 网关：一份 OpenAI / Gemini 兼容协议入口，背后聚合多家上游渠道，
提供模型路由、负载均衡、失败重试、计量计费与用量审计。

## 特性速览

- **一份协议，接住所有上游** —— 对外只暴露 OpenAI / Anthropic / Gemini 兼容入口，内部归一为
  `UnifiedRequest` / `UnifiedResponse`，三种协议的上游之间可任意互转转发
- **渠道聚合与容错** —— 多上游按权重负载均衡、失败自动重试、连通性测试、自动探测模型与能力
- **三种计费口径** —— 对话按 token、生图按张、视频按秒；Redis Lua 原子「预扣-回补」，失败全额回补
- **参数不靠猜** —— 自定义参数默认透传，协议映射与能力声明自动协商，分配结果通过响应头与调用日志回显，
  另有「参数调试」页零成本预览
- **模型别名与参数注入** —— 把上游模型 fork 成多个对外模型名，各自带独立 `override`，渠道编辑页提供可视化编辑器
- **密钥、分组与限流** —— 倍率系数与 QPM / 并发打包成分组预设，按「密钥级 > 分组级 > 全局」逐层回落
- **不止于转发** —— 内置 AI 绘图工作台、视频工作台、MCP 服务与技能注入
- **安全默认开** —— 上游密钥、OAuth 令牌、WebAuthn 公钥全部 AES-256-GCM 加密落库，
  支持 Passkey 免密登录、防爆破锁定与全量审计留痕
- **开箱即用** —— Docker Compose 一键起全套，Web 安装向导完成初始化；前端全中文、响应式

## 目录

- [特性速览](#特性速览)
- [技术栈](#技术栈)
- [目录结构](#目录结构)
- [文档](#文档)
- [快速开始](#快速开始)
- [功能面板](#功能面板)
- [部署](#部署)
  - [Docker Compose（推荐）](#方式一docker-compose推荐)
  - [Docker 单容器](#方式二docker-单容器)
  - [源码构建](#方式三源码构建二进制部署)
  - [反向代理](#反向代理nginx)
  - [升级与备份](#升级与备份)
- [接入使用](#接入使用)
  - [自定义参数透传](#自定义参数透传)
- [首次安装与安全](#首次安装与安全)
- [开源协议](#开源协议)
- [用户中心：第三方绑定](#用户中心第三方绑定)
- [安全设计](#安全设计)
  - [用户数据安全](#用户数据安全)
  - [用户信息安全](#用户信息安全)
  - [系统安全](#系统安全)
- [关键设计](#关键设计)
  - [统一内部格式](#统一内部格式)
  - [模型别名与参数注入](#模型别名与参数注入)
  - [统一 OpenAI 协议分发](#统一-openai-协议分发)
  - [自定义参数自动识别与分配](#自定义参数自动识别与分配)
  - [AI 绘图工作台](#ai-绘图工作台)
  - [计量计费](#计量计费)
- [环境变量](#环境变量)
- [统一响应格式](#统一响应格式)
- [迁移](#迁移)
- [测试](#测试)

## 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go 1.23、Gin、GORM、go-redis v9、zap、golang-jwt v5、golang-migrate |
| 存储 | PostgreSQL 16、Redis 7 |
| 前端 | Vue 3 + Vite + TypeScript + Ant Design Vue 4 + Pinia |
| 工程 | Makefile、Docker、docker-compose、GitHub Actions |

## 目录结构

```
.
├── WhaleGate-backend/       # 后端（Go）
│   ├── cmd/whalegate        # 程序入口（含迁移子命令）
│   ├── configs/             # 配置（config.example.yaml 为模板）
│   ├── migrations/          # 版本化 SQL 迁移
│   └── internal/
│       ├── app/             # 应用装配与生命周期
│       ├── config/          # 配置加载（viper + 环境变量覆盖）
│       ├── constant/        # 全局常量
│       ├── handler/         # HTTP 处理器
│       ├── middleware/      # 中间件
│       ├── model/           # GORM 数据模型
│       ├── pkg/             # 可复用基础包（响应/错误码/日志/DB/Redis/加密/JWT/协议适配）
│       ├── router/          # 路由注册
│       └── service/         # 业务逻辑
├── WhaleGate-frontend/      # 前端（Vue3 + Vite + TS + AntDV）
├── docs/                    # 设计说明书与数据库字典
└── docker-compose.yml
```

## 文档

| 文档 | 内容 |
| --- | --- |
| [`docs/design-spec.md`](docs/design-spec.md) | 设计说明书：项目定位、架构分层、协议归一、计费与安全的设计决策与验收口径 |
| [`docs/database-dictionary.md`](docs/database-dictionary.md) | 数据库字典：全部表与字段的注释说明（注释随迁移写入 PostgreSQL，可直接在库内核对） |

## 快速开始

```bash
make init                 # 生成 WhaleGate-backend/configs/config.yaml 并安装前端依赖
make up                   # 启动 Postgres / Redis
make migrate              # 执行数据库迁移
make run                  # 启动服务端（默认 :8080）
```

前端开发：`cd WhaleGate-frontend && npm run dev`

## 功能面板

前端按「简体中文、现代设计、响应式」实现，分三个分组：

**用户门户**

| 页面 | 说明 |
| --- | --- |
| 概览 | 余额 / 累计消耗 / 近 N 天请求与 Token 卡片，用量趋势图表（ECharts），快速接入示例 |
| 接入指南 | 网关地址、鉴权方式、Python / Node / cURL / Gemini 原生可复制示例；**自定义参数传参详解**（默认透传）；**AI 客户端接入**（Cherry Studio / ChatBox / NextChat / LobeChat / Open WebUI / Cursor / Cline / Continue 的填法与配置下载）；常见问题 |
| 参数调试 | 选模型、勾选或自定义参数，预览哪些会写入协议字段 / 哪些原样透传 / 哪些被管理员拦下及原因，并查看最终发往上游的请求体；也可用真实密钥打一次实测。预览不消耗额度 |
| 密钥管理 | 创建（明文只展示一次）、列表、吊销、删除；**一键导入**（生成各客户端配置，可复制或下载）；展示创建时间、最近使用、累计请求、累计 Token 与累计点数（设了额度上限时直接显示「已用 / 上限」） |
| 调用历史 | 按模型 / 状态 / 时间范围筛选，展示 token、点数、耗时、首字延迟、计费置信度；生图调用显示张数、视频调用显示秒数 |
| 账号与安全 | 密码修改、Passkey / WebAuthn、GitHub 绑定 |

**创意**

| 页面 | 说明 |
| --- | --- |
| AI 绘图 | 在线文生图工作台：左侧提示词词库（10 个分类 + 关键词检索）选模板，右侧创作面板选模型 / 尺寸 / 张数生成，生成结果转存本地并沉淀到作品库 |
| 视频工作台 | **脚本创作**：左侧脚本模板词库（7 个分类）选模板，对话模型产出结构化分镜（画面 / 旁白 / 运镜 / 时长 / 英文画面提示词），每个镜头一键送进生成面板；**视频生成**：提交异步任务，作品库自动刷新进度；**作品库**：状态、进度、时长、扣点一览 |

**管理后台**（仅管理员可见）

| 页面 | 说明 |
| --- | --- |
| 用户管理 | 创建用户、启停账号、按元调整额度（1 元 = 1000 点） |
| 渠道管理 | 增删改查、连通性测试、**自动探测模型**（只填地址与密钥即可识别协议与模型）；支持模型名映射、模型别名 fork、参数注入、参数能力声明 |
| 倍率配置 | 以**已配置渠道的模型**为基准列出当前生效倍率（点 / 1K tokens）；同时列出各模型的**生图单价**（点 / 张）与**视频单价**（点 / 秒）；未单独配置的标记为「继承兜底」并回显 `*` 倍率，编辑即成为该模型的自定义倍率，也可一键恢复默认 |
| 系统设置 | 自用模式、技能与 MCP 开关、最大工具轮次、通行密钥（RP ID / Origins）、绘图工作台存储（本地 / S3 兼容）、HTTPS 证书热加载、管理员修改自身密码 |
| 分组管理 | 把倍率系数与 QPM / 并发上限打包成预设，供密钥创建时选用；密钥级配置优先于分组，分组为 0 时回落全局默认 |
| 全局日志 | 按用户 / 模型 / 状态 / 时间范围检索，含参数应用与丢弃明细 |
| 安全审计 | 登录 / 改密 / 密钥 / 渠道 / 额度 / 倍率 / 凭证 / 绑定等管理动作留痕，含操作人、IP、UA、追踪 ID，支持按用户、动作、时间范围筛选与 CSV 导出 |
| MCP 服务 | MCP Server 配置（stdio / HTTP，支持自定义请求头）、启停、连通性测试、工具发现与路由、代执行开关、调试调用 |
| 技能管理 | SKILL.md 目录扫描与导入、启停、全文注入 / 仅索引两种模式（列表中可直接切换） |
| 认证文件 | OAuth 凭证导入、搜索筛选、重命名、导出下载、配额展示 |

## 部署

### 方式一：Docker Compose（推荐）

```bash
git clone https://cnb.cool/JingYu588/open/WhaleGate.git
cd WhaleGate

# 1. 准备配置文件
cp WhaleGate-backend/configs/config.example.yaml WhaleGate-backend/configs/config.yaml

# 2. 生成密钥并按需修改其它配置（数据库、Redis）
#    jwt_secret 不短于 32 字节；encryption_key 必须恰好 32 字节
sed -i "s|^  jwt_secret:.*|  jwt_secret: $(openssl rand -hex 32)|" WhaleGate-backend/configs/config.yaml
sed -i "s|^  encryption_key:.*|  encryption_key: $(openssl rand -hex 16)|" WhaleGate-backend/configs/config.yaml
vim WhaleGate-backend/configs/config.yaml

# 3. 启动全部服务（Postgres / Redis / 网关）
docker compose up -d

# 4. 查看初始化管理员密码，或直接打开安装向导
docker compose logs app | grep -i password
```

浏览器访问 `http://<服务器IP>:8080`：

- 若系统尚未初始化（用户表为空），会自动进入**安装向导**，填写管理员账号密码即可完成初始化并自动登录
- 若容器日志已生成随机密码，则用 `admin` 登录后按提示立即改密

常用运维命令：

```bash
docker compose logs -f app        # 跟踪日志
docker compose restart app        # 重启网关
docker compose down               # 停止（保留数据卷）
docker compose pull && docker compose up -d   # 升级镜像
```

### 方式二：Docker 单容器

适合已有外部 Postgres / Redis 的场景：

```bash
docker run -d --name whalegate \
  -p 8080:8080 \
  -e WG_DATABASE_HOST=your-pg-host \
  -e WG_DATABASE_PASSWORD=your-pg-password \
  -e WG_REDIS_HOST=your-redis-host \
  -e WG_SECURITY_ENCRYPTION_KEY=<32字节随机字符串> \
  -e WG_SECURITY_JWT_SECRET=<随机字符串> \
  -v $(pwd)/WhaleGate-backend/configs:/app/configs \
  docker.cnb.cool/jingyu588/open/whalegate/whalegate:latest
```

### 方式三：源码构建（二进制部署）

```bash
# 构建前端
cd WhaleGate-frontend && npm install && npm run build && cd ..

# 构建后端（前端产物会被内嵌到静态资源目录）
cd WhaleGate-backend && go build -o ../bin/whalegate ./cmd/whalegate && cd ..

# 执行迁移并启动
./bin/whalegate -c WhaleGate-backend/configs/config.yaml -migrate
./bin/whalegate -c WhaleGate-backend/configs/config.yaml
```

`-migrate` 只执行数据库迁移后退出，适合在 CI / 发布流程中单独一步执行。

### 反向代理（Nginx）

流式响应必须关闭缓冲，否则 SSE 会被攒包：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

    # 流式关键配置
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 300s;
    chunked_transfer_encoding on;
}
```

### 升级与备份

- **升级**：先备份数据库，再拉取新镜像重启，迁移会在启动时自动执行
- **备份**：`docker compose exec postgres pg_dump -U whalegate whalegate > backup.sql`
- **恢复**：`docker compose exec -T postgres psql -U whalegate whalegate < backup.sql`

## 接入使用

管理员完成初始化后：

1. **渠道管理** → 填入上游 API 地址与密钥 → 点「自动探测模型」，自动识别协议类型、拉取模型列表与能力 → 勾选要开放的模型 → 保存
2. **密钥管理** → 生成 `sk-` 开头的 API Key（明文只展示一次）
3. **接入指南** → 复制对应语言的示例代码，替换 `base_url` 与 `api_key` 即可调用

网关对外地址（`/api/v1` 为统一出口，`/v1` 双轨兼容保留）：

| 协议 | 地址 |
| --- | --- |
| Base URL（OpenAI） | `http://<host>:8080/api/v1` |
| Base URL（Anthropic） | `http://<host>:8080/api`（SDK 自动追加 `/v1/messages`） |
| 对话 | `POST /api/v1/chat/completions` |
| Anthropic Messages | `POST /api/v1/messages` |
| 向量 | `POST /api/v1/embeddings` |
| 文生图 | `POST /api/v1/images/generations` |
| 视频生成 | `POST /api/v1/videos`（`GET /api/v1/videos/{id}` 查询、`GET /api/v1/videos/{id}/content` 取内容） |
| 模型列表 | `GET /api/v1/models` |
| Gemini 原生 | `POST /api/v1beta/models/{model}:generateContent` |

以上端点共用同一套 API Key、限流、计费与调用日志；旧地址 `/v1/*`（`/v1/chat/completions`、
`/v1beta/models/...` 等）全部保留，存量接入无需改动。

### 自定义参数透传

所有 `/v1` 与 `/api/v1` 端点都接受自定义参数（顶层字段或 SDK 的 `extra_body`）。默认行为是
**透传**：能力表之外的参数原样转发给上游，由上游裁决——不支持时上游的报错会**原样返回给调用方**
（例如上游返回 400，调用方收到的 message 就是上游的 400 明细）。只有管理员在渠道的
参数能力声明里显式 `exclude` 的参数才会被网关拦下。

- 全局开关：`gateway.param_passthrough`（默认 true）
- 渠道级覆盖：渠道「参数能力声明」JSON 里加 `{"passthrough": false}` 恢复旧的"先过滤再转发"
- 协议有映射的标准参数（`temperature` → Gemini `generationConfig.temperature` 等）仍走映射，
  不受透传影响

## 首次安装与安全

系统提供两种初始化方式：

**方式一：Web 安装向导（推荐）**

用户表为空时访问任意页面会自动跳转到 `/install`，填写管理员账号、密码即可完成初始化并自动登录，随后按引导添加渠道与密钥。

**方式二：容器自动创建**

容器首次启动（用户表为空）时自动创建管理员账号 `admin`，并生成 24 位随机强密码。

```bash
docker compose logs app | grep -i password
```

```
{"level":"WARN","msg":"初始化完成：已创建管理员账号，初始密码仅在此处显示一次，登录后请立即修改",
 "username":"admin","password":"xxxxxxxxxxxxxxxxxxxxxxxx"}
```

安全约定：

- 明文密码**只在容器日志中出现一次**（仅在用户表为空时生成，重启不会再打印）
- 数据库只保存 bcrypt 摘要，`/api/v1/me` 等接口不返回任何密码字段
- 访问日志对 `api_key` / `token` / `password` 等查询参数脱敏为 `***`
- 账号带 `must_change_password` 标记：除改密与登出外，其余已登录接口一律返回 403
- 前端登录后弹出不可跳过的改密弹窗，改密成功服务端签发新令牌
- 普通注册永远不会获得管理员角色（管理员只由初始化流程创建），避免抢注提权

相关配置：`security.bootstrap_admin`、`security.bootstrap_password_length`、`security.allow_registration`。

相关接口：`GET /api/v1/system/status`（查询是否已初始化）、`POST /api/v1/system/install`（仅在未初始化时可用，避免被用于提权）。

## 开源协议

本项目基于 **MIT License** 开源，详见 [LICENSE](./LICENSE)。

- 允许自由使用、复制、修改、合并、发布、分发、再许可与商业使用
- 需保留版权声明与许可声明
- 软件按「原样」提供，不含任何明示或暗示的担保

项目中 `WhaleGate-frontend/public/logo.png`、`logo-mark.png` 等品牌素材同样遵循该许可。

## 用户中心：第三方绑定

用户中心（`/account`）支持两种免密/第三方登录方式。

### 通行密钥（Passkey / WebAuthn）

基于 `go-webauthn/webauthn` 实现，凭据只保存在用户设备上（指纹 / 人脸 / 设备 PIN）。

- 注册：`POST /api/v1/account/passkey/register/begin` → `finish`，会话存 Redis，5 分钟有效且用完即焚
- 登录：`POST /api/v1/auth/passkey/login/begin` → `finish`，可发现凭证（无需输入用户名）
- 凭证加密落库，签名计数与最近使用时间自动更新
- 配置：`auth.webauthn.rp_id`（域名，不含端口）、`rp_origins`（必须与浏览器地址栏一致）

### GitHub 绑定与登录

- 绑定：`GET /api/v1/account/github/authorize?mode=bind` → 授权后回调完成绑定
- 登录：`GET /api/v1/account/github/authorize?mode=login`，回调后签发一次性票据并重定向回前端 `/login?ticket=...`
- 票据（60 秒有效、用完即焚）经 `POST /api/v1/auth/ticket` 换取令牌，令牌不出现在地址栏
- `state` 存 Redis 防 CSRF，10 分钟有效
- 配置：`auth.github.enabled / client_id / client_secret`，回调地址登记为 `{网关地址}/api/v1/auth/github/callback`

## 安全设计

### 用户数据安全

- 密码仅存 bcrypt 摘要，不写日志、不返回客户端
- API Key 明文只在创建时返回一次，库中只存 SHA-256 摘要与脱敏展示值
- 上游密钥、OAuth 令牌、WebAuthn 公钥、注入参数全部 AES-256-GCM 加密落库
- 通行密钥凭据只保存在用户设备，服务端仅存公钥与签名计数（用于克隆检测）
- 所有表与字段带注释，数据字典见 [`docs/database-dictionary.md`](docs/database-dictionary.md)

### 用户信息安全

- 列表接口对邮箱脱敏：`admin@corp.com` → `a***@corp.com`；本人接口返回完整值
- 访问日志对 `api_key` / `token` / `password` 等查询参数脱敏为 `***`
- 响应体不回传任何密钥、摘要、令牌字段（均标记 `json:"-"`）
- 内部错误明细只进日志，客户端只看到统一的错误码与文案

### 系统安全

- 安全响应头：`Content-Security-Policy`、`X-Frame-Options: DENY`、`X-Content-Type-Options: nosniff`、`Referrer-Policy`、`Permissions-Policy`、`Cross-Origin-Opener-Policy`（HTTPS 下可开 HSTS）
- 认证接口按 IP 限流（`security.auth_rate_limit`，默认 20 次/分钟）
- 登录防爆破：连续失败达阈值锁定账号（`login_max_attempts` / `login_lock_seconds`，默认 5 次 / 15 分钟），成功后清零
- 请求体大小上限 8MB（`http.MaxBytesReader`）
- 审计日志 `audit_logs`：登录、改密、密钥、渠道、额度、倍率、凭证、绑定等操作全留痕（含 IP / UA / 追踪 ID），异步批量落库，管理端可查 `/api/v1/admin/audit-logs`
- 初始管理员密码只打印到容器日志一次，并强制首次登录改密

## 关键设计

### 统一内部格式

所有外部协议先转换为 `UnifiedRequest` / `UnifiedResponse`（见 `internal/pkg/gateway/spec`），
再由渠道适配器转换为上游协议，因此 OpenAI / Gemini / Anthropic 之间可以任意互转转发。

- **非标扩展归一**：上游的 `message.images[]`（data URI）与 Gemini `inlineData`
  统一收敛到 `Images` 字段；输出侧按目标协议决定还原方式
  —— OpenAI 兼容输出保留 `message.images[]`，Gemini 输出转为 `inlineData` parts。
- **思维链计量**：`usage` 归一为 `prompt / completion / reasoning` 三项，
  计费按 `prompt + completion`（含 reasoning）合计扣减。

### 模型别名与参数注入

渠道上可配置 `model_alias`，把一个上游模型 fork 成多个对外模型名，每个别名可带独立 `override`：

```json
{
  "gpt-image-1k": { "model": "gpt-4o", "override": { "tools": [{"type": "image_generation", "size": "1024x1024"}] } },
  "gpt-image-2k": { "model": "gpt-4o", "override": { "tools": [{"type": "image_generation", "size": "2048x2048"}] } }
}
```

渠道级 `request_override` 对该渠道所有请求生效，别名级 `override` 覆盖渠道级（深度合并，数组整体替换）。

渠道编辑页的「高级配置」提供可视化编辑器，不需要手写 JSON：

- 每项标题旁的 `?` 给出用途说明与**可直接套用的示例**，点「填入示例」即展开成表单
- 键 / 值支持候选下拉（映射与别名的模型名取自该渠道已登记的模型，注入字段给出常用参数）
- 结构化值可展开为多行编辑，带**自动缩进美化与 JSON 语法校验**，非法内容无法应用
- 底部「配置预览」实时展示保存后写入渠道的 JSON，与提交使用同一份组装逻辑，所见即所得

### 统一 OpenAI 协议分发

对外统一以 OpenAI 协议（`/v1/chat/completions`）分发：任何渠道
（OpenAI 兼容 / Gemini 原生 / Anthropic 原生 / Claude Code 网关）的响应
都先归一为 `UnifiedResponse`，再以 OpenAI 格式返回，客户端无需关心上游协议。
`/v1/models` 汇总所有渠道（含别名）支持的模型。

### 自定义参数自动识别与分配

客户端可传任意自定义参数（顶层字段或 OpenAI SDK 的 `extra_body`），系统按上游声明的能力自动分配：

1. 参数名归一：`topK` / `top-k` / `max_output_tokens` 等 → 统一名（`top_k`、`max_tokens`…）
2. 能力协商：有协议映射且被 `exclude` 的参数丢弃；其余参数默认**透传**给上游（见上文"自定义参数透传"），
   渠道 `param_schema` 里 `{"passthrough": false}` 可恢复"先过滤再转发"
3. 协议映射：写入上游对应字段（`temperature` → Gemini `generationConfig.temperature`、`top_k` → `topK`、`response_format` → `responseMimeType`…）
4. 上游不接受时它的报错原样返回给调用方；分配结果通过 `X-WG-Params-Applied` / `X-WG-Params-Dropped` 回显，并写入调用日志

```json
{ "supported": ["temperature", "top_p", "top_k", "repetition_penalty", "max_tokens", "stop", "seed"], "passthrough": true }
```

#### 分组：倍率与限流的预设

管理员在「分组管理」里把倍率系数与限流上限打包成预设，密钥创建时选择分组即可继承：

- **倍率**：`最终点数 = 按模型倍率算出的点数 × 分组倍率系数`。
  与按模型的倍率正交，管理员只需给分组一个系数（如内部 0.5、默认 1、加急 2），
  不必为每个分组重复维护整张模型倍率表。
- **限流**：QPM 与并发按 **密钥级 > 分组级 > 全局默认** 逐层回落，每层的 `0` 都表示「该层不设限」。
- **删除分组不会打挂调用**：分组是「策略预设」而非「权限域」，密钥上记录的是分组标识（弱引用）。
  分组被删除或禁用后，其下密钥自动回落为中性策略（系数 1、不额外限流）。
  历史遗留的自由文本分组标签同理，因此引入分组无需回填任何存量数据。
- 分组策略按 **60 秒进程内缓存**解析，刻意不折进 `KeyIdentity`——
  密钥缓存最长 24 小时，折进去会让调整分组后长时间不生效。

不想靠试错确认参数是否生效，可在「参数调试」页预览：它复用同一套 `ApplyParams`，
按模型选出渠道后直接给出「已应用 / 被丢弃（含原因）/ 未标准化」三组结果与最终上游请求体，
不向上游发起请求、不消耗额度。

### AI 绘图工作台

图片生成是同步一次性返回，没有流式与工具调用，因此不经过 `UnifiedRequest` / `UnifiedResponse`，
而是独立的一对小类型 `ImageRequest` / `ImageResponse`（见 `internal/pkg/gateway/image.go`）：

- **上游协议映射**：OpenAI 兼容渠道走 `POST {base}/images/generations`；
  Gemini 原生渠道走 `generateContent` + `generationConfig.responseModalities: ["TEXT","IMAGE"]`，
  结果从 `parts[].inlineData` 提取。Anthropic 协议不提供图片生成，选路时直接跳过。
- **chat 接口回退**：聚合平台常把部分模型的生图能力只开在 chat 接口下
  （如 Gemini 系模型走 `modalities: ["image","text"]`，结果在 `message.images[]`，
  OpenRouter 风格）。原生端点返回 400/404/405/415/422 这类"端点不认该模型"的错误时，
  自动回退到 chat 接口重试一次；鉴权、限流、服务端故障不触发回退。
  `modalities` 已作为标准参数纳入能力表，因此 `/v1/chat/completions` 也可直接用于生图。
- **图片转存**：上游返回的 `b64_json` 与临时 `url` 都转存到配置的存储后端，
  上游链接通常几小时内失效，转存后作品库才能长期回看。
- **存储可插拔**（管理员在「系统设置 → 绘图工作台存储」配置，修改后立即生效，无需重启）：

  | 后端 | 适用 | 关键配置 |
  | --- | --- | --- |
  | 本地磁盘 | 单机部署 | 存储目录（留空则用 `studio.data_dir`）、可选公开访问前缀 |
  | 对象存储（S3 兼容） | 多实例 / 容器化部署 | 服务地址、桶名、区域、密钥、可选对象键前缀与公开访问前缀 |

  对象存储覆盖 AWS S3、Cloudflare R2、阿里云 OSS、腾讯 COS 与自建 MinIO；
  自建 MinIO 与部分国内厂商需开启「路径风格寻址」。
  配置了公开访问前缀时图片直连存储，否则经 `GET /api/v1/studio/works/{id}/images/{index}`
  按登录态与归属校验后回读。
  切换存储后**历史作品仍从原存储读取**——每件作品都记录了写入时使用的后端。
- **词库**：`prompt_templates` 表按 10 个分类维护可直接使用的提示词模板，迁移内含种子数据。
- **作品库**：`image_works` 记录每次生成的模型、提示词、参数、渠道、耗时与结果。
- **接口**：工作台走用户态 `/api/v1/studio/*`；同时对外提供 OpenAI 兼容的 `/v1/images/generations`，
  复用同一套渠道选路与重试逻辑。

> 计费说明：图片生成没有 token 维度的口径，因此**以张数计费**——
> `model_ratios.image_ratio` 为单价（点 / 张，可用 `*` 兜底，未单独配置时按
> `gateway.default_image_ratio` 计；要全局免费把它配成 0），`call_logs.image_count` 记录实际出图张数。
> 额度处理与 chat 转发一致：按请求张数预扣，成功按实际张数多退少补，失败全额回补，
> 余额不足在请求发出前即被拒绝（`40201`）。工作台与 `/v1/images/generations` 两个入口共用一份口径：
> 成功与失败都记录**渠道与上游模型**（失败恰恰最需要这两个字段），
> 且上游 429 / 404 / 超时分别映射为限流、模型不存在、超时错误码，而不是笼统的"上游调用失败"。

### 计量计费

- 倍率表 `model_ratios`：token 单位「点 / 1K tokens」，生图单位「点 / 张」（`image_ratio`），
  视频单位「点 / 秒」（`video_ratio`），1 点 = 0.001 元，`*` 为兜底。
  向量生成按 token 计费，与 chat 同一套倍率表。
- 生图按张计费：按请求张数预扣，成功按实际出图张数结算；单价未配置时回落到 `gateway.default_image_ratio`
  （默认 200 点/张，置 0 则全局免费）。
- 视频按秒计费：按请求秒数预扣，成功按实际生成秒数结算；单价未配置时回落到 `gateway.default_video_ratio`
  （默认 100 点/秒，置 0 则全局免费）。脚本创作按 token 计费，与 chat 转发同源。
- 额度预扣-回补：`ReserveQuota` 用 Redis Lua 原子预扣，请求结束 `Settle` 多退少补，失败全额回补。
- 余额不足直接拒绝（`40201`）。
- 调用日志 `call_logs` 异步批量落库，记录模型、渠道、耗时、token、点数与 `usage_confidence`
  （`reported` 上游上报 / `estimated` 按请求参数估算）。

## 环境变量

所有配置项均可用 `WG_` 前缀环境变量覆盖，例如：

```bash
WG_DATABASE_HOST=postgres WG_SECURITY_JWT_SECRET=xxx ./bin/whalegate
```

## 统一响应格式

```json
{
  "code": 0,
  "message": "ok",
  "data": {},
  "trace_id": "8f14e45f-ceea-467a-9f36-6d1c5a2b3c4d",
  "timestamp": 1730000000
}
```

`code` 为业务错误码（0 表示成功），HTTP 状态码与业务码保持语义一致（业务码 / 100）。
详见 `internal/pkg/apierr/code.go`。

## 迁移

```bash
make migrate           # 升级到最新
make migrate-down      # 回滚一版
make migrate-version   # 查看当前版本
```

## 测试

```bash
make test              # go test -race -cover
```
