package interact

// InteractDefaults 定义交互层的默认运行参数
type InteractDefaults struct {
    Timeout    int // 秒
    Retries    int // 重试次数
    Threads    int // 线程数（用于本地并行处理）
    Concurrent int // 并发数（用于远端会话并发）
    // 新增默认交互参数
    PromptSuffixes   []string          // 登录成功后的提示符后缀（用于识别命令结束）
    CommandIntervalMS int              // 命令间隔毫秒（串行执行时的节流）
    AutoInteractions []AutoInteraction // 自动交互对（匹配输出后自动发送指令）
    ErrorHints       []string          // 命令错误提示关键词（供上层处理/记录）
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

// AutoInteraction 自动交互对
// 当输出包含 ExpectOutput（大小写不敏感）时，自动发送 AutoSend（通常为空格或确认）
type AutoInteraction struct {
    ExpectOutput string
    AutoSend     string
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
        PromptSuffixes:   []string{"#", ">", "]"},
        CommandIntervalMS: 150,
        AutoInteractions: []AutoInteraction{
            {ExpectOutput: "more", AutoSend: " "},
            {ExpectOutput: "press any key", AutoSend: " "},
            {ExpectOutput: "confirm", AutoSend: "y"},
        },
        ErrorHints: []string{"error", "invalid", "unrecognized", "incomplete", "ambiguous"},
    }
}

func (p *DefaultPlugin) TransformCommands(in CommandTransformInput) CommandTransformOutput {
    // 默认不做任何转换
    return CommandTransformOutput{Commands: append([]string{}, in.Commands...)}
}