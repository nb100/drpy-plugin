# MediaProxy Dockerfile
# 多阶段构建，优化镜像大小

# 构建阶段
FROM golang:1.21-alpine AS builder

# 设置工作目录
WORKDIR /app

# 安装构建依赖
RUN apk add --no-cache git ca-certificates tzdata

# 复制go mod文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
ARG VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}" \
    -trimpath \
    -o mediaProxy .

# 运行阶段
FROM alpine:latest

# 安装运行时依赖
RUN apk --no-cache add ca-certificates tzdata

# 创建非root用户
RUN addgroup -g 1001 -S mediaproxy && \
    adduser -u 1001 -S mediaproxy -G mediaproxy

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/mediaProxy .

# 复制静态文件
COPY --from=builder /app/static ./static

# 设置文件权限
RUN chown -R mediaproxy:mediaproxy /app

# 切换到非root用户
USER mediaproxy

# 暴露端口
EXPOSE 57574

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:57574/ || exit 1

# 启动命令
ENTRYPOINT ["./mediaProxy"]
CMD ["-port", "57574"]