package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerExecutor Docker 执行器
type DockerExecutor struct {
	client *client.Client
}

// NewDockerExecutor 创建 Docker 执行器
func NewDockerExecutor() (*DockerExecutor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &DockerExecutor{
		client: cli,
	}, nil
}

// Execute 执行评测任务
func (e *DockerExecutor) Execute(ctx context.Context, config *ExecuteConfig) (*ExecuteResult, error) {
	return e.ExecuteWithLogs(ctx, config, nil)
}

// ExecuteWithLogs 执行评测任务并实时获取日志
func (e *DockerExecutor) ExecuteWithLogs(ctx context.Context, config *ExecuteConfig, callback LogCallback) (*ExecuteResult, error) {
	// 创建容器配置
	containerConfig := &container.Config{
		Image:      config.Image,
		Cmd:        config.Command,
		WorkingDir: config.WorkDir,
		Env:        e.buildEnvList(config.Env),
	}

	// 创建宿主机配置
	hostConfig := &container.HostConfig{
		Resources: container.Resources{},
		Mounts:    e.buildMounts(config.Mounts),
	}

	// 设置资源限制
	if config.MemoryLimit > 0 {
		hostConfig.Resources.Memory = config.MemoryLimit * 1024 * 1024 // 转换为字节
		hostConfig.Resources.MemorySwap = hostConfig.Resources.Memory  // 禁用 swap
	}
	if config.CPULimit > 0 {
		hostConfig.Resources.NanoCPUs = int64(config.CPULimit * 1e9)
	}

	// 创建容器
	resp, err := e.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	containerID := resp.ID

	// 确保清理容器
	defer e.Cleanup(context.Background(), containerID)

	// 启动容器
	if err := e.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	// 设置超时上下文
	var execCtx context.Context
	var cancel context.CancelFunc
	if config.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(config.Timeout)*time.Second)
	} else {
		execCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	// 获取日志
	if callback != nil {
		go e.streamLogsWithCallback(execCtx, containerID, callback)
	}

	// 等待容器结束
	statusCh, errCh := e.client.ContainerWait(execCtx, containerID, container.WaitConditionNotRunning)

	result := &ExecuteResult{}

	select {
	case err := <-errCh:
		if err != nil {
			if execCtx.Err() == context.DeadlineExceeded {
				result.TimedOut = true
				e.Stop(context.Background(), containerID)
			} else {
				return nil, fmt.Errorf("error waiting for container: %w", err)
			}
		}
	case status := <-statusCh:
		result.ExitCode = int(status.StatusCode)
		if status.Error != nil {
			return nil, fmt.Errorf("container error: %s", status.Error.Message)
		}
	}

	// 检查 OOM
	inspect, err := e.client.ContainerInspect(ctx, containerID)
	if err == nil && inspect.State != nil {
		result.OOM = inspect.State.OOMKilled
	}

	// 获取输出
	stdout, stderr, err := e.getLogs(ctx, containerID)
	if err == nil {
		result.Stdout = stdout
		result.Stderr = stderr
	}

	return result, nil
}

// StreamLogs 流式获取容器日志
func (e *DockerExecutor) StreamLogs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	return e.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
}

// Stop 停止容器
func (e *DockerExecutor) Stop(ctx context.Context, containerID string) error {
	timeout := 5
	return e.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// Cleanup 清理容器
func (e *DockerExecutor) Cleanup(ctx context.Context, containerID string) error {
	return e.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
}

// Close 关闭 Docker 客户端
func (e *DockerExecutor) Close() error {
	return e.client.Close()
}

// 辅助方法

func (e *DockerExecutor) buildEnvList(env map[string]string) []string {
	var result []string
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

func (e *DockerExecutor) buildMounts(mounts []Mount) []mount.Mount {
	var result []mount.Mount
	for _, m := range mounts {
		result = append(result, mount.Mount{
			Type:     mount.TypeBind,
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}
	return result
}

func (e *DockerExecutor) getLogs(ctx context.Context, containerID string) (stdout, stderr string, err error) {
	reader, err := e.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
	})
	if err != nil {
		return "", "", err
	}
	defer reader.Close()

	var stdoutBuf, stderrBuf strings.Builder
	_, err = stdcopy.StdCopy(&stdoutBuf, &stderrBuf, reader)
	if err != nil {
		return "", "", err
	}

	return stdoutBuf.String(), stderrBuf.String(), nil
}

func (e *DockerExecutor) streamLogsWithCallback(ctx context.Context, containerID string, callback LogCallback) {
	reader, err := e.StreamLogs(ctx, containerID)
	if err != nil {
		return
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		// Docker 日志格式有 8 字节头，需要跳过
		if len(line) > 8 {
			line = line[8:]
		}
		if err := callback(line); err != nil {
			return
		}
	}
}
