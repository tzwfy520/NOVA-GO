package interact

import "sync"

// 注册中心，按平台名称获取交互插件
var (
    registryMu sync.RWMutex
    registry   = map[string]InteractPlugin{
        "default": &DefaultPlugin{},
    }
)

// 常用平台别名映射，便于宽松匹配
// 例如：device_platform=“h3c” 映射到交换机插件 h3c_s
var aliases = map[string]string{
    "h3c":        "h3c_s",
    "h3c_s":      "h3c_s",
    "h3c_sr":     "h3c_sr",
    "h3c_msr":    "h3c_msr",
    "huawei":     "huawei_s",
    "huawei_s":   "huawei_s",
    "huawei_ce":  "huawei_ce",
    "cisco":      "cisco_ios",
    "cisco_ios":  "cisco_ios",
    "default":    "default",
}

// Register 注册一个交互插件
func Register(name string, plugin InteractPlugin) {
    registryMu.Lock()
    defer registryMu.Unlock()
    registry[name] = plugin
}

// Get 获取指定平台的交互插件，不存在则返回 default
func Get(name string) InteractPlugin {
    registryMu.RLock()
    defer registryMu.RUnlock()
    // 规范化名称
    n := name
    if n == "" {
        n = "default"
    }
    // 先直接命中注册表
    if p, ok := registry[n]; ok {
        return p
    }
    // 再尝试别名映射
    if alias, ok := aliases[n]; ok {
        if p, ok := registry[alias]; ok {
            return p
        }
    }
    // 最后回退 default
    return registry["default"]
}