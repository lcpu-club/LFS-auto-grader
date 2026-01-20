package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/lcpu-club/lfs-auto-grader/internal/config"
	"github.com/lcpu-club/lfs-auto-grader/internal/executor"
	"github.com/lcpu-club/lfs-auto-grader/pkg/aoiclient"
	"github.com/lcpu-club/lfs-auto-grader/pkg/judgerproto"
)

const pollInterval = 250 * time.Millisecond

type RunningConfig struct {
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Timeout     int64             `json:"timeout"`
	MemoryLimit int64             `json:"memoryLimit"`
	CPULimit    float64           `json:"cpuLimit"`
	Env         map[string]string `json:"env"`
	Variables   map[string]any    `json:"variables"`
}

type Manager struct {
	conf *config.ManagerConfig
	aoi  *aoiclient.Client
	exec *executor.DockerExecutor
}

func NewManager(conf *config.ManagerConfig) *Manager {
	return &Manager{conf: conf}
}

func (m *Manager) Init() error {
	exec, err := executor.NewDockerExecutor()
	if err != nil {
		return err
	}
	m.exec = exec

	aoi := aoiclient.New(*m.conf.Endpoint)
	if *m.conf.RunnerID != "" || *m.conf.RunnerKey != "" {
		aoi.Authenticate(*m.conf.RunnerID, *m.conf.RunnerKey)
	} else {
		return errors.New("runner ID and key must be provided")
	}
	m.aoi = aoi

	return nil
}

func (m *Manager) Start() error {
	for {
		time.Sleep(pollInterval)

		soln, err := m.aoi.Poll(context.TODO())
		if err != nil {
			log.Println("Failed to poll:", err)
			continue
		}

		if soln.SolutionId == "" || soln.TaskId == "" {
			continue
		}

		log.Println("Received solution", soln.SolutionId, "for task", soln.TaskId)

		err = m.run(soln)
		if err != nil {
			log.Println("Failed to run solution:", err)
			m.failSoln(soln, "Failed to run solution: "+err.Error())
		}
	}
}

func (m *Manager) failSoln(soln *aoiclient.SolutionPoll, reason string) {
	s := m.aoi.Solution(soln.SolutionId, soln.TaskId)
	s.Patch(context.TODO(), &aoiclient.SolutionInfo{
		Score:   0,
		Status:  aoiclient.StatusError,
		Message: reason,
	})
	s.SaveDetails(context.TODO(), &aoiclient.SolutionDetails{Summary: reason})
	s.Complete(context.TODO())
}

func (m *Manager) run(soln *aoiclient.SolutionPoll) error {
	log.Printf("Starting evaluation for solution %s, task %s", soln.SolutionId, soln.TaskId)

	rc := new(RunningConfig)
	if err := json.Unmarshal(soln.ProblemConfig.Judge.Config, rc); err != nil {
		return err
	}

	aoi := m.aoi.Solution(soln.SolutionId, soln.TaskId)
	execConfig := m.buildExecuteConfig(soln, rc)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(execConfig.Timeout)*time.Second)
	defer cancel()

	result, err := m.exec.ExecuteWithLogs(ctx, execConfig, func(line string) error {
		log.Printf("[%s] %s", soln.SolutionId, line)
		m.processMessage(line, aoi)
		return nil
	})

	if err != nil {
		return fmt.Errorf("docker execution failed: %w", err)
	}

	if result.TimedOut {
		log.Printf("Solution %s timed out", soln.SolutionId)
	}
	if result.OOM {
		log.Printf("Solution %s ran out of memory", soln.SolutionId)
	}

	log.Printf("Solution %s finished with exit code %d", soln.SolutionId, result.ExitCode)
	aoi.Complete(context.TODO())

	return nil
}

func (m *Manager) buildExecuteConfig(soln *aoiclient.SolutionPoll, rc *RunningConfig) *executor.ExecuteConfig {
	config := &executor.ExecuteConfig{
		Image:       rc.Image,
		Command:     rc.Command,
		Timeout:     rc.Timeout,
		MemoryLimit: rc.MemoryLimit,
		CPULimit:    rc.CPULimit,
		Env:         make(map[string]string),
		WorkDir:     "/work",
	}

	if config.Timeout == 0 {
		config.Timeout = 300
	}
	if config.MemoryLimit == 0 {
		config.MemoryLimit = 512
	}

	for k, v := range rc.Env {
		config.Env[k] = v
	}

	config.Env["SOLUTION_ID"] = soln.SolutionId
	config.Env["TASK_ID"] = soln.TaskId
	config.Env["USER_ID"] = soln.UserId
	config.Env["SOLUTION_DATA_URL"] = soln.SolutionDataUrl
	config.Env["SOLUTION_DATA_HASH"] = soln.SolutionDataHash
	config.Env["PROBLEM_DATA_URL"] = soln.ProblemDataUrl
	config.Env["PROBLEM_DATA_HASH"] = soln.ProblemDataHash

	if rc.Variables != nil {
		if varsJSON, err := json.Marshal(rc.Variables); err == nil {
			config.Env["JUDGE_VARIABLES"] = string(varsJSON)
		}
	}

	if *m.conf.SharedVolumePath != "" {
		config.Mounts = append(config.Mounts, executor.Mount{
			Source:   *m.conf.SharedVolumePath,
			Target:   "/data",
			ReadOnly: true,
		})
	}

	return config
}

func (m *Manager) Close() error {
	if m.exec != nil {
		return m.exec.Close()
	}
	return nil
}

func (m *Manager) processMessage(msg string, aoi *aoiclient.SolutionClient) {
	parsed, err := judgerproto.MessageFromString(msg)
	if err != nil {
		return
	}

	switch parsed.Action {
	case judgerproto.ActionComplete:
		aoi.Complete(context.TODO())
	case judgerproto.ActionPatch:
		var body judgerproto.PatchBody
		if json.Unmarshal(parsed.Body, &body) == nil {
			aoi.Patch(context.TODO(), (*aoiclient.SolutionInfo)(&body))
		}
	case judgerproto.ActionDetail:
		var body judgerproto.DetailBody
		if json.Unmarshal(parsed.Body, &body) == nil {
			aoi.SaveDetails(context.TODO(), (*aoiclient.SolutionDetails)(&body))
		}
	}
}
