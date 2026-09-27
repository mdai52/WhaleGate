<div align="center">

<img src="web/public/logo-mark.png" width="96" alt="WhaleGate logo" />

# 鲸闸 WhaleGate

</div>

统一的 AI 大模型 API 网关：一份 OpenAI / Gemini 兼容协议入口，背后聚合多家上游渠道，
提供模型路由、负载均衡、失败重试、计量计费与用量审计。

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
└── docker-compose.yml
```

## 快速开始

```bash
make init                 # 生成 WhaleGate-backend/configs/config.yaml 并安装前端依赖
make up                   # 启动 Postgres / Redis
make migrate              # 执行数据库迁移
make run                  # 启动服务端（默认 :8080）
```

前端开发：`cd WhaleGate-frontend && npm run dev`

## 功能面板

前端按「简体中文、现代设计、响应式」实现，分两个分组：

**用户门户**

| 页面 | 说明 |
| --- | --- |
| 概览 | 余额 / 累计消耗 / 近 N 天请求与 Token 卡片，用量趋势图表（ECharts），快速接入示例 |
| 接入指南 | 网关地址、鉴权方式、Python / Node / cURL / Gemini 原生可复制示例、常见问题 |
| 密钥管理 | 创建（明文只展示一次）、列表、吊销、删除；展示创建时间、最近使用、累计请求与累计 Token |
| 调用历史 | 按模型 / 状态 / 时间范围筛选，展示 token、点数、耗时、首字延迟、计费置信度 |
| 账号与安全 | 密码修改、Passkey / WebAuthn、GitHub 绑定 |

**管理后台**（仅管理员可见）

| 页面 | 说明 |
| --- | --- |
| 用户管理 | 创建用户、启停账号、按元调整额度（1 元 = 1000 点） |
| 渠道管理 | 增删改查、连通性测试、**自动探测模型**（只填地址与密钥即可识别协议与模型）；支持模型名映射、模型别名 fork、参数注入、参数能力声明 |
| 倍率配置 | 模型倍率表（点 / 1K tokens）增删改查，`*` 为兜底 |
| 全局日志 | 按用户 / 模型 / 状态 / 时间范围检索，含参数应用与丢弃明细 |
| MCP 服务 | MCP Server 配置（stdio / HTTP）、工具发现与路由、代执行开关、调试调用 |
| 技能管理 | SKILL.md 目录扫描与导入、启停、全文注入 / 仅索引两种模式 |
| 认证文件 | OAuth 凭证导入、搜索筛选、重命名、导出下载、配额展示 |
| 系统设置 | 自用模式（开启后免计费）、技能与 MCP 开关、最大工具轮次、管理员修改自身密码 |

## 部署

### 方式一：Docker Compose（推荐）

```bash
git clone https://cnb.cool/JingYu588/open/WhaleGate.git
cd WhaleGate

# 1. 准备配置文件
cp WhaleGate-backend/configs/config.example.yaml WhaleGate-backend/configs/config.yaml

# 2. 按需修改关键配置（数据库、Redis、加密密钥、JWT 密钥）
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
  -e WG_SECRET_ENCRYPTION_KEY=<32字节随机字符串> \
  -e WG_JWT_SECRET=<随机字符串> \
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

网关对外地址：

| 协议 | 地址 |
| --- | --- |
| OpenAI 兼容 | `http://<host>:8080/v1/chat/completions` |
| 模型列表 | `http://<host>:8080/v1/models` |
| Gemini 原生 | `http://<host>:8080/v1beta/models/{model}:generateContent` |

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
再由渠道适配器转换为上游协议，因此 OpenAI 与 Gemini 可以互相转发。

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

### 统一 OpenAI 协议分发

对外统一以 OpenAI 协议（`/v1/chat/completions`）分发：任何渠道（OpenAI 兼容 / Gemini 原生）的响应
都先归一为 `UnifiedResponse`，再以 OpenAI 格式返回，客户端无需关心上游协议。
`/v1/models` 汇总所有渠道（含别名）支持的模型。

### 自定义参数自动识别与分配

客户端可传任意自定义参数（顶层字段或 OpenAI SDK 的 `extra_body`），系统按上游声明的能力自动分配：

1. 参数名归一：`topK` / `top-k` / `max_output_tokens` 等 → 统一名（`top_k`、`max_tokens`…）
2. 能力协商：按渠道 `param_schema.supported` 过滤（未声明则用协议默认集，可用 `exclude` 排除）
3. 协议映射：写入上游对应字段（`temperature` → Gemini `generationConfig.temperature`、`top_k` → `topK`、`response_format` → `responseMimeType`…）
4. 不支持的参数丢弃，避免上游报错；分配结果通过 `X-WG-Params-Applied` / `X-WG-Params-Dropped` 回显，并写入调用日志

```json
{ "supported": ["temperature", "top_p", "top_k", "repetition_penalty", "max_tokens", "stop", "seed"] }
```

### 计量计费

- 倍率表 `model_ratios`：单位「点 / 1K tokens」，1 点 = 0.001 元，`*` 为兜底。
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
