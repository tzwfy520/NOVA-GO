# 项目二进制文件打包说明

本文档详细介绍了 SSH 采集器专业版的二进制文件打包、构建和部署流程。

## 目录
- [构建脚本使用](#构建脚本使用)
- [可执行文件运行](#可执行文件运行)
- [运行目录结构](#运行目录结构)
- [部署最佳实践](#部署最佳实践)
- [故障排除](#故障排除)

## 构建脚本使用

### 1. 脚本位置
构建脚本位于项目根目录的 `scripts/build.sh`。

### 2. 基本使用

```bash
# 使用默认版本 1.0.0 构建
./scripts/build.sh

# 指定版本号构建
VERSION=1.2.3 ./scripts/build.sh

# 指定其他版本
VERSION=2.0.0 ./scripts/build.sh
```

### 3. 构建过程

构建脚本会自动执行以下步骤：

1. **环境检查**：验证 Go 环境和依赖
2. **代码质量检查**：运行测试和代码检查（如果安装了 golangci-lint）
3. **多平台编译**：为以下平台构建可执行文件：
   - Linux (amd64, arm64)
   - macOS/Darwin (amd64, arm64)
   - Windows (amd64, arm64)
4. **文件打包**：复制配置文件和文档
5. **压缩归档**：创建对应的压缩包

### 4. 构建输出

构建完成后，所有文件将位于 `binaryfile/nova-go-v${VERSION}/` 目录下：

```
binaryfile/nova-go-v1.2.3/
├── linux/
│   ├── nova-go-v1.2.3-linux-amd64
│   ├── nova-go-v1.2.3-linux-amd64.tar.gz
│   ├── nova-go-v1.2.3-linux-arm64
│   ├── nova-go-v1.2.3-linux-arm64.tar.gz
│   ├── configs/
│   └── README.md
├── darwin/
│   ├── nova-go-v1.2.3-darwin-amd64
│   ├── nova-go-v1.2.3-darwin-amd64.tar.gz
│   ├── nova-go-v1.2.3-darwin-arm64
│   ├── nova-go-v1.2.3-darwin-arm64.tar.gz
│   ├── configs/
│   └── README.md
└── windows/
    ├── nova-go-v1.2.3-windows-amd64.exe
    ├── nova-go-v1.2.3-windows-amd64.zip
    ├── nova-go-v1.2.3-windows-arm64.exe
    ├── nova-go-v1.2.3-windows-arm64.zip
    ├── configs/
    └── README.md
```

## 可执行文件运行

### 1. 基本运行

```bash
# Linux/macOS
./nova-go-v1.2.3-linux-amd64 -config configs/dev.yaml

# Windows
nova-go-v1.2.3-windows-amd64.exe -config configs/dev.yaml
```

### 2. 命令行参数

| 参数 | 说明 | 默认值 | 示例 |
|------|------|--------|------|
| `-config` | 配置文件路径 | `configs/config.yaml` | `-config configs/prod.yaml` |

### 3. 环境变量

可以通过环境变量覆盖部分配置：

```bash
# 指定服务端口
PORT=8080 ./nova-go-v1.2.3-linux-amd64

# 指定版本号（构建时使用）
VERSION=1.2.3 ./scripts/build.sh
```

### 4. 服务验证

启动后可以通过以下方式验证服务状态：

```bash
# 健康检查
curl http://localhost:18000/health

# 获取服务统计信息
curl http://localhost:18000/api/v1/collector/stats
```

## 运行目录结构

### 1. 推荐的部署目录结构

```
/opt/nova-go/
├── bin/
│   └── nova-go-v1.2.3-linux-amd64    # 可执行文件
├── configs/
│   ├── config.yaml                    # 主配置文件
│   ├── dev.yaml                       # 开发环境配置
│   └── prod.yaml                      # 生产环境配置
├── logs/                              # 日志目录
├── data/                              # 数据目录（SQLite等）
├── simulate/                          # 模拟器配置（可选）
│   ├── simulate.yaml
│   └── namespace/
└── docs/                              # 文档（可选）
    └── README.md
```

### 2. 必需的目录和文件

- **可执行文件**：主程序二进制文件
- **configs/ 目录**：包含所有配置文件
- **logs/ 目录**：日志输出目录（程序会自动创建）

### 3. 可选的目录和文件

- **simulate/ 目录**：如果需要使用设备模拟功能
- **data/ 目录**：用于存放 SQLite 数据库文件
- **docs/ 目录**：项目文档

### 4. 权限要求

```bash
# 设置可执行文件权限
chmod +x nova-go-v1.2.3-linux-amd64

# 设置目录权限
chmod 755 configs/ logs/ data/
chmod 644 configs/*.yaml
```

## 部署最佳实践

### 1. 生产环境部署

#### 系统服务配置（systemd）

创建服务文件 `/etc/systemd/system/nova-go.service`：

```ini
[Unit]
Description=Nova Go SSH Collector Pro
After=network.target

[Service]
Type=simple
User=nova-go
Group=nova-go
WorkingDirectory=/opt/nova-go
ExecStart=/opt/nova-go/bin/nova-go-v1.2.3-linux-amd64 -config /opt/nova-go/configs/prod.yaml
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
COPY nova-go-v1.2.3-linux-amd64 /app/nova-go
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