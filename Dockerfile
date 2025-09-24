# 使用官方Go镜像作为构建环境
FROM golang:1.24-alpine AS builder

# 设置工作目录
WORKDIR /app

# 安装必要的包
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev

# 设置Go代理
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.google.cn

# 复制go mod文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o sshcollector ./cmd/server

# 使用轻量级的alpine镜像作为运行环境
FROM alpine:latest

# 安装必要的包
RUN apk --no-cache add ca-certificates tzdata sqlite

# 设置时区
ENV TZ=Asia/Shanghai

# 创建非root用户
RUN addgroup -g 1001 -S sshcollector && \
    adduser -u 1001 -S sshcollector -G sshcollector

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/sshcollector .

# 创建必要的目录
RUN mkdir -p /app/configs /app/logs /app/data /app/temp && \
    chown -R sshcollector:sshcollector /app

# 复制配置文件
COPY configs/config.yaml /app/configs/

# 切换到非root用户
USER sshcollector

# 暴露端口
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# 启动命令
CMD ["./sshcollector", "-config", "/app/configs/config.yaml"]