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
	config      *Config
	connections map[string]*pooledConnection
	mutex       sync.RWMutex
	maxIdle     int
	maxActive   int
	idleTimeout time.Duration
	cleanupInterval time.Duration
}

// pooledConnection 池化的连接
type pooledConnection struct {
	client     *Client
	info       *ConnectionInfo
	lastUsed   time.Time
	inUse      bool
	created    time.Time
}

// PoolConfig 连接池配置
type PoolConfig struct {
	MaxIdle        int           `yaml:"max_idle"`
	MaxActive      int           `yaml:"max_active"`
	IdleTimeout    time.Duration `yaml:"idle_timeout"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
	SSHConfig      *Config       `yaml:"ssh"`
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
func (p *Pool) GetConnection(ctx context.Context, info *ConnectionInfo) (*Client, error) {
    key := p.getConnectionKey(info)

    p.mutex.Lock()
    defer p.mutex.Unlock()

    logger.Debugf("SSH pool: GetConnection start key=%s", key)
    // 查找现有连接
    if conn, exists := p.connections[key]; exists {
        if !conn.inUse && conn.client.IsConnected() {
            conn.inUse = true
            conn.lastUsed = time.Now()
            logger.Debugf("SSH pool: reuse connection key=%s created=%s", key, conn.created.Format(time.RFC3339))
            return conn.client, nil
        }
        // 连接已断开或正在使用，删除
        logger.Debugf("SSH pool: drop stale/busy connection key=%s in_use=%v alive=%v", key, conn.inUse, conn.client.IsConnected())
        delete(p.connections, key)
    }

	// 检查连接数限制
    activeCount := p.getActiveCount()
    if activeCount >= p.maxActive {
        logger.Warnf("SSH pool: full active=%d max_active=%d", activeCount, p.maxActive)
        return nil, fmt.Errorf("connection pool is full, active connections: %d", activeCount)
    }

	// 创建新连接
    client := NewClient(p.config)
    if err := client.Connect(ctx, info); err != nil {
        logger.Error("SSH pool: connect failed", "key", key, "error", err)
        return nil, fmt.Errorf("failed to create SSH connection: %w", err)
    }

	// 添加到连接池
    p.connections[key] = &pooledConnection{
        client:   client,
        info:     info,
        lastUsed: time.Now(),
        inUse:    true,
        created:  time.Now(),
    }

    logger.Debugf("SSH pool: new connection established key=%s", key)
    return client, nil
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
	client, err := p.GetConnection(ctx, info)
	if err != nil {
		return nil, err
	}
	defer p.ReleaseConnection(info)

	return client.ExecuteCommand(ctx, command)
}

// ExecuteCommands 通过连接池批量执行命令
func (p *Pool) ExecuteCommands(ctx context.Context, info *ConnectionInfo, commands []string) ([]*CommandResult, error) {
	client, err := p.GetConnection(ctx, info)
	if err != nil {
		return nil, err
	}
	defer p.ReleaseConnection(info)

	return client.ExecuteCommands(ctx, commands)
}

// ExecuteInteractiveCommand 通过连接池执行交互式命令
func (p *Pool) ExecuteInteractiveCommand(ctx context.Context, info *ConnectionInfo, command string, responses []string) (*CommandResult, error) {
	client, err := p.GetConnection(ctx, info)
	if err != nil {
		return nil, err
	}
	defer p.ReleaseConnection(info)

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
		"max_idle":          p.maxIdle,
		"max_active":        p.maxActive,
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