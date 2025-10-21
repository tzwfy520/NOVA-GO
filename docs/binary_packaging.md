# 项目二进制文件打包说明

本文档详细说明了 Nova-Go SSH 采集器的构建、打包和部署流程。

## 构建脚本使用

### 基本用法

```bash
# 使用默认版本 1.0.0
./scripts/build.sh

# 指定版本号
VERSION=2.1.3 ./scripts/build.sh
```

### 构建过程

构建脚本会执行以下步骤：

1. **环境检查**：验证 Go 环境是否可用
2. **依赖管理**：下载和整理项目依赖
3. **跳过测试**：已禁用测试环节以加快构建速度
4. **代码检查**：运行 golangci-lint（如果可用）
5. **多平台编译**：为以下平台生成可执行文件
   - Linux (amd64, arm64)
   - macOS (amd64, arm64)
   - Windows (amd64, arm64)
6. **文件打包**：创建压缩包并复制配置文件

### 构建产物

构建完成后，文件将位于 `binaryfile/${VERSION}/` 目录下：

```
binaryfile/
└── 1.0.0/                              # 版本目录
    ├── configs/                         # 配置文件目录
    │   ├── config.yaml
    │   ├── dev.yaml
    │   └── prod.yaml
    ├── README.md                        # 项目说明文档
    ├── nova-go-1.0.0.linux-amd64       # Linux x64 可执行文件
    ├── nova-go-1.0.0.linux-amd64.tar.gz
    ├── nova-go-1.0.0.linux-arm64       # Linux ARM64 可执行文件
    ├── nova-go-1.0.0.linux-arm64.tar.gz
    ├── nova-go-1.0.0.darwin-amd64      # macOS x64 可执行文件
    ├── nova-go-1.0.0.darwin-amd64.tar.gz
    ├── nova-go-1.0.0.darwin-arm64      # macOS ARM64 可执行文件
    ├── nova-go-1.0.0.darwin-arm64.tar.gz
    ├── nova-go-1.0.0.windows-amd64.exe # Windows x64 可执行文件
    ├── nova-go-1.0.0.windows-amd64.zip
    ├── nova-go-1.0.0.windows-arm64.exe # Windows ARM64 可执行文件
    └── nova-go-1.0.0.windows-arm64.zip
```

### 文件命名规则

- **可执行文件**：`nova-go-${VERSION}.${OS}-${ARCH}[.exe]`
- **压缩包**：
  - Unix 系统：`nova-go-${VERSION}.${OS}-${ARCH}.tar.gz`
  - Windows 系统：`nova-go-${VERSION}.${OS}-${ARCH}.zip`

## 可执行文件运行

### 启动命令

```bash
# 基本启动
./nova-go-1.0.0.linux-amd64

# 指定配置文件
./nova-go-1.0.0.linux-amd64 -config configs/prod.yaml

# 指定端口（环境变量）
PORT=18001 ./nova-go-1.0.0.linux-amd64 -config configs/prod.yaml
```

### 2. 命令行参数

### 启动参数

| 参数 | 说明 | 默认值 | 示例 |
|------|------|--------|------|
| `-config` | 配置文件路径 | `configs/config.yaml` | `-config configs/prod.yaml` |

### 环境变量

可以通过环境变量覆盖部分配置：

```bash
# 指定服务端口
PORT=8080 ./nova-go-1.0.0.linux-amd64

# 指定版本号（构建时使用）
VERSION=1.0.0 ./scripts/build.sh
```

### 服务验证

启动后可以通过以下方式验证服务状态：

```bash
# 健康检查
curl http://localhost:18000/health

# 获取服务统计信息
curl http://localhost:18000/api/v1/collector/stats
```

## 运行目录结构

### 推荐的部署目录结构

```
/opt/nova-go/
├── bin/
│   └── nova-go-1.0.0.linux-amd64     # 可执行文件
├── configs/
│   ├── config.yaml                   # 主配置文件
│   ├── dev.yaml                      # 开发环境配置
│   └── prod.yaml                     # 生产环境配置
├── logs/                             # 日志目录
├── data/                             # 数据目录（SQLite等）
├── simulate/                         # 模拟器配置（可选）
│   ├── simulate.yaml
│   └── namespace/
└── docs/                             # 文档（可选）
    └── README.md
```

### 必需的目录和文件

- **可执行文件**：主程序二进制文件
- **configs/ 目录**：包含所有配置文件
- **logs/ 目录**：日志输出目录（程序会自动创建）

### 可选的目录和文件

- **simulate/ 目录**：如果需要使用设备模拟功能
- **data/ 目录**：用于存放 SQLite 数据库文件
- **docs/ 目录**：项目文档

### 权限要求

