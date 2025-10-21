# 配置文档

本文档描述SSH采集器专业版的配置参数，包括超时设置、平台配置等。

## 配置文件结构

配置文件采用YAML格式，主要包含以下几个部分：

- `server`: 服务器配置
- `ssh`: SSH连接配置
- `collector`: 采集器配置
- `database`: 数据库配置
- `storage`: 存储配置

## 超时配置

### 全局超时配置

在 `ssh` 部分配置全局超时参数：

```yaml
ssh:
  timeout: 30s  # 全局SSH连接和命令执行超时时间
```

### 平台特定超时配置

在 `collector.device_defaults` 部分为不同平台配置特定的超时参数：

```yaml
collector:
  device_defaults:
    linux:
      timeout:
        timeout_all: 45  # Linux平台的总超时时间（秒）
    cisco:
      timeout:
        timeout_all: 60  # Cisco设备的总超时时间（秒）
    huawei:
      timeout:
        timeout_all: 30  # 华为设备的总超时时间（秒）
```

### timeout_all 参数说明

`timeout_all` 是新增的超时配置参数，用于控制设备交互的总超时时间。

#### 参数特性

- **类型**: 整数（秒）
- **作用域**: 控制单个设备的完整交互过程超时
- **优先级**: 平台特定配置 > 全局SSH配置 > 默认值(60秒)

#### 配置优先级

系统按以下优先级选择超时时间：

1. **平台特定配置**: 如果为设备平台配置了 `timeout_all`，优先使用该值
2. **全局SSH配置**: 如果没有平台特定配置，使用 `ssh.timeout` 转换为秒数
3. **系统默认值**: 如果以上都未配置，使用默认的60秒

#### 配置示例

```yaml
# 完整配置示例
ssh:
  timeout: 30s  # 全局超时30秒

collector:
  max_workers: 10
  device_defaults:
    # Linux服务器配置
    linux:
      timeout:
        timeout_all: 45  # Linux设备45秒超时
      username: "admin"
      
    # Cisco设备配置  
    cisco:
      timeout:
        timeout_all: 60  # Cisco设备60秒超时
      username: "cisco"
      enable_password: "enable123"
      
    # 华为设备配置
    huawei:
      timeout:
        timeout_all: 30  # 华为设备30秒超时
      username: "huawei"
```

#### 使用场景

- **快速响应设备**: 设置较短的 `timeout_all`（如20-30秒）
- **慢速设备**: 设置较长的 `timeout_all`（如60-120秒）
- **网络环境差**: 适当增加超时时间避免误判
- **批量操作**: 根据命令复杂度调整超时时间

#### 超时行为

当达到 `timeout_all` 设定的时间后：

1. 系统会强制中断当前的SSH连接
2. 任务状态标记为失败
3. 返回超时错误信息：`system interrupt: by timeout_all setting (Xs)`
4. 记录设备交互持续时间到统计信息中

#### 监控和统计

系统会记录以下超时相关的统计信息：

- 设备交互总时长
- 平均交互时长  
- 最大交互时长
- 最小交互时长
- 超时中断次数

可通过 `/api/v1/collector/stats` 接口查询这些统计数据。

## 其他配置参数

### 并发配置

```yaml
collector:
  max_workers: 10  # 最大并发工作线程数
```

### 数据库配置

```yaml
database:
  type: "sqlite"
  sqlite:
    path: "data/collector.db"
```

### 存储配置

```yaml
storage:
  type: "local"
  local:
    path: "data/outputs"
```

## 配置验证

启动时系统会验证配置文件的有效性：

- 检查必需参数是否存在
- 验证数值参数的合理性
- 确保文件路径的可访问性

如果配置有误，系统会输出详细的错误信息并拒绝启动。

## 最佳实践

1. **根据网络环境调整超时**: 网络延迟高的环境适当增加超时时间
2. **区分设备类型**: 不同厂商和型号的设备响应速度不同，建议分别配置
3. **监控超时统计**: 定期查看超时统计，优化配置参数
4. **测试验证**: 新配置上线前先在测试环境验证
5. **文档记录**: 记录配置变更的原因和预期效果