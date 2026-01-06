package ssh

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// Pool SSH连接池
type Pool struct {
	config          *Config
	connections     map[string]*pooledConnection
	mutex           sync.RWMutex
	maxIdle         int
	maxActive       int
	idleTimeout     time.Duration
	cleanupInterval time.Duration
}

// pooledConnection 池化的连接
type pooledConnection struct {
	client   *Client
	info     *ConnectionInfo
	lastUsed time.Time
	inUse    bool
	created  time.Time
}

// PoolConfig 连接池配置
type PoolConfig struct {
	MaxIdle         int           `yaml:"max_idle"`
	MaxActive       int           `yaml:"max_active"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
	SSHConfig       *Config       `yaml:"ssh"`
}

// NewPool 创建SSH连接池
func NewPool(config *PoolConfig) *Pool {
	pool := &Pool{
		config:      config.SSHConfig,
		connections: make(map[string]*pooledConnection),
		maxIdle:     config.MaxIdle,
		maxActive:   config.MaxActive,
		idleTimeout: config.IdleTimeout,
	}
	ci := config.CleanupInterval
	if ci <= 0 {
		ci = 30 * time.Second
	}
	pool.cleanupInterval = ci

	// 启动清理协程
	go pool.cleanup()

	return pool
}

// GetConnection 获取SSH连接
// 返回: 客户端, 是否为临时连接(不归还池), 错误
func (p *Pool) GetConnection(ctx context.Context, info *ConnectionInfo) (*Client, bool, error) {
	key := p.getConnectionKey(info)

	p.mutex.Lock()

	// 1. 尝试复用现有空闲连接
	if conn, exists := p.connections[key]; exists {
		if !conn.inUse && conn.client.IsConnected() {
			conn.inUse = true
			conn.lastUsed = time.Now()
			p.mutex.Unlock()
			logger.Debugf("SSH pool: reuse connection key=%s", key)
			return conn.client, false, nil
		}
		// 若连接已断开，清理之
		if !conn.client.IsConnected() {
			delete(p.connections, key)
			logger.Debugf("SSH pool: remove dead connection key=%s", key)
		}
		// 若连接正在使用(inUse)，则根据策略我们需要创建临时连接
		// 因为 map key 限制了每个 (Host, User) 只能有一个池化连接
	}

	// 2. 检查是否需要创建临时连接（当池满或当前Host已有繁忙连接时）
	// 检查全局连接数限制
	activeCount := p.getActiveCount()
	// 如果池已满，或者该 Host 已经有连接（且繁忙，见上文逻辑），则创建临时连接
	_, keyExists := p.connections[key]

	if keyExists || activeCount >= p.maxActive {
		p.mutex.Unlock()
		logger.Debugf("SSH pool: busy/full (key_exists=%v active=%d max=%d), creating temp connection key=%s", keyExists, activeCount, p.maxActive, key)

		client := NewClient(p.config)
		if err := client.Connect(ctx, info); err != nil {
			return nil, false, fmt.Errorf("failed to create temp connection: %w", err)
		}
		// 返回临时连接标记 true
		return client, true, nil
	}

	// 3. 创建新的池化连接
	// 为避免持有锁进行网络IO，先解锁
	p.mutex.Unlock()

	client := NewClient(p.config)
	if err := client.Connect(ctx, info); err != nil {
		return nil, false, fmt.Errorf("failed to create pooled connection: %w", err)
	}

	// 重新加锁以放入池中
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// 双重检查：在 Connect 期间是否已有其他协程抢占了该 key
	if _, exists := p.connections[key]; exists {
		// 发生竞态：已有连接。
		// 策略：直接将当前新建的连接作为临时连接返回，不替换池中连接
		logger.Debugf("SSH pool: race detected, returning as temp connection key=%s", key)
		return client, true, nil
	}

	// 再次检查全局容量
	if p.getActiveCount() >= p.maxActive {
		logger.Debugf("SSH pool: full during connect, returning as temp key=%s", key)
		return client, true, nil
	}

	// 放入池中
	p.connections[key] = &pooledConnection{
		client:   client,
		info:     info,
		lastUsed: time.Now(),
		inUse:    true,
		created:  time.Now(),
	}
	logger.Debugf("SSH pool: new pooled connection key=%s", key)

	return client, false, nil
}

// ReleaseConnection 释放SSH连接
func (p *Pool) ReleaseConnection(info *ConnectionInfo) {
	key := p.getConnectionKey(info)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if conn, exists := p.connections[key]; exists {
		// 若连接已失效，立即关闭并从池中移除，避免后续复用导致 EOF
		if !conn.client.IsConnected() {
			conn.client.Close()
			delete(p.connections, key)
			logger.Debugf("SSH pool: release and remove dead connection key=%s", key)
			return
		}
		conn.inUse = false
		conn.lastUsed = time.Now()
		logger.Debugf("SSH pool: release connection key=%s", key)
	}
}

// CloseConnection 关闭指定连接
func (p *Pool) CloseConnection(info *ConnectionInfo) error {
	key := p.getConnectionKey(info)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if conn, exists := p.connections[key]; exists {
		err := conn.client.Close()
		delete(p.connections, key)
		return err
	}

	return nil
}

// ExecuteCommand 通过连接池执行命令
func (p *Pool) ExecuteCommand(ctx context.Context, info *ConnectionInfo, command string) (*CommandResult, error) {
	client, isTemp, err := p.GetConnection(ctx, info)
	if err != nil {
		return nil, err
	}
	if isTemp {
		defer client.Close()
	} else {
		defer p.ReleaseConnection(info)
	}

	return client.ExecuteCommand(ctx, command)
}

// ExecuteCommands 通过连接池批量执行命令
func (p *Pool) ExecuteCommands(ctx context.Context, info *ConnectionInfo, commands []string) ([]*CommandResult, error) {
	client, isTemp, err := p.GetConnection(ctx, info)
	if err != nil {
		return nil, err
	}
	if isTemp {
		defer client.Close()
	} else {
		defer p.ReleaseConnection(info)
	}

	return client.ExecuteCommands(ctx, commands)
}

// ExecuteInteractiveCommand 通过连接池执行交互式命令
func (p *Pool) ExecuteInteractiveCommand(ctx context.Context, info *ConnectionInfo, command string, responses []string) (*CommandResult, error) {
	client, isTemp, err := p.GetConnection(ctx, info)
	if err != nil {
		return nil, err
	}
	if isTemp {
		defer client.Close()
	} else {
		defer p.ReleaseConnection(info)
	}

	return client.ExecuteInteractiveCommand(ctx, command, responses)
}

// Close 关闭连接池
func (p *Pool) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var lastErr error
	for key, conn := range p.connections {
		if err := conn.client.Close(); err != nil {
			lastErr = err
		}
		delete(p.connections, key)
	}

	return lastErr
}

// GetStats 获取连接池统计信息
func (p *Pool) GetStats() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_connections":  len(p.connections),
		"active_connections": p.getActiveCount(),
		"idle_connections":   p.getIdleCount(),
		"max_idle":           p.maxIdle,
		"max_active":         p.maxActive,
	}

	return stats
}

// getConnectionKey 生成连接键
func (p *Pool) getConnectionKey(info *ConnectionInfo) string {
	return fmt.Sprintf("%s:%d@%s", info.Host, info.Port, info.Username)
}

// getActiveCount 获取活跃连接数
func (p *Pool) getActiveCount() int {
	count := 0
	for _, conn := range p.connections {
		if conn.inUse {
			count++
		}
	}
	return count
}

// getIdleCount 获取空闲连接数
func (p *Pool) getIdleCount() int {
	count := 0
	for _, conn := range p.connections {
		if !conn.inUse {
			count++
		}
	}
	return count
}

// cleanup 清理过期连接
func (p *Pool) cleanup() {
	// 使用可配置清理周期（默认 30s）
	ticker := time.NewTicker(p.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanupExpiredConnections()
	}
}

// cleanupExpiredConnections 清理过期连接
func (p *Pool) cleanupExpiredConnections() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := time.Now()
	toDelete := make([]string, 0)

	for key, conn := range p.connections {
		// 清理超时的空闲连接
		if !conn.inUse && now.Sub(conn.lastUsed) > p.idleTimeout {
			toDelete = append(toDelete, key)
			continue
		}

		// 清理断开的连接
		if !conn.client.IsConnected() {
			toDelete = append(toDelete, key)
			continue
		}
	}

	// 删除过期连接
	for _, key := range toDelete {
		if conn, exists := p.connections[key]; exists {
			conn.client.Close()
			delete(p.connections, key)
			logger.Debugf("SSH pool: cleanup remove key=%s", key)
		}
	}

	// 如果空闲连接过多，关闭一些
	idleCount := p.getIdleCount()
	if idleCount > p.maxIdle {
		excess := idleCount - p.maxIdle
		for key, conn := range p.connections {
			if excess <= 0 {
				break
			}
			if !conn.inUse {
				conn.client.Close()
				delete(p.connections, key)
				excess--
				logger.Debugf("SSH pool: reduce idle remove key=%s", key)
			}
		}
	}
}

// Health 健康检查
func (p *Pool) Health() error {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	totalConnections := len(p.connections)
	if totalConnections == 0 {
		return nil // 没有连接也是正常的
	}

	connectedCount := 0
	for _, conn := range p.connections {
		if conn.client.IsConnected() {
			connectedCount++
		}
	}

	if connectedCount == 0 && totalConnections > 0 {
		return fmt.Errorf("all connections are disconnected")
	}

	return nil
}
