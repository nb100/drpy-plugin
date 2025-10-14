# MediaProxy Makefile
# 提供简洁的构建命令

.PHONY: all build clean help install dev test fmt vet deps

# 项目信息
APP_NAME := mediaProxy
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S_UTC')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# 构建目录
BUILD_DIR := build
DIST_DIR := dist

# Go构建参数
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)
BUILD_FLAGS := -ldflags="$(LDFLAGS)" -trimpath

# 默认目标
all: clean deps build

# 构建当前平台
build:
	@echo "构建 $(APP_NAME) v$(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(APP_NAME) .
	@echo "构建完成: $(BUILD_DIR)/$(APP_NAME)"

# 构建所有平台
build-all: clean deps
	@echo "构建所有平台..."
	@chmod +x build.sh
	@./build.sh --all

# 构建指定平台
# 用法: make build-platform PLATFORM=linux/amd64
build-platform: clean deps
	@echo "构建平台: $(PLATFORM)"
	@chmod +x build.sh
	@./build.sh --platform $(PLATFORM)

# 开发模式构建（快速构建，不优化）
dev:
	@echo "开发模式构建..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) .
	@echo "开发构建完成: $(BUILD_DIR)/$(APP_NAME)"

# 运行程序
run: dev
	@echo "启动 $(APP_NAME)..."
	@$(BUILD_DIR)/$(APP_NAME)

# 运行程序（调试模式）
run-debug: dev
	@echo "启动 $(APP_NAME) (调试模式)..."
	@$(BUILD_DIR)/$(APP_NAME) -debug

# 安装到系统
install: build
	@echo "安装 $(APP_NAME) 到系统..."
	@sudo cp $(BUILD_DIR)/$(APP_NAME) /usr/local/bin/
	@echo "安装完成"

# 清理构建文件
clean:
	@echo "清理构建文件..."
	@rm -rf $(BUILD_DIR) $(DIST_DIR)
	@echo "清理完成"

# 下载依赖
deps:
	@echo "下载依赖..."
	@go mod tidy
	@go mod download

# 运行测试
test:
	@echo "运行测试..."
	@go test -v ./...

# 代码格式化
fmt:
	@echo "格式化代码..."
	@go fmt ./...

# 代码检查
vet:
	@echo "代码检查..."
	@go vet ./...

# 代码质量检查（需要安装golangci-lint）
lint:
	@echo "代码质量检查..."
	@golangci-lint run

# 生成版本信息
version:
	@echo "应用名称: $(APP_NAME)"
	@echo "版本: $(VERSION)"
	@echo "构建时间: $(BUILD_TIME)"
	@echo "Git提交: $(GIT_COMMIT)"

# Docker构建
docker-build:
	@echo "构建Docker镜像..."
	@docker build -t $(APP_NAME):$(VERSION) .
	@docker tag $(APP_NAME):$(VERSION) $(APP_NAME):latest
	@echo "Docker镜像构建完成"

# 创建发布
release: clean deps build-all
	@echo "创建发布包..."
	@echo "发布包位置: $(DIST_DIR)/"
	@ls -la $(DIST_DIR)/

# 快速开始（首次使用）
quickstart: deps build
	@echo ""
	@echo "=========================================="
	@echo "MediaProxy 快速开始"
	@echo "=========================================="
	@echo "1. 启动服务: make run"
	@echo "2. 调试模式: make run-debug"
	@echo "3. 测试请求:"
	@echo "   curl \"http://localhost:57574/?url=https://httpbin.org/json\""
	@echo ""
	@echo "更多命令: make help"
	@echo "=========================================="

# 显示帮助
help:
	@echo "MediaProxy 构建系统"
	@echo ""
	@echo "常用命令:"
	@echo "  make build          构建当前平台"
	@echo "  make build-all      构建所有平台"
	@echo "  make dev            开发模式构建"
	@echo "  make run            运行程序"
	@echo "  make run-debug      调试模式运行"
	@echo "  make clean          清理构建文件"
	@echo "  make test           运行测试"
	@echo "  make quickstart     快速开始"
	@echo ""
	@echo "高级命令:"
	@echo "  make build-platform PLATFORM=linux/amd64  构建指定平台"
	@echo "  make install        安装到系统"
	@echo "  make release        创建发布包"
	@echo "  make docker-build   构建Docker镜像"
	@echo ""
	@echo "代码质量:"
	@echo "  make fmt            格式化代码"
	@echo "  make vet            代码检查"
	@echo "  make lint           代码质量检查"
	@echo ""
	@echo "其他:"
	@echo "  make deps           下载依赖"
	@echo "  make version        显示版本信息"
	@echo "  make help           显示此帮助"