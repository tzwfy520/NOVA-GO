package integration

import (
	"testing"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/stretchr/testify/assert"
)

// TestTimeoutAllConfigurationUnit 单元测试timeout_all配置功能
func TestTimeoutAllConfigurationUnit(t *testing.T) {
	// 测试用例1：平台特定配置优先
	cfg1 := &config.Config{
		SSH: config.SSHConfig{
			Timeout: 30 * time.Second,
		},
		Collector: config.CollectorConfig{
			DeviceDefaults: map[string]config.PlatformDefaultsConfig{
				"linux": {
					Timeout: config.PlatformTimeoutConfig{
						TimeoutAll: 45,
					},
				},
			},
		},
	}
	
	linuxTimeout := cfg1.GetTimeoutAll("linux")
	assert.Equal(t, 45, linuxTimeout, "Linux平台应该返回平台特定的45秒")
	
	// 测试用例2：全局配置作为后备
	unknownTimeout := cfg1.GetTimeoutAll("unknown_platform")
	assert.Equal(t, 30, unknownTimeout, "未知平台应该使用全局SSH.Timeout配置30秒")
	
	// 测试用例3：默认值
	cfg2 := &config.Config{
		SSH: config.SSHConfig{
			Timeout: 0, // 无全局配置
		},
		Collector: config.CollectorConfig{
			DeviceDefaults: map[string]config.PlatformDefaultsConfig{},
		},
	}
	
	defaultTimeout := cfg2.GetTimeoutAll("any_platform")
	assert.Equal(t, 60, defaultTimeout, "无任何配置时应该使用默认值60秒")
}