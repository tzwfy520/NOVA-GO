# SSH采集器专业版部署指南

本文档详细记录了SSH采集器专业版的部署过程、遇到的问题及解决方案。

## 部署环境

### 服务器信息
- **服务器**: huoshan-1 (115.190.80.219)
- **操作系统**: Ubuntu 22.04
- **内存**: 2GB+
- **存储**: 10GB+

### 软件要求
- Docker 27.5.1+
- Go 1.21+ (可选，用于本地编译)
- SSH客户端

## 部署方式

### 方式一：直接部署二进制文件（推荐）

#### 1. 本地编译
```bash
# 在本地开发机器上编译Linux版本
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o sshcollector-linux ./cmd/server
```

#### 2. 传输文件
```bash
# 将编译好的二进制文件传输到服务器
scp sshcollector-linux huoshan-1:/opt/ssh-collector-pro/sshcollector

# 传输配置文件（如果需要）
scp -r configs huoshan-1:/opt/ssh-collector-pro/
```

#### 3. 启动服务
```bash
# SSH连接到服务器
ssh huoshan-1

# 进入部署目录
cd /opt/ssh-collector-pro

# 创建必要目录
mkdir -p logs data temp

# 设置执行权限
chmod +x sshcollector

# 启动服务（后台运行）
nohup ./sshcollector > logs/app.log 2>&1 &
```

#### 4. 验证部署
```bash
# 检查进程状态
ps aux | grep sshcollector | grep -v grep

# 检查端口监听
netstat -tlnp | grep 8100

# 测试健康检查接口
curl -s http://localhost:18000/api/v1/health
```

### 方式二：Docker容器化部署

#### 1. 准备项目文件
```bash
# 打包项目文件（排除不必要的文件）
tar --exclude='*.log' --exclude='data' --exclude='logs' --exclude='temp' \
    --exclude='sshcollector*' --exclude='.git' -czf sshcollector-deploy.tar.gz .

# 传输到服务器
scp sshcollector-deploy.tar.gz huoshan-1:/tmp/
```

#### 2. 服务器端构建
```bash
# SSH连接到服务器
ssh huoshan-1

# 解压项目文件
cd /opt/ssh-collector-pro
rm -rf *
tar -xzf /tmp/sshcollector-deploy.tar.gz

# 构建Docker镜像（使用国内镜像源）
GOPROXY=https://goproxy.cn,direct docker build -f deploy/Dockerfile -t sshcollector:latest .
```

#### 3. 运行容器
```bash
# 运行容器
docker run -d \
  --name sshcollector \
  -p 8100:8080 \
  -v /opt/ssh-collector-pro/data:/app/data \
  -v /opt/ssh-collector-pro/logs:/app/logs \
  -v /opt/ssh-collector-pro/configs:/app/configs \
  sshcollector:latest
```

## 关键问题及解决方案

### 1. CGO依赖问题

**问题**: 使用`go-sqlite3`驱动时遇到CGO依赖问题
```
fatal error: 'sqlite3.h' file not found
```

**解决方案**: 
- 添加纯Go SQLite驱动: `modernc.org/sqlite`
- 修改数据库初始化代码，显式指定驱动
- 使用`CGO_ENABLED=0`编译

```go
// internal/database/sqlite.go
db, err := gorm.Open(sqlite.Dialector{
    DriverName: "sqlite",
    DSN:        cfg.Path,
}, gormConfig)
```

### 2. SSH兼容性问题

**问题**: 连接华为设备时出现主机密钥类型不匹配
```
no matching host key type found. Their offer: ssh-rsa
```

**解决方案**: 在SSH客户端配置中添加对旧算法的支持

```go
// pkg/ssh/client.go
Config: ssh.Config{
    KeyExchanges: []string{
        "diffie-hellman-group14-sha256",
        "diffie-hellman-group14-sha1",
        "diffie-hellman-group1-sha1",
        // ... 更多算法
    },
    Ciphers: []string{
        "aes128-ctr", "aes192-ctr", "aes256-ctr",
        "aes128-cbc", "3des-cbc", "aes192-cbc", "aes256-cbc",
    },
    MACs: []string{
        "hmac-sha2-256", "hmac-sha1", "hmac-sha1-96",
    },
},
```

### 3. 网络连接问题

**问题**: Docker构建时网络超时
```
dial tcp 142.250.217.81:443: i/o timeout
```

**解决方案**: 使用国内镜像源
```bash
GOPROXY=https://goproxy.cn,direct docker build -f deploy/Dockerfile -t sshcollector:latest .
```

## 服务管理

### 启动服务
```bash
# 直接启动
./sshcollector

# 后台启动
nohup ./sshcollector > logs/app.log 2>&1 &

# Docker启动
docker start sshcollector
```

### 停止服务
```bash
# 停止进程
pkill -f sshcollector

# 停止Docker容器
docker stop sshcollector
```

### 重启服务
```bash
# 停止并重新启动
pkill -f sshcollector
nohup ./sshcollector > logs/app.log 2>&1 &
```

