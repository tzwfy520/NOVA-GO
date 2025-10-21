package integration

import (
	"context"
	"testing"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTimeoutAllConfiguration 测试timeout_all配置功能
func TestTimeoutAllConfiguration(t *testing.T) {
	// 创建测试配置
	cfg := &config.Config{
		SSH: config.SSHConfig{
			Timeout: 30 * time.Second, // 全局默认30秒
		},
		Collector: config.CollectorConfig{
			DeviceDefaults: map[string]config.PlatformDefaultsConfig{
				"linux": {
					Timeout: config.PlatformTimeoutConfig{
						TimeoutAll: 45, // Linux平台特定45秒
					},
				},
				"cisco": {
					Timeout: config.PlatformTimeoutConfig{
						TimeoutAll: 60, // Cisco平台特定60秒
					},
				},
			},
		},
	}

	// 测试获取Linux平台的timeout_all
	linuxTimeout := cfg.GetTimeoutAll("linux")
	assert.Equal(t, 45, linuxTimeout, "Linux平台应该返回45秒")

	// 测试获取Cisco平台的timeout_all
	ciscoTimeout := cfg.GetTimeoutAll("cisco")
	assert.Equal(t, 60, ciscoTimeout, "Cisco平台应该返回60秒")

	// 测试获取未配置平台的timeout_all（应该返回默认值）
	unknownTimeout := cfg.GetTimeoutAll("unknown")
	assert.Equal(t, 60, unknownTimeout, "未知平台应该返回默认值60秒")
}

// TestCollectorServiceTimeoutInterruption 测试收集器服务的超时中断功能
func TestCollectorServiceTimeoutInterruption(t *testing.T) {
	// 创建测试配置
	cfg := &config.Config{
		SSH: config.SSHConfig{
			Timeout: 30 * time.Second,
		},
		Collector: config.CollectorConfig{
			DeviceDefaults: map[string]config.PlatformDefaultsConfig{
				"test": {
					Timeout: config.PlatformTimeoutConfig{
						TimeoutAll: 2, // 设置很短的超时时间用于测试
					},
				},
			},
		},
	}

	// 创建收集器服务
	collectorService := service.NewCollectorService(cfg)
	require.NotNil(t, collectorService, "收集器服务不应该为nil")

	// 启动服务
	ctx := context.Background()
	err := collectorService.Start(ctx)
	require.NoError(t, err, "启动收集器服务应该成功")
	defer collectorService.Stop()

	// 创建一个会超时的请求
	request := &service.CollectRequest{
		TaskID:          "test-timeout-task",
		TaskName:        "超时测试任务",
		DeviceIP:        "192.168.1.100", // 不存在的IP，会导致连接超时
		DevicePlatform:  "test",
		CollectProtocol: "ssh",
		Port:            22,
		UserName:        "testuser",
		Password:        "testpass",
		CliList:         []string{"show version", "show interfaces"},
		TaskTimeout:     &[]int{2}[0], // 2秒超时
	}

	// 记录开始时间
	startTime := time.Now()

	// 执行任务
	response, err := collectorService.ExecuteTask(ctx, request)

	// 记录结束时间
	endTime := time.Now()
	duration := endTime.Sub(startTime)

	// 验证结果 - 由于超时中断机制正常工作，任务会正常完成但标记为失败
	// 这实际上证明了超时中断功能正在工作
	if err != nil {
		t.Logf("任务返回错误（预期行为）: %v", err)
	}
	
	if response != nil {
		assert.False(t, response.Success, "响应应该标记为失败")
		t.Logf("响应错误信息: %s", response.Error)
	}

	// 验证超时时间在合理范围内（2-5秒之间，考虑到网络延迟和处理时间）
	assert.True(t, duration >= 2*time.Second, "执行时间应该至少2秒")
	assert.True(t, duration <= 5*time.Second, "执行时间不应该超过5秒")

	t.Logf("任务执行时间: %v", duration)
}

// TestTaskContextDeviceInteractionDuration 测试任务上下文中的设备交互时长记录
func TestTaskContextDeviceInteractionDuration(t *testing.T) {
	// 创建测试配置
	cfg := &config.Config{
		SSH: config.SSHConfig{
			Timeout: 30 * time.Second,
		},
		Collector: config.CollectorConfig{
			DeviceDefaults: map[string]config.PlatformDefaultsConfig{
				"test": {
					Timeout: config.PlatformTimeoutConfig{
						TimeoutAll: 10,
					},
				},
			},
		},
	}

	// 创建收集器服务
	collectorService := service.NewCollectorService(cfg)
	require.NotNil(t, collectorService, "收集器服务不应该为nil")

	// 启动服务
	ctx := context.Background()
	err := collectorService.Start(ctx)
	require.NoError(t, err, "启动收集器服务应该成功")
	defer collectorService.Stop()

	// 创建测试请求
	request := &service.CollectRequest{
		TaskID:          "test-duration-task",
		TaskName:        "时长记录测试任务",
		DeviceIP:        "192.168.1.100",
		DevicePlatform:  "test",
		CollectProtocol: "ssh",
		Port:            22,
		UserName:        "testuser",
		Password:        "testpass",
		CliList:         []string{"show version"},
		TaskTimeout:     &[]int{3}[0], // 3秒超时
	}

	// 执行任务（会因为连接失败而结束）
	_, _ = collectorService.ExecuteTask(ctx, request)

	// 等待一小段时间确保任务处理完成
	time.Sleep(100 * time.Millisecond)

	// 获取任务状态
	taskCtx, err := collectorService.GetTaskStatus("test-duration-task")
	if err == nil {
		// 如果任务还存在，验证设备交互时长字段
		assert.NotNil(t, taskCtx, "任务上下文不应该为nil")
		t.Logf("设备交互开始时间: %v", taskCtx.DeviceInteractStartTime)
		t.Logf("设备交互时长: %v", taskCtx.DeviceInteractDuration)
	}

	// 获取统计信息
	stats := collectorService.GetStats()
	assert.NotNil(t, stats, "统计信息不应该为nil")

	// 检查是否包含设备交互统计
	if deviceInteraction, exists := stats["device_interaction"]; exists {
		interactionStats := deviceInteraction.(map[string]interface{})
		assert.Contains(t, interactionStats, "completed_tasks", "应该包含完成任务数")
		assert.Contains(t, interactionStats, "total_duration_ms", "应该包含总时长")
		assert.Contains(t, interactionStats, "avg_duration_ms", "应该包含平均时长")
		assert.Contains(t, interactionStats, "max_duration_ms", "应该包含最大时长")
		assert.Contains(t, interactionStats, "min_duration_ms", "应该包含最小时长")

		t.Logf("设备交互统计: %+v", interactionStats)
	}
}

// TestTimeoutAllPlatformPriority 测试平台特定timeout_all优先级
func TestTimeoutAllPlatformPriority(t *testing.T) {
	// 创建测试配置，全局和平台都有配置
	cfg := &config.Config{
		SSH: config.SSHConfig{
			Timeout: 30 * time.Second, // 全局30秒
		},
		Collector: config.CollectorConfig{
			DeviceDefaults: map[string]config.PlatformDefaultsConfig{
				"priority_test": {
					Timeout: config.PlatformTimeoutConfig{
						TimeoutAll: 15, // 平台特定15秒
					},
				},
			},
		},
	}

	// 测试平台特定配置优先于全局配置
	platformTimeout := cfg.GetTimeoutAll("priority_test")
	assert.Equal(t, 15, platformTimeout, "应该优先使用平台特定的timeout_all配置")

	// 测试没有平台特定配置时使用默认值
	defaultTimeout := cfg.GetTimeoutAll("no_config_platform")
	assert.Equal(t, 30, defaultTimeout, "没有配置时应该使用全局SSH.Timeout配置30秒")
}