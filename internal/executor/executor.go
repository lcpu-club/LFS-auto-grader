package executor

import (
	"context"
	"io"
)

// ExecuteConfig 评测执行配置
type ExecuteConfig struct {
	Image       string            `json:"image"`       // Docker 镜像
	Command     []string          `json:"command"`     // 执行命令
	Timeout     int64             `json:"timeout"`     // 超时时间（秒）
	MemoryLimit int64             `json:"memoryLimit"` // 内存限制（MB）
	CPULimit    float64           `json:"cpuLimit"`    // CPU 限制（核心数）
	Env         map[string]string `json:"env"`         // 环境变量
	WorkDir     string            `json:"workDir"`     // 工作目录
	Mounts      []Mount           `json:"mounts"`      // 挂载配置
}

// Mount 挂载配置
type Mount struct {
	Source   string `json:"source"`   // 宿主机路径
	Target   string `json:"target"`   // 容器内路径
	ReadOnly bool   `json:"readOnly"` // 是否只读
}

// ExecuteResult 执行结果
type ExecuteResult struct {
	ExitCode int    // 退出码
	Stdout   string // 标准输出
	Stderr   string // 标准错误
	TimedOut bool   // 是否超时
	OOM      bool   // 是否内存超限
}

// LogCallback 日志回调函数
type LogCallback func(line string) error

// Executor 执行器接口
type Executor interface {
	// Execute 执行评测任务
	Execute(ctx context.Context, config *ExecuteConfig) (*ExecuteResult, error)

	// ExecuteWithLogs 执行评测任务并实时获取日志
	ExecuteWithLogs(ctx context.Context, config *ExecuteConfig, callback LogCallback) (*ExecuteResult, error)

	// StreamLogs 流式获取容器日志
	StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error)

	// Stop 停止执行中的任务
	Stop(ctx context.Context, containerID string) error

	// Cleanup 清理资源
	Cleanup(ctx context.Context, containerID string) error
}