### 查看日志
```bash
# 查看应用日志
tail -f logs/app.log

# 查看Docker日志
docker logs -f sshcollector
```

## 测试验证

### 1. 健康检查
```bash
# 本地测试
curl -s http://localhost:18000/api/v1/health

# 外网测试
curl -s http://115.190.80.219:18000/api/v1/health
```

### 2. 设备采集测试
```bash
# 测试华为设备采集
curl -X POST http://localhost:18000/api/v1/collector/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "test-001",
    "device_ip": "139.196.196.96",
    "port": 21202,
    "username": "eccom123",
    "password": "Eccom@12345",
    "commands": ["display current"],
    "timeout": 30
  }'
```

### 3. 外网API测试
```bash
# 从本地调用外网IP
curl -X POST http://115.190.80.219:18000/api/v1/collector/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "external-test-001",
    "device_ip": "139.196.196.96",
    "port": 21202,
    "username": "eccom123",
    "password": "Eccom@12345",
    "commands": ["display current"],
    "timeout": 30
  }'
```

## 性能优化

### 1. 连接池配置
```yaml
# configs/config.yaml
ssh:
  timeout: 30
  keep_alive_interval: 300
  max_sessions: 10
```

### 2. 数据库优化
```yaml
database:
  sqlite:
    path: "data/collector.db"
    max_open_conns: 25
    max_idle_conns: 5
```

### 3. 系统资源监控
```bash
# 查看进程资源使用
ps aux | grep sshcollector

# 查看端口状态
netstat -tlnp | grep 18000

# 查看系统负载
top -p $(pgrep sshcollector)
```

## 安全配置

### 1. 防火墙设置
```bash
# 开放18000端口
sudo ufw allow 18000/tcp

# 查看防火墙状态
sudo ufw status
```

### 2. 服务用户
```bash
# 创建专用用户（可选）
sudo useradd -r -s /bin/false sshcollector
sudo chown -R sshcollector:sshcollector /opt/ssh-collector-pro
```

### 3. 文件权限
```bash
# 设置适当的文件权限
chmod 755 /opt/ssh-collector-pro/sshcollector
chmod 644 /opt/ssh-collector-pro/configs/*
chmod 755 /opt/ssh-collector-pro/logs
chmod 755 /opt/ssh-collector-pro/data
```

## 故障排查

### 1. 服务无法启动
```bash
# 检查配置文件
cat configs/config.yaml

# 查看错误日志
tail -f logs/app.log

# 检查端口占用
netstat -tlnp | grep 18000
```

### 2. SSH连接失败
```bash
# 测试网络连通性
telnet target_host 22

# 手动SSH测试
ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=no username@target_host
```

### 3. 性能问题
```bash
# 查看系统资源
htop

# 查看网络连接
ss -tuln | grep 18000

# 查看磁盘使用
df -h
```

## 维护建议

### 1. 定期备份
```bash
# 备份数据库
cp data/collector.db data/collector.db.backup.$(date +%Y%m%d)

# 备份配置文件
tar -czf configs.backup.$(date +%Y%m%d).tar.gz configs/
```

### 2. 日志轮转
```bash
# 设置logrotate
sudo vim /etc/logrotate.d/sshcollector
```

### 3. 监控告警
- 设置进程监控
- 配置端口检查
- 监控磁盘空间
- 设置日志告警

## 版本更新

### 1. 更新步骤
```bash
# 1. 停止服务
pkill -f sshcollector

# 2. 备份当前版本
cp sshcollector sshcollector.backup

# 3. 部署新版本
scp new-sshcollector huoshan-1:/opt/ssh-collector-pro/sshcollector

# 4. 启动服务
nohup ./sshcollector > logs/app.log 2>&1 &

# 5. 验证服务
curl -s http://localhost:8100/api/v1/health
```

### 2. 回滚方案
```bash
# 如果新版本有问题，快速回滚
pkill -f sshcollector
cp sshcollector.backup sshcollector
nohup ./sshcollector > logs/app.log 2>&1 &
```

---

## Docker部署记录

### 最新部署 (2025-09-24)

**部署服务器**: huoshan-1 (115.190.80.219)  
**部署路径**: /opt/ssh-collector-pro  
**Docker镜像**: ssh-collector-pro:latest  
**容器名称**: ssh-collector-pro  
**端口映射**: 18000:18000  

#### 部署步骤
1. **构建Docker镜像**
   ```bash
   # 在huoshan-1服务器上构建
   cd /opt/ssh-collector-pro
   docker build -f deploy/Dockerfile -t ssh-collector-pro .
   ```

2. **启动容器**
   ```bash
  docker run -d --name ssh-collector-pro \
    -p 18000:18000 \
     -v $(pwd)/data:/app/data \
     -v $(pwd)/logs:/app/logs \
     -v $(pwd)/configs:/app/configs \
     ssh-collector-pro
   ```

