# SSH采集器专业版 (SSH Collector Pro)

一个高性能、可扩展的SSH设备信息采集系统，支持分布式部署与实时监控。

## 🚀 功能特性

### 核心功能
- **SSH连接管理**: 支持密码和密钥认证，连接池复用
- **设备信息采集**: 自动采集系统信息、性能指标、配置文件等
- **分布式架构**: 支持多节点部署，负载均衡
- **任务执行**: 支持并发执行与取消，提供任务状态查询
 
- **数据存储**: SQLite本地存储

### 技术特性
- **高性能**: Go语言开发，并发处理能力强
- **容器化**: Docker一键部署，支持Docker Compose
- **RESTful API**: 完整的API接口，支持第三方集成
- **配置管理**: YAML配置文件，支持环境变量覆盖
- **日志系统**: 结构化日志，支持多种输出格式
- **健康检查**: 内置健康检查接口

## 📋 系统要求

### 最低要求
- **操作系统**: Linux/macOS/Windows
- **内存**: 512MB RAM
- **存储**: 1GB可用空间
- **网络**: 支持SSH连接的网络环境

### Docker部署要求
- **Docker**: 20.10+
- **Docker Compose**: 2.0+
- **内存**: 2GB RAM (包含所有服务)
- **存储**: 5GB可用空间

## 🛠️ 快速开始

> 📖 **详细部署指南**: 请参考 [部署文档](docs/DEPLOYMENT.md) 获取完整的部署说明、问题解决方案和最佳实践。

### 方式一：Docker部署（推荐）

1. **克隆项目**
```bash
git clone https://github.com/your-org/sshcollectorpro.git
cd sshcollectorpro
```

2. **一键部署**
```bash
./deploy/deploy.sh
```

3. **访问服务**
- SSH采集器API: http://localhost:18000

### 方式二：直接部署（生产环境推荐）

1. **本地编译**
```bash
# 编译Linux版本
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sshcollector-linux ./cmd/server
```

2. **部署到服务器**
```bash
# 传输文件到服务器
scp sshcollector-linux your-server:/opt/ssh-collector-pro/sshcollector

# SSH连接到服务器
ssh your-server

# 启动服务
cd /opt/ssh-collector-pro
mkdir -p logs data temp
chmod +x sshcollector
nohup ./sshcollector > logs/app.log 2>&1 &
```

3. **验证部署**
```bash
# 检查服务状态
curl -s http://localhost:8100/api/v1/health
```

### 方式三：源码编译

1. **环境准备**
```bash
# 安装Go 1.21+
go version

# 安装依赖
go mod download
```

2. **编译项目**
```bash
./scripts/build.sh
```

3. **运行服务**
```bash
./dist/sshcollector-linux-amd64 -config configs/config.yaml
```

## 📖 使用指南

### 设备管理

#### 添加设备
```bash
curl -X POST http://localhost:18000/api/devices \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Web服务器01",
    "ip": "192.168.1.100",
    "port": 22,
    "username": "root",
    "password": "your_password",
    "device_type": "linux"
  }'
```

#### 测试连接
```bash
curl -X POST http://localhost:18000/api/devices/1/test
```

### 任务执行

#### 执行采集任务
```bash
curl -X POST http://localhost:18000/api/collector/execute \
  -H "Content-Type: application/json" \
  -d '{
    "device_ip": "192.168.1.100",
    "port": 22,
    "username": "root",
    "password": "your_password",
    "commands": ["uptime", "df -h", "free -m"]
  }'
```

#### 查看任务状态
```bash
curl http://localhost:18000/api/collector/status/task_id_here
```

### 批量执行
```bash
curl -X POST http://localhost:18000/api/collector/batch \
  -H "Content-Type: application/json" \
  -d '{
    "requests": [
      {
        "device_ip": "192.168.1.100",
        "commands": ["uptime"]
      },
      {
        "device_ip": "192.168.1.101", 
        "commands": ["df -h"]
      }
    ]
  }'
```

## 🔧 配置说明

### 主配置文件 (configs/config.yaml)

```yaml
# 服务器配置
server:
  host: "0.0.0.0"
  port: 18000
  read_timeout: 30
  write_timeout: 30

# 数据库配置
database:
  sqlite:
    path: "data/sshcollector.db"
    max_open_conns: 25
    max_idle_conns: 5


# SSH配置
ssh:
  timeout: 30
  max_connections: 100

# 采集器配置
collector:
  name: "sshcollector-01"
  tags: ["production", "datacenter-1"]
  heartbeat_interval: 30

# 采集器配置
collector:
  id: "collector-001"
  type: "ssh"
  version: "1.0.0"
  tags: ["production", "ssh"]
  threads: 10
  concurrent: 5

# 日志配置
log:
  level: "info"
  output: "file"
  file_path: "logs/sshcollector.log"
  max_size: 100
  max_backups: 10
  max_age: 30
```

### 环境变量覆盖

