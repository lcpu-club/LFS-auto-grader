package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/lcpu-club/hpcgame-judger/internal/executor"
	"github.com/redis/go-redis/v9"
)

// RunningConfig Docker 评测配置
type RunningConfig struct {
	Image       string            `json:"image"`       // Docker 镜像
	Command     []string          `json:"command"`     // 执行命令
	Timeout     int64             `json:"timeout"`     // 超时时间（秒）
	MemoryLimit int64             `json:"memoryLimit"` // 内存限制（MB）
	CPULimit    float64           `json:"cpuLimit"`    // CPU 限制（核心数）
	Env         map[string]string `json:"env"`         // 额外环境变量
	Variables   map[string]any    `json:"variables"`   // 模板变量
}

func (s *JudgeSession) run() error {
	defer s.runningCleanup()

	log.Printf("Starting evaluation for solution %s, task %s", s.soln.SolutionId, s.soln.TaskId)

	// 构建执行配置
	execConfig := s.buildExecuteConfig()

	// 使用 Docker 执行器运行
	return s.executeWithDocker(execConfig)
}

func (s *JudgeSession) buildExecuteConfig() *executor.ExecuteConfig {
	config := &executor.ExecuteConfig{
		Image:       s.rc.Image,
		Command:     s.rc.Command,
		Timeout:     s.rc.Timeout,
		MemoryLimit: s.rc.MemoryLimit,
		CPULimit:    s.rc.CPULimit,
		Env:         make(map[string]string),
		WorkDir:     "/work",
	}

	// 默认值
	if config.Timeout == 0 {
		config.Timeout = 300 // 默认 5 分钟
	}
	if config.MemoryLimit == 0 {
		config.MemoryLimit = 512 // 默认 512MB
	}

	// 复制用户环境变量
	for k, v := range s.rc.Env {
		config.Env[k] = v
	}

	// 注入评测相关环境变量
	config.Env["SOLUTION_ID"] = s.soln.SolutionId
	config.Env["TASK_ID"] = s.soln.TaskId
	config.Env["USER_ID"] = s.soln.UserId
	config.Env["SOLUTION_DATA_URL"] = s.soln.SolutionDataUrl
	config.Env["SOLUTION_DATA_HASH"] = s.soln.SolutionDataHash
	config.Env["PROBLEM_DATA_URL"] = s.soln.ProblemDataUrl
	config.Env["PROBLEM_DATA_HASH"] = s.soln.ProblemDataHash

	// 如果有变量，序列化为 JSON
	if s.rc.Variables != nil {
		varsJSON, err := json.Marshal(s.rc.Variables)
		if err == nil {
			config.Env["JUDGE_VARIABLES"] = string(varsJSON)
		}
	}

	// 添加数据目录挂载
	if *s.m.conf.SharedVolumePath != "" {
		config.Mounts = append(config.Mounts, executor.Mount{
			Source:   *s.m.conf.SharedVolumePath,
			Target:   "/data",
			ReadOnly: true,
		})
	}

	return config
}

func (s *JudgeSession) executeWithDocker(config *executor.ExecuteConfig) error {
	ctx := context.Background()

	// 创建执行上下文（带超时）
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(config.Timeout)*time.Second)
	defer cancel()

	// 执行评测，并实时处理日志
	result, err := s.m.exec.ExecuteWithLogs(execCtx, config, func(line string) error {
		// 处理每一行日志
		log.Printf("[%s] Log: %s", s.soln.SolutionId, line)

		// 尝试解析为评测消息
		if err := s.processMessage(line); err != nil {
			log.Printf("Failed to process message: %v", err)
			// 不中断执行
		}

		// 更新处理时间戳
		now := time.Now()
		s.updateProcessedTimestamp(&now)

		return nil
	})

	if err != nil {
		return fmt.Errorf("docker execution failed: %w", err)
	}

	// 处理执行结果
	if result.TimedOut {
		log.Printf("Solution %s timed out", s.soln.SolutionId)
	}
	if result.OOM {
		log.Printf("Solution %s ran out of memory", s.soln.SolutionId)
	}

	log.Printf("Solution %s finished with exit code %d", s.soln.SolutionId, result.ExitCode)

	// 确保完成评测
	s.aoi.Complete(context.TODO())

	return nil
}

func (s *JudgeSession) runningCleanup() {
	err := s.deleteProcessedTimestamp()
	if err != nil {
		log.Println("Failed to delete processed timestamp:", err)
	}
}

// Redis 时间戳相关方法

func (s *JudgeSession) processedTimestampKey() string {
	return fmt.Sprintf("judge:processed:%s", s.id)
}

func (s *JudgeSession) updateProcessedTimestamp(t *time.Time) error {
	return s.m.r.Client.Set(context.TODO(), s.processedTimestampKey(), t.Format(time.RFC3339), 0).Err()
}

func (s *JudgeSession) getProcessedTimestamp() (*time.Time, error) {
	t, err := s.m.r.Client.Get(context.TODO(), s.processedTimestampKey()).Result()

	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	parsed, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

func (s *JudgeSession) deleteProcessedTimestamp() error {
	return s.m.r.Client.Del(context.TODO(), s.processedTimestampKey()).Err()
}
