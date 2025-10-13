package interact

// InteractDefaults 定义交互层的默认运行参数
type InteractDefaults struct {
    Timeout    int // 秒
    Retries    int // 重试次数
    Threads    int // 线程数（用于本地并行处理）
    Concurrent int // 并发数（用于远端会话并发）
}

// CommandTransformInput 输入命令与元数据
type CommandTransformInput struct {
    Commands []string
    Metadata map[string]interface{}
}

// CommandTransformOutput 输出转换后的命令
type CommandTransformOutput struct {
    Commands []string
}

// InteractPlugin 交互插件接口
type InteractPlugin interface {
    // Name 插件名称（如：default、cisco_ios、huawei_s）
    Name() string
    // Defaults 返回插件的默认运行参数
    Defaults() InteractDefaults
    // TransformCommands 根据平台特性转换命令序列（如进入特权模式）
    TransformCommands(in CommandTransformInput) CommandTransformOutput
}

// DefaultPlugin 系统默认交互插件
type DefaultPlugin struct{}

func (p *DefaultPlugin) Name() string { return "default" }

func (p *DefaultPlugin) Defaults() InteractDefaults {
    return InteractDefaults{
        Timeout:    30,
        Retries:    1,
        Threads:    4,
        Concurrent: 5,
    }
}

func (p *DefaultPlugin) TransformCommands(in CommandTransformInput) CommandTransformOutput {
    // 默认不做任何转换
    return CommandTransformOutput{Commands: append([]string{}, in.Commands...)}
}