```bash
# 服务器配置
export SERVER_HOST=0.0.0.0
export SERVER_PORT=18000

# 数据库配置
export DATABASE_SQLITE_PATH=/app/data/sshcollector.db


# 日志配置
export LOG_LEVEL=info
export LOG_OUTPUT=file
```

## 🐳 Docker部署详解

### 服务架构

```
┌─────────────────┐
│  SSH Collector  │
│   (核心服务)     │
│   Port: 18000   │
└─────────────────┘
```

### 部署脚本命令

```bash
# 完整部署
./deploy/deploy.sh deploy

# 启动服务
./deploy/deploy.sh start

# 停止服务
./deploy/deploy.sh stop

# 重启服务
./deploy/deploy.sh restart

# 查看状态
./deploy/deploy.sh status

# 查看日志
./deploy/deploy.sh logs [service_name]

# 清理环境
./deploy/deploy.sh clean
```

### 数据持久化

本地目录映射：
- `./data`: 应用数据目录
- `./logs`: 应用日志目录
- `./configs`: 配置文件目录

 

## 🔍 故障排查

### 常见问题

1. **服务启动失败**
```bash
# 查看服务日志
./deploy/deploy.sh logs sshcollector

# 检查配置文件
cat configs/config.yaml
```

2. **SSH连接失败**
```bash
# 测试网络连通性
telnet target_host 22

# 检查认证信息
ssh username@target_host
```

3. **数据库连接问题**
```bash
# 检查SQLite文件权限
ls -la data/sshcollector.db

```

### 日志分析

```bash
# 查看应用日志
tail -f logs/sshcollector.log

# 查看Docker容器日志
docker logs sshcollector

# 查看系统资源使用
docker stats
```

## 🚀 性能优化

### 连接池配置
```yaml
ssh:
  max_connections: 100    # 最大连接数
  timeout: 30            # 连接超时时间
  keep_alive: 300        # 连接保持时间
```

### 数据库优化
```yaml
database:
  sqlite:
    max_open_conns: 25   # 最大打开连接数
    max_idle_conns: 5    # 最大空闲连接数
    conn_max_lifetime: 300 # 连接最大生命周期
```

 

## 🔐 安全配置

### SSH密钥认证
```yaml
ssh:
  auth_method: "key"     # 认证方式：password/key
  private_key_path: "/path/to/private/key"
  passphrase: "key_passphrase"
```

### API访问控制
```yaml
server:
  enable_auth: true      # 启用API认证
  api_key: "your_api_key"
  rate_limit: 1000       # 请求频率限制
```

### 数据加密
```yaml
security:
  encrypt_passwords: true # 加密存储密码
  encryption_key: "32_char_encryption_key_here"
```

## 📚 API文档

### 设备管理API

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | /api/devices | 创建设备 |
| GET | /api/devices | 获取设备列表 |
| GET | /api/devices/{id} | 获取设备详情 |
| PUT | /api/devices/{id} | 更新设备 |
| DELETE | /api/devices/{id} | 删除设备 |
| POST | /api/devices/{id}/test | 测试设备连接 |

### 采集器API

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | /api/collector/execute | 执行采集任务 |
| POST | /api/collector/batch | 批量执行任务 |
| GET | /api/collector/status/{taskId} | 获取任务状态 |
| DELETE | /api/collector/cancel/{taskId} | 取消任务 |
| GET | /api/collector/stats | 获取统计信息 |

### 系统API

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | /health | 健康检查 |
 
| GET | /api/stats | 系统统计 |

## 🤝 贡献指南

### 开发环境搭建

1. **Fork项目**
2. **克隆代码**
```bash
git clone https://github.com/your-username/sshcollectorpro.git
cd sshcollectorpro
```

3. **安装依赖**
```bash
go mod download
```

4. **运行测试**
```bash
go test -v ./...
```

5. **代码检查**
```bash
golangci-lint run
```

### 提交规范

- feat: 新功能
- fix: 修复bug
- docs: 文档更新
- style: 代码格式调整
- refactor: 代码重构
- test: 测试相关
- chore: 构建过程或辅助工具的变动

## 📄 许可证

本项目采用 [MIT License](LICENSE) 许可证。

## 📞 支持与反馈

- **Issues**: [GitHub Issues](https://github.com/your-org/sshcollectorpro/issues)
- **讨论**: [GitHub Discussions](https://github.com/your-org/sshcollectorpro/discussions)
- **邮箱**: support@sshcollectorpro.com

## 🎯 路线图

### v1.1.0 (计划中)
- [ ] Web管理界面
- [ ] 更多设备类型支持
- [ ] 插件系统
- [ ] 集群模式

### v1.2.0 (计划中)
- [ ] 机器学习异常检测
- [ ] 自动化运维脚本
- [ ] 移动端应用
- [ ] 多租户支持

---

**SSH采集器专业版** - 让设备管理更简单、更高效！ 🚀