package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

var rdb *redis.Client

// InitRedis 初始化Redis连接
func InitRedis(cfg config.RedisConfig) error {
	// 如果Host为空或者为空字符串，表示不使用Redis
	if cfg.Host == "" || strings.TrimSpace(cfg.Host) == "" {
		return nil
	}
	
	rdb = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}

	logger.Info("Redis cache initialized successfully")
	return nil
}

// GetRedis 获取Redis客户端
func GetRedis() *redis.Client {
	return rdb
}

// Close 关闭Redis连接
func Close() error {
	if rdb != nil {
		return rdb.Close()
	}
	return nil
}

// Set 设置缓存
func Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return rdb.Set(ctx, key, data, expiration).Err()
}

// Get 获取缓存
func Get(ctx context.Context, key string, dest interface{}) error {
	data, err := rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("key not found: %s", key)
		}
		return fmt.Errorf("failed to get value: %w", err)
	}

	return json.Unmarshal([]byte(data), dest)
}

// Del 删除缓存
func Del(ctx context.Context, keys ...string) error {
	return rdb.Del(ctx, keys...).Err()
}

// Exists 检查键是否存在
func Exists(ctx context.Context, keys ...string) (int64, error) {
	return rdb.Exists(ctx, keys...).Result()
}

// Expire 设置过期时间
func Expire(ctx context.Context, key string, expiration time.Duration) error {
	return rdb.Expire(ctx, key, expiration).Err()
}

// TTL 获取剩余过期时间
func TTL(ctx context.Context, key string) (time.Duration, error) {
	return rdb.TTL(ctx, key).Result()
}

// HSet 设置哈希字段
func HSet(ctx context.Context, key string, values ...interface{}) error {
	return rdb.HSet(ctx, key, values...).Err()
}

// HGet 获取哈希字段
func HGet(ctx context.Context, key, field string) (string, error) {
	return rdb.HGet(ctx, key, field).Result()
}

// HGetAll 获取所有哈希字段
func HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return rdb.HGetAll(ctx, key).Result()
}

// HDel 删除哈希字段
func HDel(ctx context.Context, key string, fields ...string) error {
	return rdb.HDel(ctx, key, fields...).Err()
}

// LPush 从左侧推入列表
func LPush(ctx context.Context, key string, values ...interface{}) error {
	return rdb.LPush(ctx, key, values...).Err()
}

// RPush 从右侧推入列表
func RPush(ctx context.Context, key string, values ...interface{}) error {
	return rdb.RPush(ctx, key, values...).Err()
}

// LPop 从左侧弹出列表元素
func LPop(ctx context.Context, key string) (string, error) {
	return rdb.LPop(ctx, key).Result()
}

// RPop 从右侧弹出列表元素
func RPop(ctx context.Context, key string) (string, error) {
	return rdb.RPop(ctx, key).Result()
}

// LLen 获取列表长度
func LLen(ctx context.Context, key string) (int64, error) {
	return rdb.LLen(ctx, key).Result()
}

// LRange 获取列表范围内的元素
func LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return rdb.LRange(ctx, key, start, stop).Result()
}

// SAdd 添加集合成员
func SAdd(ctx context.Context, key string, members ...interface{}) error {
	return rdb.SAdd(ctx, key, members...).Err()
}

// SMembers 获取集合所有成员
func SMembers(ctx context.Context, key string) ([]string, error) {
	return rdb.SMembers(ctx, key).Result()
}

// SRem 删除集合成员
func SRem(ctx context.Context, key string, members ...interface{}) error {
	return rdb.SRem(ctx, key, members...).Err()
}

// Health 检查Redis健康状态
func Health(ctx context.Context) error {
	if rdb == nil {
		return fmt.Errorf("redis not initialized")
	}

	_, err := rdb.Ping(ctx).Result()
	return err
}

// GetStats 获取Redis统计信息
func GetStats(ctx context.Context) map[string]interface{} {
	if rdb == nil {
		return nil
	}

	poolStats := rdb.PoolStats()
	return map[string]interface{}{
		"hits":         poolStats.Hits,
		"misses":       poolStats.Misses,
		"timeouts":     poolStats.Timeouts,
		"total_conns":  poolStats.TotalConns,
		"idle_conns":   poolStats.IdleConns,
		"stale_conns":  poolStats.StaleConns,
	}
}