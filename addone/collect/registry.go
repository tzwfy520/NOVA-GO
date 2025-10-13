package collect

import "sync"

var (
    registryMu sync.RWMutex
    registry   = map[string]CollectPlugin{
        "default": &DefaultPlugin{},
    }
)

// Register 注册采集插件
func Register(name string, plugin CollectPlugin) {
    registryMu.Lock()
    defer registryMu.Unlock()
    registry[name] = plugin
}

// Get 获取指定平台的采集插件
func Get(name string) CollectPlugin {
    registryMu.RLock()
    defer registryMu.RUnlock()
    if p, ok := registry[name]; ok {
        return p
    }
    return registry["default"]
}