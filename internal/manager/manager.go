package manager

import (
	"errors"
	"log"
	"os"

	"github.com/lcpu-club/hpcgame-judger/internal/config"
	"github.com/lcpu-club/hpcgame-judger/internal/executor"
	"github.com/lcpu-club/hpcgame-judger/internal/utils"
	"github.com/lcpu-club/hpcgame-judger/pkg/aoiclient"
)

type Manager struct {
	conf *config.ManagerConfig
	aoi  *aoiclient.Client
	r    *Redis
	rl   *RateLimiter
	exec *executor.DockerExecutor

	managerID string
}

func NewManager(conf *config.ManagerConfig) *Manager {
	return &Manager{
		conf: conf,
	}
}

func (m *Manager) genID() {
	m.managerID = ""

	// First acquire hostname
	hostname, err := os.Hostname()
	if err == nil {
		m.managerID = hostname + "-"
	}

	// Then use a random string
	m.managerID += utils.GenerateRandomString(6, "")

	log.Println("Using manager ID:", m.managerID)
}

func (m *Manager) Init() error {
	// 初始化 Docker 执行器
	exec, err := executor.NewDockerExecutor()
	if err != nil {
		return err
	}
	m.exec = exec

	// 初始化 AOI 客户端
	aoi := aoiclient.New(*m.conf.Endpoint)
	if *m.conf.RunnerID != "" || *m.conf.RunnerKey != "" {
		aoi.Authenticate(*m.conf.RunnerID, *m.conf.RunnerKey)
	} else {
		return errors.New("runner ID and key must be provided")
	}
	m.aoi = aoi

	// 初始化 Redis
	r, err := NewRedis(*m.conf.RedisConfig)
	if err != nil {
		return err
	}
	m.r = r

	m.genID()

	// 初始化速率限制器
	m.rl = NewRateLimiter(m.r, "ratelimit", "ratelimit:total")
	return m.rl.Init(*m.conf.RateLimit)
}

func (m *Manager) Start() error {
	go m.findNotRunningLoop()
	return m.pollLoop()
}

func (m *Manager) ID() string {
	return m.managerID
}
