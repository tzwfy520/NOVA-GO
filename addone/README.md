# SSH采集器插件系统

本目录包含SSH采集器的扩展插件，用于支持不同厂商设备的专用功能。

## 目录结构

```
addone/
├── collect/          # 采集插件
│   └── huawei.go    # 华为设备采集插件
├── interact/         # 交互插件  
│   └── huawei.go    # 华为设备交互插件
└── README.md        # 本文档
```

## 插件类型

### 1. 采集插件 (collect/)

采集插件负责解析和处理设备输出，提供设备特定的数据解析功能。

**主要功能：**
- 设备信息解析
- 配置文件解析
- 输出格式化
- 数据验证

**华为采集插件功能：**
- 解析设备基本信息（型号、版本、序列号等）
- 解析配置文件结构
- 提供优化的命令集
- 格式化输出结果

### 2. 交互插件 (interact/)

交互插件处理设备特定的交互逻辑，如登录流程、特权模式切换等。

**主要功能：**
- 自动登录
- 特权模式切换
- 交互式命令处理
- 错误检测和处理

**华为交互插件功能：**
- 自动化登录流程
- 系统视图切换
- 配置保存确认
- 提示符识别

## 使用示例

### 华为采集插件使用

```go
package main

import (
    "fmt"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

func main() {
    // 创建华为采集插件
    plugin := collect.NewHuaweiPlugin()
    
    // 获取插件信息
    info := plugin.GetInfo()
    fmt.Printf("插件: %s v%s\n", info["name"], info["version"])
    
    // 解析设备信息
    output := `Product Name: S5700-28C-EI
             Version: V200R005C00SPC500
             Serial Number: 210235A29GH000012345`
    
    deviceInfo, err := plugin.ParseDeviceInfo(output)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("设备型号: %s\n", deviceInfo.Model)
    fmt.Printf("软件版本: %s\n", deviceInfo.Version)
    fmt.Printf("序列号: %s\n", deviceInfo.SerialNo)
    
    // 获取优化命令集
    commands := plugin.GetOptimizedCommands("switch")
    fmt.Printf("推荐命令: %v\n", commands)
}
```

### 华为交互插件使用

```go
package main

import (
    "context"
    "fmt"
    "github.com/sshcollectorpro/sshcollectorpro/addone/interact"
)

func main() {
    // 创建华为交互插件
    interactor := interact.NewHuaweiInteractor()
    
    // 创建登录会话
    loginSession := interactor.CreateLoginSession("switch")
    fmt.Printf("登录步骤数: %d\n", len(loginSession.Steps))
    
    // 创建配置会话
    configCommands := []string{
        "interface GigabitEthernet0/0/1",
        "description Connected to Server",
        "port link-type access",
        "port default vlan 100",
    }
    
    configSession := interactor.CreateConfigSession(configCommands)
    fmt.Printf("配置步骤数: %d\n", len(configSession.Steps))
    
    // 检测错误
    output := "Error: Invalid command"
    hasError, errorMsg := interactor.DetectError(output)
    if hasError {
        fmt.Printf("检测到错误: %s\n", errorMsg)
    }
    
    // 处理提示符
    prompt, found := interactor.HandlePrompt("<Huawei>")
    if found {
        fmt.Printf("识别到提示符: %s\n", prompt)
    }
}
```

## 插件开发规范

### 采集插件接口

采集插件应实现以下方法：

```go
type CollectPlugin interface {
    GetInfo() map[string]interface{}
    ParseDeviceInfo(output string) (*DeviceInfo, error)
    ParseConfiguration(output string) ([]*ConfigSection, error)
    GetOptimizedCommands(deviceType string) []string
    ValidateOutput(command, output string) error
    FormatOutput(command, output string) map[string]interface{}
}
```

### 交互插件接口

交互插件应实现以下方法：

```go
type InteractPlugin interface {
    GetInfo() map[string]interface{}
    CreateLoginSession(deviceType string) *InteractionSession
    CreatePrivilegeSession() *InteractionSession
    CreateConfigSession(commands []string) *InteractionSession
    HandlePrompt(output string) (string, bool)
    DetectError(output string) (bool, string)
    ProcessInteractiveCommand(ctx context.Context, command string, responses []string) ([]*InteractionStep, error)
    ValidateSession(output string) (bool, string)
}
```

## 支持的设备

### 华为设备

**支持的设备类型：**
- 交换机：S5700、S6700、S9700系列
- 路由器：AR系列、NE系列
- 防火墙：USG系列

**支持的命令：**
- `display current-configuration` - 显示当前配置
- `display current` - 显示当前配置（简化版）
- `display version` - 显示版本信息
- `display device` - 显示设备信息
- `display interface brief` - 显示接口摘要
- `display vlan` - 显示VLAN信息
- `display ip routing-table` - 显示路由表

## 扩展开发

### 添加新厂商插件

1. 在 `collect/` 目录下创建厂商插件文件
2. 在 `interact/` 目录下创建对应的交互插件
3. 实现相应的接口方法
4. 添加单元测试
5. 更新文档

### 插件命名规范

- 文件名：`{厂商名}.go`
- 结构体名：`{厂商名}Plugin` / `{厂商名}Interactor`
- 包名：`collect` / `interact`

### 测试规范

每个插件都应包含完整的单元测试：

```go
func TestHuaweiPlugin_ParseDeviceInfo(t *testing.T) {
    plugin := NewHuaweiPlugin()
    
    output := `Product Name: S5700-28C-EI
              Version: V200R005C00SPC500`
    
    info, err := plugin.ParseDeviceInfo(output)
    assert.NoError(t, err)
    assert.Equal(t, "S5700-28C-EI", info.Model)
    assert.Equal(t, "V200R005C00SPC500", info.Version)
}
```

## 注意事项

1. **安全性**：插件不应包含硬编码的密码或敏感信息
2. **性能**：解析大型配置文件时注意内存使用
3. **兼容性**：确保插件与不同版本的设备兼容
4. **错误处理**：提供详细的错误信息和恢复机制
5. **日志记录**：适当添加日志以便调试和监控

## 贡献指南

欢迎贡献新的设备插件！请遵循以下步骤：

1. Fork项目
2. 创建功能分支
3. 实现插件功能
4. 添加测试用例
5. 更新文档
6. 提交Pull Request

## 许可证

本插件系统遵循项目主许可证。