```bash
# 设置可执行文件权限
chmod +x nova-go-1.0.0.linux-amd64

# 设置目录权限
chmod 755 configs/ logs/ data/
chmod 644 configs/*.yaml
```

## 部署最佳实践

### 生产环境部署

#### 系统服务配置（systemd）

创建服务文件 `/etc/systemd/system/nova-go.service`：

```ini
[Unit]
Description=Nova Go SSH Collector
After=network.target

[Service]
Type=simple
User=nova-go
Group=nova-go
WorkingDirectory=/opt/nova-go
ExecStart=/opt/nova-go/bin/nova-go-1.0.0.linux-amd64 -config /opt/nova-go/configs/prod.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable nova-go
sudo systemctl start nova-go
sudo systemctl status nova-go
```

#### Docker 部署

创建 Dockerfile：

```dockerfile
FROM alpine:latest

# 安装必要的包
RUN apk --no-cache add ca-certificates tzdata

# 创建用户
RUN addgroup -g 1000 nova-go && \
    adduser -D -s /bin/sh -u 1000 -G nova-go nova-go

# 设置工作目录
WORKDIR /app

# 复制文件
COPY nova-go-1.0.0.linux-amd64 /app/nova-go
COPY configs/ /app/configs/

# 设置权限
RUN chmod +x /app/nova-go && \
    chown -R nova-go:nova-go /app

# 切换用户
USER nova-go

# 暴露端口
EXPOSE 18000

# 启动命令
CMD ["./nova-go", "-config", "configs/prod.yaml"]
```

### 2. 配置管理

#### 环境配置分离

- **开发环境**：使用 `configs/dev.yaml`
- **测试环境**：使用 `configs/test.yaml`
- **生产环境**：使用 `configs/prod.yaml`

#### 敏感信息管理

```yaml
# 使用环境变量替换敏感信息
database:
  password: ${DB_PASSWORD}
  
ssh:
  default_password: ${SSH_DEFAULT_PASSWORD}
```

### 3. 监控和日志

#### 日志配置

```yaml
log:
  level: "info"              # 生产环境建议使用 info 或 warn
  format: "json"             # 便于日志分析
  output: "file"
  file_path: "logs/nova-go.log"
  max_size: 100              # MB
  max_backups: 10
  max_age: 30                # 天
  compress: true
```

#### 监控指标

- 服务健康状态：`GET /health`
- 采集器统计：`GET /api/v1/collector/stats`
- 系统资源使用情况
- 日志错误率

## 故障排除

### 1. 常见问题

#### 端口占用

```bash
# 检查端口占用
lsof -i :18000

# 修改配置文件中的端口
server:
  port: 18001
```

#### 权限问题

```bash
# 检查文件权限
ls -la nova-go-v1.2.3-linux-amd64

# 设置执行权限
chmod +x nova-go-v1.2.3-linux-amd64
```

#### 配置文件问题

```bash
# 验证配置文件语法
./nova-go-v1.2.3-linux-amd64 -config configs/prod.yaml --validate-config
```

### 2. 日志分析

#### 查看实时日志

```bash
# 查看服务日志
journalctl -u nova-go -f

# 查看应用日志
tail -f logs/nova-go.log
```

#### 常见错误信息

| 错误信息 | 可能原因 | 解决方案 |
|----------|----------|----------|
| `bind: address already in use` | 端口被占用 | 修改端口或停止占用进程 |
| `permission denied` | 权限不足 | 检查文件权限和用户权限 |
| `config file not found` | 配置文件路径错误 | 检查配置文件路径 |
| `database connection failed` | 数据库连接失败 | 检查数据库配置和权限 |

### 3. 性能优化

#### 并发配置

```yaml
collector:
  concurrency_profile: "M"    # 根据服务器规格选择 S/M/L/XL
  concurrent: 16              # 手动指定并发数
```

#### 资源监控

```bash
# 监控进程资源使用
top -p $(pgrep nova-go)

# 监控网络连接
netstat -tulpn | grep nova-go
```

## 版本升级

### 1. 升级步骤

1. **备份当前版本**
2. **停止服务**
3. **替换可执行文件**
4. **更新配置文件**（如有必要）
5. **启动新版本**
6. **验证服务状态**

### 2. 回滚方案

保留前一个版本的可执行文件，以便快速回滚：

```bash
# 备份当前版本
cp nova-go-v1.2.3-linux-amd64 nova-go-v1.2.3-linux-amd64.backup

# 回滚到备份版本
cp nova-go-v1.2.3-linux-amd64.backup nova-go-v1.2.3-linux-amd64
```

## 联系支持

如果遇到问题，请提供以下信息：

- 操作系统和架构
- 程序版本号
- 配置文件内容（去除敏感信息）
- 错误日志
- 复现步骤

---

更多详细信息请参考项目 README.md 和 API 文档。