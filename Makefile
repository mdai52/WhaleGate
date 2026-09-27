APP_NAME   := whalegate
BACKEND    := WhaleGate-backend
FRONTEND   := WhaleGate-frontend
BIN_DIR    := bin
CMD_DIR    := ./$(BACKEND)/cmd/whalegate
GO         := go
GOFLAGS    ?=
LDFLAGS    := -s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# ---------------------------------------------------------------- 基础命令
.PHONY: help
help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: init
init: ## 初始化开发环境（生成配置文件 + 安装前端依赖）
	@test -f $(BACKEND)/configs/config.yaml || cp $(BACKEND)/configs/config.example.yaml $(BACKEND)/configs/config.yaml
	cd $(FRONTEND) && npm install

.PHONY: fmt
fmt: ## 格式化代码
	cd $(BACKEND) && $(GO) fmt ./...
	cd $(FRONTEND) && npm run format --if-present

.PHONY: vet
vet: ## go vet 静态检查
	cd $(BACKEND) && $(GO) vet ./...

.PHONY: lint
lint: ## golangci-lint（未安装则回退到 go vet）
	cd $(BACKEND) && (command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || $(GO) vet ./...)

.PHONY: test
test: ## 单元测试（含竞态 + 覆盖率）
	cd $(BACKEND) && $(GO) test -race -covermode=atomic -coverprofile=coverage.out ./...

.PHONY: cover
cover: test ## 查看覆盖率报告
	cd $(BACKEND) && $(GO) tool cover -html=coverage.out -o coverage.html

# ---------------------------------------------------------------- 构建 / 运行
.PHONY: build
build: ## 构建后端二进制
	cd $(BACKEND) && CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags "$(LDFLAGS)" -o ../$(BIN_DIR)/$(APP_NAME) ./cmd/whalegate

.PHONY: build-web
build-web: ## 构建前端静态资源
	cd $(FRONTEND) && npm run build

.PHONY: build-all
build-all: build-web build ## 构建全部产物

.PHONY: run
run: ## 本地运行服务端（在后端目录启动，静态资源按 ../WhaleGate-frontend/dist 定位）
	cd $(BACKEND) && $(GO) run ./cmd/whalegate -c configs/config.yaml

.PHONY: dev
dev: ## 前后端并行开发（需 air + npm）
	cd $(FRONTEND) && npm run dev &
	air -c .air.toml

# ---------------------------------------------------------------- 数据库迁移
.PHONY: migrate
migrate: ## 执行迁移到最新版本
	cd $(BACKEND) && $(GO) run ./cmd/whalegate -c configs/config.yaml -migrate

.PHONY: migrate-down
migrate-down: ## 回滚一个版本
	cd $(BACKEND) && $(GO) run ./cmd/whalegate -c configs/config.yaml -migrate-down

.PHONY: migrate-version
migrate-version: ## 查看迁移版本
	cd $(BACKEND) && $(GO) run ./cmd/whalegate -c configs/config.yaml -migrate-version

# ---------------------------------------------------------------- 依赖 / 容器
.PHONY: tidy
tidy: ## 整理 go.mod
	cd $(BACKEND) && $(GO) mod tidy

.PHONY: up
up: ## 启动依赖容器（Postgres / Redis）
	docker compose up -d postgres redis

.PHONY: down
down: ## 停止所有容器
	docker compose down

.PHONY: docker-build
docker-build: ## 构建镜像
	docker build -t $(APP_NAME):latest .

.PHONY: clean
clean: ## 清理构建产物
	rm -rf $(BIN_DIR) $(BACKEND)/coverage.out $(BACKEND)/coverage.html $(FRONTEND)/dist
