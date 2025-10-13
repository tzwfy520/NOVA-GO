package interact

import "sync"

// 注册中心，按平台名称获取交互插件
var (
    registryMu sync.RWMutex
    registry   = map[string]InteractPlugin{
        "default": &DefaultPlugin{},
    }
)

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
    if p, ok := registry[name]; ok {
        return p
    }
    return registry["default"]
}