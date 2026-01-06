# nova-go SSH 客户端并发隐患与 Panic 分析报告

## 1. 问题现象
系统运行过程中，SSH 客户端模块出现 panic，导致程序崩溃。堆栈信息如下：

```text
goroutine 74 [running]:                                                  
golang.org/x/crypto/ssh.(*Client).NewSession(0x1dcd6500?)                
        /Users/wangfuyu/go/pkg/mod/golang.org/x/crypto@v0.42.0/ssh/client.go:135 +0x18                                                            
github.com/sshcollectorpro/sshcollectorpro/pkg/ssh.(*Client).newSessionWithRetry(0x140004da580)                                                   
        /Users/wangfuyu/PythonCode/velo-go/nova-go/pkg/ssh/client.go:268 +0x11c
...
```

报错位置为 `nova-go/pkg/ssh/client.go:268`，调用了 `c.connection.NewSession()`。Panic 的直接原因是空指针解引用（Nil Pointer Dereference），即 `c.connection` 为 `nil`。

## 2. 问题原因

### 2.1 核心逻辑漏洞：重连失败后的空指针使用
在 `newSessionWithRetry` 方法中，针对 `EOF` 或 `disconnect` 错误的重试逻辑存在缺陷：

1.  **主动置空连接**：
    当检测到可重试错误时，代码调用了 `_ = c.Close()`。`Close` 方法内部会将 `c.connection` 显式置为 `nil`。

2.  **重连失败未处理**：
    紧接着调用 `_ = c.Connect(ctx, c.info)` 尝试建立新连接。**关键问题在于代码忽略了 `Connect` 的返回值**。如果重连失败（如超时、认证错误），`Connect` 返回错误且不会重新给 `c.connection` 赋值，此时 `c.connection` 仍为 `nil`。

3.  **循环重入导致 Panic**：
    代码执行 `continue` 跳过当前循环，进入下一次迭代。在下一次迭代开始时（约第 268 行），直接调用了 `c.connection.NewSession()`。由于此时 `c.connection` 是 `nil`，导致了运行时 Panic。

### 2.2 并发安全隐患
虽然此次 Panic 的主因是逻辑错误，但代码中还存在并发安全隐患：
- `newSessionWithRetry` 方法在访问 `c.connection` 时没有加读锁（`RLock`）。
- `Connect` 和 `Close` 方法会加写锁（`Lock`）修改 `c.connection`。
- 在高并发场景下，如果一个 Goroutine 正在执行重连（修改连接状态），另一个 Goroutine 尝试读取 `c.connection`，可能导致数据竞争（Data Race）或读取到中间状态。

## 3. 修复建议

### 3.1 修复空指针 Panic
必须在重连尝试后检查连接是否成功建立。如果重连失败，不应继续尝试使用该连接。

**建议修改方案：**
在重连逻辑中，捕获 `Connect` 的错误。如果重连失败，应记录日志并退出当前循环（或返回错误），而不是 `continue`。

```go
// 伪代码示例
_ = c.Close()
ctx, cancel := context.WithTimeout(context.Background(), c.config.ConnectTimeout)
err := c.Connect(ctx, c.info) // 捕获错误
cancel()

if err != nil {
    logger.Errorf("SSH newSession: reconnect failed: %v", err)
    // 重连失败，无法继续，返回上一次的错误或新的重连错误
    return nil, lastErr 
}

// 重连成功，等待片刻让设备就绪
time.Sleep(200 * time.Millisecond)
logger.Debug("SSH newSession: reconnect success")
continue
```

### 3.2 增强并发安全性
为了保证线程安全，建议在访问共享资源 `c.connection` 时使用读写锁进行保护。

1.  **加读锁**：在 `newSessionWithRetry` 中获取 `c.connection` 之前，应使用 `c.mutex.RLock()`，获取到本地变量后再解锁，或者在整个操作期间持有锁（需注意锁的粒度，避免阻塞耗时的网络操作）。
2.  **原子性检查**：在 `newSessionWithRetry` 的每次循环开始处，都应检查 `c.connection` 是否为 `nil`。

### 3.3 优化重试策略
当前的重试策略混合了“瞬时网络抖动重试”和“连接断开重连”两种场景。建议：
- 明确区分错误类型。对于连接已断开（EOF, Broken Pipe）这类不可恢复的错误，直接触发重连机制。
- 限制重连次数。避免在网络不可达时陷入死循环或过多的无效重连尝试。