3. **验证部署**
   ```bash
   # 健康检查
curl -f http://115.190.80.219:18000/api/v1/health
   
   # 设备采集测试
curl -X POST http://115.190.80.219:18000/api/v1/collector/execute \
     -H "Content-Type: application/json" \
     -d '{
       "task_id": "test-001",
       "device_ip": "139.196.196.96",
       "port": 21202,
       "username": "eccom123",
       "password": "Eccom@12345",
       "commands": ["display current"],
       "timeout": 30
     }'
   ```

#### 部署问题解决
1. **Go代理超时**: 在Dockerfile中添加了国内代理设置
2. **gcc编译器缺失**: 在构建镜像时安装了gcc和musl-dev
3. **端口映射更新**: 统一为容器内外端口(18000:18000)
4. **权限问题**: 设置了正确的文件权限(1001:1001)

#### 容器管理命令
```bash
# 查看容器状态
docker ps | grep ssh-collector-pro

# 查看容器日志
docker logs ssh-collector-pro

# 停止容器
docker stop ssh-collector-pro

# 重启容器
docker restart ssh-collector-pro

# 删除容器
docker rm ssh-collector-pro
```

---

**部署完成时间**: 2025-09-24 16:55  
**部署版本**: v1.0.0  
**部署方式**: Docker容器  
**部署状态**: ✅ 成功  
**测试状态**: ✅ 通过  
**外网访问**: ✅ 正常  

## 输出过滤规则配置（可选）

为避免设备分页提示污染原始输出（例如 `--More--`、`---- More ----`、`more`），系统提供可配置的行级过滤规则。通过在配置文件的 `collector.output_filter` 中定义两类匹配规则即可：

- 前缀匹配：当某行以指定前缀开始时移除该行。
- 包含匹配：当某行包含指定子串时移除该行。

示例（`configs/config.yaml`/`dev.yaml`/`prod.yaml`）：

```yaml
collector:
  # ... 其他配置
  output_filter:
    # 前缀匹配：行以这些前缀开始则移除
    prefixes:
      - "---- More ----"
      - "more"
    # 包含匹配：行包含这些子串则移除
    contains:
      - "--more--"
    # 匹配选项
    case_insensitive: true   # 忽略大小写（建议开启）
    trim_space: true         # 匹配前先去除首尾空格（建议开启）
```

说明：
- 该过滤在采集服务层统一生效，对所有平台与命令的原始输出按行处理。
- 可以根据实际设备的分页提示样式增删条目，支持多前缀与多子串。
- 修改配置后需重启服务使其生效。

重启示例：

```bash
# 直接部署
pkill -f sshcollector || true
nohup ./sshcollector > logs/app.log 2>&1 &

# Docker 部署
docker restart sshcollector-pro
```

## 交互配置（自动交互与错误提示）

将交互式会话的自动响应与命令错误提示统一配置到 `collector.interact`，在不同环境的 `configs/*.yaml` 中按需调整。

- 自动执行交互参数对：当设备输出包含指定字符串（大小写可不敏感）时，自动发送对应命令（如确认提示）。
- 命令错误提示：当设备回显以列表中的字符串开头时，标记命令可能错误，并在结果的 `error` 字段给出提示。

示例（`configs/config.yaml`/`dev.yaml`/`prod.yaml`）：

```yaml
collector:
  interact:
    auto_interactions:
      - except_output: "do you want to save this config? yes/no"
        command_auto_send: "yes"
      - except_output: "do you want to reload this device? yes/no"
        command_auto_send: "no"
    error_hints:
      - "ERROR:"
      - "invalid parameters detect"
    case_insensitive: true
    trim_space: true
```

行为说明：
- `auto_interactions`：如配置存在，则覆盖平台插件默认的自动交互规则；为空时使用插件默认值。
- `error_hints`：逐行匹配设备回显，满足“开头匹配”则将该行作为命令错误提示写入结果的 `error` 字段（不更改系统返回的 `exit_code`）。
- `case_insensitive`/`trim_space`：同时作用于两类匹配，提升鲁棒性。

应用配置后，请重启服务：

```bash
pkill -f sshcollector || true
nohup ./sshcollector > logs/app.log 2>&1 &

# 或 Docker 部署
docker restart sshcollector-pro
```

## 联系信息

如有问题，请联系运维团队或查看项目文档。
### 4. 并发档位（推荐）
在采集器启动时从配置读取并发档位，根据宿主机规格自动设置安全的并发度。档位优先于 `collector.concurrent` 数值。

```yaml
# configs/config.yaml
collector:
  # 档位：S/M/L/XL（也支持 "Concurrency-S" 形式）
  concurrency_profile: "S"
  # 档位与并发数映射（可按需调整）
  concurrency_profiles:
    S: 8    # 2c4g
    M: 16   # 4c8g
    L: 32   # 8c16g
    XL: 64  # 16c32g
  # 兼容老配置：若未设置档位，则使用 numeric 并发数
  concurrent: 5
```

说明：
- 档位会覆盖并发数，统一影响内部 `workers` 队列和 SSH 连接池的 `max_active`。
- 服务器日志会输出当前应用的档位与并发度，便于运维核验。
- 如需更保守或更激进的并发，可直接修改 `concurrency_profiles` 对应数值。