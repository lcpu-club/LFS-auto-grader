# HPCGame Judger 完整技术文档

本文档详细介绍项目的每个文件功能、代码结构以及完整的评测流程。

---

## 目录

1. [项目概述](#1-项目概述)
2. [项目结构总览](#2-项目结构总览)
3. [核心模块详解](#3-核心模块详解)
4. [评测流程详解](#4-评测流程详解)
5. [评测容器开发指南](#5-评测容器开发指南)
6. [部署与运行](#6-部署与运行)
7. [常见问题](#7-常见问题)

---

## 1. 项目概述

**HPCGame Judger** 是一个基于 Docker 的自动评测系统，用于与 **AOI (Azukiiro)** 在线评测平台集成。

### 核心功能
- 从 AOI 平台轮询待评测的提交（Solution）
- 使用 Docker 容器隔离执行评测任务
- 实时获取评测日志并解析评测结果
- 将评测结果回传至 AOI 平台

### 技术栈
| 技术 | 用途 |
|------|------|
| Go 1.23+ | 主要开发语言 |
| Docker | 容器化评测隔离 |
| Redis | 分布式状态管理、速率限制、分布式锁 |
| AOI/Azukiiro | 后端评测平台 API |

---

## 2. 项目结构总览

```
hpcgame-judger/
├── cmd/                          # 可执行程序入口
│   ├── manager/                  # 主服务（评测管理器）
│   │   └── main.go
│   └── utility/                  # 命令行工具
│       ├── main.go
│       ├── register.go
│       └── pull.go
├── internal/                     # 内部包（不对外暴露）
│   ├── config/                   # 配置定义
│   │   └── config.go
│   ├── executor/                 # Docker 执行器
│   │   ├── executor.go           # 执行器接口定义
│   │   └── docker.go             # Docker 实现
│   ├── manager/                  # 核心管理器
│   │   ├── manager.go            # Manager 主结构
│   │   ├── poll.go               # 任务轮询
│   │   ├── protocol.go           # 消息协议处理
│   │   ├── ratelimit.go          # 速率限制
│   │   ├── redis.go              # Redis 操作
│   │   ├── running.go            # 评测执行
│   │   ├── session.go            # 评测会话
│   │   ├── unrun.go              # 未运行任务检测
│   │   └── utils.go              # 工具函数
│   └── utils/                    # 通用工具
│       ├── jwt.go
│       └── secretmanager.go
├── pkg/                          # 公开包（可被外部引用）
│   ├── aoiclient/                # AOI API 客户端
│   │   ├── client.go             # 客户端主类
│   │   ├── errors.go             # 错误处理
│   │   ├── register.go           # 注册 API
│   │   ├── solution.go           # 解答相关 API
│   │   └── status.go             # 状态常量
│   ├── framework/                # 评测框架（预留）
│   └── judgerproto/              # 评测协议
│       └── protocol.go
├── Dockerfile                    # Docker 镜像构建
├── go.mod                        # Go 模块定义
├── justfile                      # 构建脚本
└── README.md                     # 项目说明
```

---

## 3. 核心模块详解

### 3.1 入口程序

#### 📄 `cmd/manager/main.go` - 主服务入口

**功能**：解析命令行参数，初始化并启动 Manager 服务

```go
func main() {
    conf := &config.ManagerConfig{}
    
    // 定义命令行参数
    conf.Listen = flag.String("listen", ":8080", "Listen address")
    conf.RedisConfig = flag.String("redis-config", "redis://localhost:6379", "Redis configuration")
    conf.Endpoint = flag.String("endpoint", "https://hpcgame.pku.edu.cn", "API endpoint")
    conf.RunnerID = flag.String("runner-id", os.Getenv("RUNNER_ID"), "Runner ID")
    conf.RunnerKey = flag.String("runner-key", os.Getenv("RUNNER_KEY"), "Runner Key")
    conf.RateLimit = flag.Int64("rate-limit", 64, "Rate limit")
    
    flag.Parse()
    
    // 创建并启动 Manager
    s := manager.NewManager(conf)
    s.Init()   // 初始化各组件
    s.Start()  // 启动轮询循环
}
```

**配置参数说明**：
| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-listen` | `:8080` | HTTP 服务监听地址 |
| `-redis-config` | `redis://localhost:6379` | Redis 连接字符串 |
| `-endpoint` | `https://hpcgame.pku.edu.cn` | AOI 平台 API 地址 |
| `-runner-id` | 环境变量 `RUNNER_ID` | Runner 身份标识 |
| `-runner-key` | 环境变量 `RUNNER_KEY` | Runner 身份密钥 |
| `-rate-limit` | `64` | 最大并发评测数 |
| `-shared-volume-path` | `/data` | 共享数据目录 |

---

#### 📄 `cmd/utility/main.go` - 命令行工具入口

**功能**：提供命令行工具，用于注册 Runner 和手动轮询

```go
func main() {
    app := cli.NewApp()
    app.Name = "hpcgame-utility"
    
    // 全局参数
    // --endpoint, -e  : API 地址
    // --runner-id, -id : Runner ID
    // --runner-key, -key : Runner Key
    
    registerCommand(app)  // 注册 register 子命令
    pollCommand(app)      // 注册 poll 子命令
    
    app.Run(os.Args)
}
```

---

#### 📄 `cmd/utility/register.go` - 注册命令

**功能**：向 AOI 平台注册新的 Runner

```bash
# 使用示例
./utility register \
  --endpoint "https://hpcgame.pku.edu.cn" \
  --name "my-judger" \
  --label "gpu" --label "cuda" \
  --token "your-registration-token" \
  --write-file "runner.env"
```

**输出**：生成 `runner.env` 文件，包含 `RUNNER_ID` 和 `RUNNER_KEY`

---

#### 📄 `cmd/utility/pull.go` - 轮询命令

**功能**：手动测试轮询功能，获取一个待评测的 Solution

```bash
./utility poll --runner-id "xxx" --runner-key "xxx"
```

---

### 3.2 配置模块

#### 📄 `internal/config/config.go` - 配置结构体

```go
type ManagerConfig struct {
    Listen           *string  // HTTP 监听地址
    Endpoint         *string  // AOI 平台 API 地址
    RunnerID         *string  // Runner 标识
    RunnerKey        *string  // Runner 密钥
    RateLimit        *int64   // 并发限制
    RedisConfig      *string  // Redis 连接字符串
    SharedVolumePath *string  // 共享数据目录
    TLSCertFile      *string  // TLS 证书文件
    TLSKeyFile       *string  // TLS 密钥文件
}
```

**为什么使用指针 (`*string`)**：
- 可以区分"未设置"（nil）和"设置为空"（""）
- 方便从 flag 包获取值

---

### 3.3 Manager 核心模块

#### 📄 `internal/manager/manager.go` - Manager 主结构

**功能**：系统核心，协调各组件工作

```go
type Manager struct {
    conf      *config.ManagerConfig    // 配置
    aoi       *aoiclient.Client        // AOI 客户端
    r         *Redis                   // Redis 连接
    rl        *RateLimiter             // 速率限制器
    exec      *executor.DockerExecutor // Docker 执行器
    managerID string                   // 本实例唯一 ID
}
```

**主要方法**：
| 方法 | 功能 |
|------|------|
| `NewManager(conf)` | 创建 Manager 实例 |
| `Init()` | 初始化 Docker、AOI、Redis、速率限制器 |
| `Start()` | 启动轮询循环和未运行任务检测循环 |
| `genID()` | 生成唯一的 Manager ID（主机名 + 随机串） |

---

#### 📄 `internal/manager/poll.go` - 任务轮询

**功能**：定期从 AOI 平台获取待评测的提交

```go
const pollInterval = 250 * time.Millisecond  // 每 250ms 轮询一次

func (m *Manager) pollLoop() error {
    for {
        time.Sleep(pollInterval)
        
        // 1. 检查速率限制
        ok, err := m.rl.Request()
        if !ok { continue }  // 达到并发上限，跳过
        
        // 2. 从 AOI 获取任务
        polled, err := m.poll()
        
        // 3. 如果没有任务或出错，释放配额
        if err != nil || !polled {
            m.rl.Release()
        }
    }
}

func (m *Manager) poll() (bool, error) {
    // 调用 AOI API 获取待评测的 Solution
    soln, err := m.aoi.Poll(context.TODO())
    
    if soln.SolutionId == "" {
        return false, nil  // 没有待评测的任务
    }
    
    // 存入 Redis 并启动评测
    m.solnAdmission(soln)
    return true, nil
}

func (m *Manager) solnAdmission(soln *aoiclient.SolutionPoll) error {
    // 1. 将 Solution 数据存入 Redis
    id, err := m.r.StoreSolutionPoll(soln)
    
    // 2. 启动 goroutine 执行评测
    go m.run(id)
    return nil
}
```

**轮询流程图**：
```
┌─────────────────┐
│  pollLoop 开始  │
└────────┬────────┘
         ▼
┌─────────────────┐
│  等待 250ms     │
└────────┬────────┘
         ▼
┌─────────────────┐    否
│  速率限制检查   │──────┐
└────────┬────────┘      │
         │ 是           │
         ▼              │
┌─────────────────┐      │
│  调用 AOI Poll  │      │
└────────┬────────┘      │
         ▼              │
┌─────────────────┐    否 │
│  有待评测任务？  │──────┤
└────────┬────────┘      │
         │ 是           │
         ▼              │
┌─────────────────┐      │
│  存入 Redis     │      │
│  启动评测协程   │      │
└────────┬────────┘      │
         │              │
         ◄──────────────┘
         │
         ▼ (循环)
```

---

#### 📄 `internal/manager/session.go` - 评测会话

**功能**：管理单个 Solution 的评测生命周期

```go
type JudgeSession struct {
    id        string                    // 会话 ID (Redis key)
    m         *Manager                  // Manager 引用
    lockKey   string                    // Redis 分布式锁的 key
    closeChan chan struct{}             // 关闭信号通道
    soln      *aoiclient.SolutionPoll   // Solution 数据
    aoi       *aoiclient.SolutionClient // AOI 客户端
    stopped   *atomic.Int32             // 停止标记
    rc        *RunningConfig            // 评测配置
}
```

**生命周期**：
```
NewJudgeSession()    创建会话
       │
       ▼
    init()           初始化：从 Redis 读取数据，解析配置
       │
       ▼
    Run()            开始执行
       │
       ├── tryLock()     获取分布式锁
       │
       ├── lockLoop()    后台刷新锁（防止过期）
       │
       ├── run()         执行实际评测
       │
       └── cleanup()     清理：释放锁、删除 Redis 数据
```

**分布式锁机制**：
- 锁超时时间：6 分钟
- 锁刷新间隔：2 分钟
- 确保同一个 Solution 不会被多个 Manager 同时评测

---

#### 📄 `internal/manager/running.go` - 评测执行

**功能**：构建 Docker 配置并执行评测

```go
type RunningConfig struct {
    Image       string            // Docker 镜像名称
    Command     []string          // 执行命令
    Timeout     int64             // 超时时间（秒）
    MemoryLimit int64             // 内存限制（MB）
    CPULimit    float64           // CPU 限制（核心数）
    Env         map[string]string // 额外环境变量
    Variables   map[string]any    // 自定义变量
}
```

**评测配置构建**：
```go
func (s *JudgeSession) buildExecuteConfig() *executor.ExecuteConfig {
    config := &executor.ExecuteConfig{
        Image:       s.rc.Image,
        Command:     s.rc.Command,
        Timeout:     s.rc.Timeout,      // 默认 300 秒
        MemoryLimit: s.rc.MemoryLimit,  // 默认 512 MB
        CPULimit:    s.rc.CPULimit,
        Env:         make(map[string]string),
        WorkDir:     "/work",
    }
    
    // 注入评测相关环境变量
    config.Env["SOLUTION_ID"] = s.soln.SolutionId
    config.Env["TASK_ID"] = s.soln.TaskId
    config.Env["USER_ID"] = s.soln.UserId
    config.Env["SOLUTION_DATA_URL"] = s.soln.SolutionDataUrl
    config.Env["SOLUTION_DATA_HASH"] = s.soln.SolutionDataHash
    config.Env["PROBLEM_DATA_URL"] = s.soln.ProblemDataUrl
    config.Env["PROBLEM_DATA_HASH"] = s.soln.ProblemDataHash
    config.Env["JUDGE_VARIABLES"] = string(varsJSON)  // 自定义变量 JSON
    
    // 挂载共享数据目录
    config.Mounts = append(config.Mounts, executor.Mount{
        Source:   "/data",
        Target:   "/data",
        ReadOnly: true,
    })
    
    return config
}
```

**评测容器可用的环境变量**：
| 环境变量 | 说明 |
|----------|------|
| `SOLUTION_ID` | 提交 ID |
| `TASK_ID` | 任务 ID |
| `USER_ID` | 用户 ID |
| `SOLUTION_DATA_URL` | 提交数据下载 URL |
| `SOLUTION_DATA_HASH` | 提交数据哈希值 |
| `PROBLEM_DATA_URL` | 题目数据下载 URL |
| `PROBLEM_DATA_HASH` | 题目数据哈希值 |
| `JUDGE_VARIABLES` | 自定义变量（JSON 格式） |

---

#### 📄 `internal/manager/protocol.go` - 消息协议处理

**功能**：解析评测容器输出的 JSON 消息，调用相应的 AOI API

```go
func (s *JudgeSession) processMessage(msg string) error {
    m, err := judgerproto.MessageFromString(msg)
    
    switch m.Action {
    case judgerproto.ActionPatch:      // "p" - 更新分数和状态
        s.aoi.Patch(ctx, &body)
        
    case judgerproto.ActionDetail:     // "d" - 保存详细结果
        s.aoi.SaveDetails(ctx, &body)
        
    case judgerproto.ActionComplete:   // "c" - 完成评测
        s.aoi.Complete(ctx)
        
    case judgerproto.ActionLog:        // "l" - 日志
        log.Println("Log:", body)
        
    case judgerproto.ActionError:      // "e" - 错误
        return errors.New(body)
        
    case judgerproto.ActionGreet:      // "0" - 启动确认
        log.Println("Received greet")
        
    case judgerproto.ActionQuit:       // "q" - 退出
        s.deleteNamespace()
    }
}
```

---

#### 📄 `internal/manager/ratelimit.go` - 速率限制

**功能**：使用 Redis 实现分布式速率限制，控制并发评测数量

```go
type RateLimiter struct {
    r        *Redis
    key      string  // 当前并发数的 key
    totalKey string  // 最大并发数的 key
}

// Request 请求一个评测配额
func (rl *RateLimiter) Request() (bool, error) {
    // Lua 脚本（原子操作）
    // 如果 current < total，则 current++，返回 1
    // 否则返回 0
}

// Release 释放一个评测配额
func (rl *RateLimiter) Release() error {
    // 如果 current > 0，则 current--
}
```

---

#### 📄 `internal/manager/redis.go` - Redis 操作

**功能**：封装 Redis 操作，包括分布式锁、数据存储等

```go
type Redis struct {
    *redis.Client
}

// 分布式锁相关
func (r *Redis) AcquireLock(key, value string, exp time.Duration) (bool, error)
func (r *Redis) RefreshLock(key string, exp time.Duration) error
func (r *Redis) ReleaseLock(key, value string) error
func (r *Redis) IsLocked(key string) (bool, error)

// Solution 数据存储
func (r *Redis) StoreSolutionPoll(soln *SolutionPoll) (id string, err error)
func (r *Redis) GetSolutionPoll(id string) (*SolutionPoll, error)
func (r *Redis) DeleteSolutionPoll(id string) error
func (r *Redis) ListSolutionPoll() ([]string, error)
```

---

#### 📄 `internal/manager/unrun.go` - 未运行任务检测

**功能**：定期检查 Redis 中是否有未被处理的任务（异常恢复）

```go
const findNotRunningInterval = 8 * time.Minute

func (m *Manager) findNotRunningLoop() {
    for {
        m.findNotRunning()
        time.Sleep(findNotRunningInterval)
    }
}

func (m *Manager) findNotRunning() error {
    // 1. 列出 Redis 中所有 Solution
    solutions := m.r.ListSolutionPoll()
    
    for _, item := range solutions {
        // 2. 检查是否已被锁定（正在评测中）
        if !m.isLocked(item) {
            // 3. 未锁定则重新启动评测
            go m.run(item)
        }
    }
}
```

---

### 3.4 Docker 执行器

#### 📄 `internal/executor/executor.go` - 执行器接口

```go
type ExecuteConfig struct {
    Image       string            // Docker 镜像
    Command     []string          // 执行命令
    Timeout     int64             // 超时时间（秒）
    MemoryLimit int64             // 内存限制（MB）
    CPULimit    float64           // CPU 限制（核心数）
    Env         map[string]string // 环境变量
    WorkDir     string            // 工作目录
    Mounts      []Mount           // 挂载配置
}

type ExecuteResult struct {
    ExitCode int     // 退出码
    Stdout   string  // 标准输出
    Stderr   string  // 标准错误
    TimedOut bool    // 是否超时
    OOM      bool    // 是否内存超限
}

type Executor interface {
    Execute(ctx, config) (*ExecuteResult, error)
    ExecuteWithLogs(ctx, config, callback) (*ExecuteResult, error)
    StreamLogs(ctx, containerID) (io.ReadCloser, error)
    Stop(ctx, containerID) error
    Cleanup(ctx, containerID) error
}
```

---

#### 📄 `internal/executor/docker.go` - Docker 实现

**功能**：使用 Docker API 执行评测容器

```go
func (e *DockerExecutor) ExecuteWithLogs(ctx, config, callback) (*ExecuteResult, error) {
    // 1. 创建容器配置
    containerConfig := &container.Config{
        Image:      config.Image,
        Cmd:        config.Command,
        WorkingDir: config.WorkDir,
        Env:        e.buildEnvList(config.Env),
    }
    
    // 2. 设置资源限制
    hostConfig := &container.HostConfig{
        Resources: container.Resources{
            Memory:    config.MemoryLimit * 1024 * 1024,  // 字节
            NanoCPUs:  int64(config.CPULimit * 1e9),      // 纳核
        },
        Mounts: e.buildMounts(config.Mounts),
    }
    
    // 3. 创建并启动容器
    resp := e.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
    e.client.ContainerStart(ctx, resp.ID, ...)
    
    // 4. 实时读取日志并调用回调
    go e.streamLogsWithCallback(ctx, resp.ID, callback)
    
    // 5. 等待容器结束
    statusCh, errCh := e.client.ContainerWait(ctx, resp.ID, ...)
    
    // 6. 检查超时和 OOM
    if ctx.Err() == context.DeadlineExceeded {
        result.TimedOut = true
    }
    if inspect.State.OOMKilled {
        result.OOM = true
    }
    
    // 7. 清理容器
    defer e.Cleanup(context.Background(), resp.ID)
    
    return result, nil
}
```

---

### 3.5 AOI 客户端

#### 📄 `pkg/aoiclient/client.go` - 客户端主类

```go
type Client struct {
    r *resty.Client  // HTTP 客户端
}

func New(addr string) *Client {
    return &Client{
        r: resty.New().SetBaseURL(addr),
    }
}

func (c *Client) Authenticate(id, key string) *Client {
    c.r.SetHeader("X-AOI-Runner-Id", id)
    c.r.SetHeader("X-AOI-Runner-Key", key)
    return c
}

func (c *Client) Poll(ctx) (*SolutionPoll, error)  // 轮询待评测任务
func (c *Client) Solution(solutionID, taskID) *SolutionClient
```

---

#### 📄 `pkg/aoiclient/solution.go` - 解答相关 API

```go
type SolutionPoll struct {
    TaskId           string        // 任务 ID
    SolutionId       string        // 提交 ID
    UserId           string        // 用户 ID
    ContestId        string        // 比赛 ID
    ProblemConfig    ProblemConfig // 题目配置
    ProblemDataUrl   string        // 题目数据 URL
    ProblemDataHash  string        // 题目数据哈希
    SolutionDataUrl  string        // 提交数据 URL
    SolutionDataHash string        // 提交数据哈希
}

type SolutionInfo struct {
    Score   float64  // 分数
    Status  string   // 状态
    Message string   // 消息
}

type SolutionDetails struct {
    Version int                   // 版本
    Jobs    []*SolutionDetailsJob // 子任务列表
    Summary string                // 总结
}

type SolutionClient struct {
    func Patch(ctx, info *SolutionInfo) error     // 更新状态
    func Complete(ctx) error                       // 完成评测
    func SaveDetails(ctx, details) error          // 保存详情
}
```

---

#### 📄 `pkg/aoiclient/status.go` - 状态常量

```go
const (
    StatusError               = "Error"
    StatusSuccess             = "Success"
    StatusAccepted            = "Accepted"
    StatusWrongAnswer         = "Wrong Answer"
    StatusTimeLimitExceeded   = "Time Limit Exceeded"
    StatusMemoryLimitExceeded = "Memory Limit Exceeded"
    StatusRuntimeError        = "Runtime Error"
    StatusCompileError        = "Compile Error"
    StatusInternalError       = "Internal Error"
)
```

---

### 3.6 评测协议

#### 📄 `pkg/judgerproto/protocol.go` - 通信协议

评测容器通过 **标准输出 (stdout)** 发送 JSON 消息与 Manager 通信。

```go
type Message struct {
    Time   time.Time       `json:"t"`  // 时间戳
    Action Action          `json:"a"`  // 动作类型
    Body   json.RawMessage `json:"b"`  // 消息体
}

// 动作类型
const (
    ActionGreet    = "0"  // 启动确认
    ActionNoop     = "n"  // 无操作
    ActionError    = "e"  // 错误
    ActionLog      = "l"  // 日志
    ActionComplete = "c"  // 完成
    ActionQuit     = "q"  // 退出
    ActionPatch    = "p"  // 更新状态
    ActionDetail   = "d"  // 保存详情
)
```

**消息示例**：
```json
// 启动确认
{"t":"2026-01-20T10:00:00Z","a":"0"}

// 更新分数和状态
{"t":"2026-01-20T10:00:05Z","a":"p","b":{"score":50,"status":"Running","message":"正在评测..."}}

// 保存详细结果
{"t":"2026-01-20T10:00:10Z","a":"d","b":{"version":1,"jobs":[{"name":"test1","score":50,"status":"Accepted"}],"summary":"通过 1/2"}}

// 完成评测
{"t":"2026-01-20T10:00:15Z","a":"c"}
```

---

## 4. 评测流程详解

### 4.1 完整流程图

```
┌─────────────────────────────────────────────────────────────────┐
│                           AOI 平台                               │
│  (用户提交代码 → 创建 Solution → 放入待评测队列)                  │
└───────────────────────────────┬─────────────────────────────────┘
                                │
                                │ HTTP API: POST /api/runner/solution/poll
                                ▼
┌───────────────────────────────────────────────────────────────────┐
│                          Manager                                   │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ pollLoop() - 每 250ms 轮询一次                                │ │
│  │   1. 检查速率限制 (RateLimiter)                              │ │
│  │   2. 调用 aoi.Poll() 获取 SolutionPoll                       │ │
│  │   3. 存入 Redis: StoreSolutionPoll()                         │ │
│  │   4. 启动协程: go m.run(id)                                   │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                │                                   │
│                                ▼                                   │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ run(id) - 执行评测                                            │ │
│  │   1. NewJudgeSession(id, m)                                   │ │
│  │   2. sess.Run()                                               │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                │                                   │
│                                ▼                                   │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ JudgeSession.Run()                                            │ │
│  │   1. tryLock() - 获取 Redis 分布式锁                          │ │
│  │   2. go lockLoop() - 后台刷新锁                               │ │
│  │   3. run() - 执行实际评测                                     │ │
│  │   4. cleanup() - 清理资源                                     │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                │                                   │
│                                ▼                                   │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ run() - 实际评测逻辑                                          │ │
│  │   1. buildExecuteConfig() - 构建 Docker 配置                  │ │
│  │   2. executeWithDocker() - 执行 Docker 容器                   │ │
│  └──────────────────────────────────────────────────────────────┘ │
└───────────────────────────────┬───────────────────────────────────┘
                                │
                                ▼
┌───────────────────────────────────────────────────────────────────┐
│                      DockerExecutor                                │
│   1. ContainerCreate() - 创建容器                                  │
│   2. ContainerStart() - 启动容器                                   │
│   3. streamLogsWithCallback() - 实时读取日志                       │
│   4. ContainerWait() - 等待容器结束                                │
│   5. ContainerRemove() - 清理容器                                  │
└───────────────────────────────┬───────────────────────────────────┘
                                │
                                ▼
┌───────────────────────────────────────────────────────────────────┐
│                        评测容器 (Docker)                           │
│   - 读取环境变量获取任务信息                                        │
│   - 下载提交数据和题目数据                                          │
│   - 执行评测逻辑                                                    │
│   - 通过 stdout 输出 JSON 消息                                      │
└───────────────────────────────┬───────────────────────────────────┘
                                │
                                │ stdout: JSON 消息
                                ▼
┌───────────────────────────────────────────────────────────────────┐
│                      processMessage()                              │
│   解析 JSON，根据 action 类型调用 AOI API:                          │
│   - "p" → aoi.Patch()      更新分数状态                            │
│   - "d" → aoi.SaveDetails()  保存详情                              │
│   - "c" → aoi.Complete()   完成评测                                │
└───────────────────────────────┬───────────────────────────────────┘
                                │
                                │ HTTP API
                                ▼
┌───────────────────────────────────────────────────────────────────┐
│                           AOI 平台                                 │
│  (更新 Solution 状态 → 用户查看评测结果)                            │
└───────────────────────────────────────────────────────────────────┘
```

---

### 4.2 数据流详解

#### 步骤 1: 用户提交
用户在 AOI 平台提交代码，平台创建 `Solution` 记录并放入待评测队列。

#### 步骤 2: Manager 轮询
```
AOI 平台  ─────────────────────────────>  Manager
          POST /api/runner/solution/poll
          
返回数据:
{
    "taskId": "task-123",
    "solutionId": "soln-456",
    "userId": "user-789",
    "problemConfig": {
        "judge": {
            "adapter": "docker",
            "config": {
                "image": "judge-cpp:latest",
                "command": ["/judge"],
                "timeout": 60,
                "memoryLimit": 256
            }
        }
    },
    "problemDataUrl": "https://...",
    "solutionDataUrl": "https://..."
}
```

#### 步骤 3: 存入 Redis
```
Redis Key: soln:soln-456:task-123
Redis Value: (JSON 序列化的 SolutionPoll)
```

#### 步骤 4: 启动评测容器
```bash
docker run \
  -e SOLUTION_ID=soln-456 \
  -e TASK_ID=task-123 \
  -e SOLUTION_DATA_URL=https://... \
  -e PROBLEM_DATA_URL=https://... \
  -v /data:/data:ro \
  --memory=256m \
  --cpus=1 \
  judge-cpp:latest \
  /judge
```

#### 步骤 5: 评测容器执行并输出消息
```bash
# 容器输出 (stdout)
{"t":"2026-01-20T10:00:00Z","a":"0"}
{"t":"2026-01-20T10:00:01Z","a":"p","b":{"score":0,"status":"Running"}}
{"t":"2026-01-20T10:00:05Z","a":"p","b":{"score":100,"status":"Accepted"}}
{"t":"2026-01-20T10:00:06Z","a":"d","b":{"version":1,"jobs":[...],"summary":"All tests passed"}}
{"t":"2026-01-20T10:00:07Z","a":"c"}
```

#### 步骤 6: 结果回传 AOI
```
Manager  ──────────────────────────>  AOI 平台
         PATCH /api/runner/solution/task/soln-456/task-123
         Body: {"score": 100, "status": "Accepted"}
         
         POST /api/runner/solution/task/soln-456/task-123/complete
```

---

## 5. 评测容器开发指南

### 5.1 基本要求

评测容器需要：
1. 读取环境变量获取评测信息
2. 下载并验证数据（提交数据、题目数据）
3. 执行评测逻辑
4. 通过 stdout 输出 JSON 消息

### 5.2 环境变量

| 变量名 | 说明 | 示例 |
|--------|------|------|
| `SOLUTION_ID` | 提交 ID | `soln-456` |
| `TASK_ID` | 任务 ID | `task-123` |
| `USER_ID` | 用户 ID | `user-789` |
| `SOLUTION_DATA_URL` | 提交数据下载 URL | `https://...` |
| `SOLUTION_DATA_HASH` | 提交数据 SHA256 | `abc123...` |
| `PROBLEM_DATA_URL` | 题目数据下载 URL | `https://...` |
| `PROBLEM_DATA_HASH` | 题目数据 SHA256 | `def456...` |
| `JUDGE_VARIABLES` | 自定义变量 (JSON) | `{"time_limit":1000}` |

### 5.3 输出消息格式

#### 更新状态 (Patch)
```json
{"t":"2026-01-20T10:00:00Z","a":"p","b":{"score":50,"status":"Running","message":"正在编译..."}}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `score` | float64 | 当前得分 (0-100) |
| `status` | string | 状态（见状态列表） |
| `message` | string | 显示给用户的消息 |

#### 保存详情 (Detail)
```json
{
  "t": "2026-01-20T10:00:00Z",
  "a": "d",
  "b": {
    "version": 1,
    "jobs": [
      {
        "name": "编译",
        "score": 0,
        "scoreScale": 0,
        "status": "Accepted",
        "tests": [],
        "summary": "编译成功"
      },
      {
        "name": "测试点",
        "score": 80,
        "scoreScale": 100,
        "status": "Wrong Answer",
        "tests": [
          {"name": "test1", "score": 20, "scoreScale": 20, "status": "Accepted", "summary": "通过"},
          {"name": "test2", "score": 20, "scoreScale": 20, "status": "Accepted", "summary": "通过"},
          {"name": "test3", "score": 20, "scoreScale": 20, "status": "Accepted", "summary": "通过"},
          {"name": "test4", "score": 20, "scoreScale": 20, "status": "Accepted", "summary": "通过"},
          {"name": "test5", "score": 0, "scoreScale": 20, "status": "Wrong Answer", "summary": "输出不匹配"}
        ],
        "summary": "通过 4/5 个测试点"
      }
    ],
    "summary": "得分: 80/100"
  }
}
```

#### 完成评测 (Complete)
```json
{"t":"2026-01-20T10:00:00Z","a":"c"}
```

#### 日志 (Log)
```json
{"t":"2026-01-20T10:00:00Z","a":"l","b":"这是一条日志消息"}
```

#### 错误 (Error)
```json
{"t":"2026-01-20T10:00:00Z","a":"e","b":"发生了错误: xxx"}
```

### 5.4 状态列表

| 状态 | 含义 |
|------|------|
| `Accepted` | 答案正确 |
| `Wrong Answer` | 答案错误 |
| `Time Limit Exceeded` | 超时 |
| `Memory Limit Exceeded` | 内存超限 |
| `Runtime Error` | 运行时错误 |
| `Compile Error` | 编译错误 |
| `Error` | 系统错误 |

### 5.5 示例：Python 评测容器

```python
#!/usr/bin/env python3
import os
import json
import urllib.request
from datetime import datetime

def output_message(action, body=None):
    """输出 JSON 消息"""
    msg = {
        "t": datetime.utcnow().isoformat() + "Z",
        "a": action
    }
    if body is not None:
        msg["b"] = body
    print(json.dumps(msg), flush=True)

def main():
    # 1. 发送启动确认
    output_message("0")
    
    # 2. 获取环境变量
    solution_id = os.environ.get("SOLUTION_ID")
    solution_url = os.environ.get("SOLUTION_DATA_URL")
    problem_url = os.environ.get("PROBLEM_DATA_URL")
    
    # 3. 更新状态
    output_message("p", {"score": 0, "status": "Running", "message": "下载数据..."})
    
    # 4. 下载数据
    urllib.request.urlretrieve(solution_url, "/tmp/solution.zip")
    urllib.request.urlretrieve(problem_url, "/tmp/problem.zip")
    
    # 5. 执行评测逻辑
    output_message("p", {"score": 0, "status": "Running", "message": "正在评测..."})
    
    # ... 评测逻辑 ...
    score = 100
    status = "Accepted"
    
    # 6. 保存详情
    output_message("d", {
        "version": 1,
        "jobs": [
            {
                "name": "测试",
                "score": score,
                "scoreScale": 100,
                "status": status,
                "tests": [],
                "summary": "全部通过"
            }
        ],
        "summary": f"得分: {score}/100"
    })
    
    # 7. 发送最终结果
    output_message("p", {"score": score, "status": status})
    
    # 8. 完成评测
    output_message("c")

if __name__ == "__main__":
    main()
```

### 5.6 示例：Dockerfile

```dockerfile
FROM python:3.11-slim

WORKDIR /app
COPY judge.py /app/

ENTRYPOINT ["python", "/app/judge.py"]
```

---

## 6. 部署与运行

### 6.1 前置要求

- Go 1.23+
- Docker
- Redis

### 6.2 构建

```bash
# 使用 just（如果已安装）
just build

# 或者直接使用 go build
go build -o ./build/manager ./cmd/manager
go build -o ./build/utility ./cmd/utility

# 构建 Docker 镜像
docker build -t lfs-auto-grader:latest .
```

### 6.3 注册 Runner

首先需要在 AOI 平台获取注册令牌 (Registration Token)，然后：

```bash
./build/utility register \
  --endpoint "https://hpcgame.pku.edu.cn" \
  --name "my-judger-1" \
  --label "default" \
  --token "your-registration-token" \
  --write-file "runner.env"
```

这会生成 `runner.env` 文件：
```
RUNNER_ID=xxx
RUNNER_KEY=xxx
```

### 6.4 运行 Manager

```bash
# 加载环境变量
source runner.env  # Linux/Mac
# 或在 PowerShell 中
Get-Content runner.env | ForEach-Object { $_ -match "(.+)=(.+)" | Out-Null; [Environment]::SetEnvironmentVariable($matches[1], $matches[2]) }

# 运行
./build/manager \
  -redis-config="redis://localhost:6379" \
  -endpoint="https://hpcgame.pku.edu.cn" \
  -rate-limit=32 \
  -shared-volume-path="/data"
```

### 6.5 使用 Docker 运行

```bash
docker run -d \
  --name judger \
  -e RUNNER_ID="your-runner-id" \
  -e RUNNER_KEY="your-runner-key" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /data:/data:ro \
  lfs-auto-grader:latest \
  -redis-config="redis://redis:6379" \
  -endpoint="https://hpcgame.pku.edu.cn" \
  -rate-limit=32
```

### 6.6 Docker Compose 示例

```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    volumes:
      - redis-data:/data

  judger:
    image: lfs-auto-grader:latest
    depends_on:
      - redis
    environment:
      - RUNNER_ID=your-runner-id
      - RUNNER_KEY=your-runner-key
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /data:/data:ro
    command:
      - -redis-config=redis://redis:6379
      - -endpoint=https://hpcgame.pku.edu.cn
      - -rate-limit=32

volumes:
  redis-data:
```

---

## 7. 常见问题

### Q1: 如何查看评测日志？

Manager 会将日志输出到标准输出，可以使用：
```bash
docker logs -f judger
```

### Q2: 评测容器无法访问网络？

默认情况下，评测容器可以访问网络。如需限制，可以在评测配置中添加网络限制。

### Q3: 如何增加并发数？

修改 `-rate-limit` 参数，同时确保 Redis 和 Docker 资源足够。

### Q4: 评测卡住怎么办？

1. 检查 Redis 中的锁状态
2. 检查 Docker 容器状态
3. `findNotRunningLoop` 会每 8 分钟自动重试未完成的任务

### Q5: 如何自定义评测超时？

在 AOI 平台的题目配置中设置：
```json
{
  "judge": {
    "config": {
      "image": "judge-image:latest",
      "timeout": 600  // 10 分钟
    }
  }
}
```

---

## 附录：代码结构图

```
hpcgame-judger/
│
├── cmd/                              # 可执行程序
│   ├── manager/main.go               # 主服务入口
│   └── utility/                      # CLI 工具
│       ├── main.go                   # 工具入口
│       ├── register.go               # 注册命令
│       └── pull.go                   # 轮询命令
│
├── internal/                         # 内部实现
│   ├── config/config.go              # 配置结构体
│   ├── executor/                     # 执行器
│   │   ├── executor.go               # 接口定义
│   │   └── docker.go                 # Docker 实现
│   ├── manager/                      # 核心逻辑
│   │   ├── manager.go                # Manager 主结构
│   │   ├── poll.go                   # 轮询逻辑
│   │   ├── session.go                # 评测会话
│   │   ├── running.go                # 评测执行
│   │   ├── protocol.go               # 协议处理
│   │   ├── redis.go                  # Redis 操作
│   │   ├── ratelimit.go              # 速率限制
│   │   ├── unrun.go                  # 未运行检测
│   │   └── utils.go                  # 工具函数
│   └── utils/                        # 通用工具
│       ├── jwt.go                    # JWT 解析
│       └── secretmanager.go          # 密钥管理
│
├── pkg/                              # 公开包
│   ├── aoiclient/                    # AOI 客户端
│   │   ├── client.go                 # 客户端主类
│   │   ├── solution.go               # 解答 API
│   │   ├── register.go               # 注册 API
│   │   ├── status.go                 # 状态常量
│   │   └── errors.go                 # 错误处理
│   └── judgerproto/protocol.go       # 评测协议
│
├── Dockerfile                        # Docker 构建
├── go.mod                            # Go 模块
├── justfile                          # 构建脚本
└── README.md                         # 说明文档
```

---

**文档版本**: 1.0  
**最后更新**: 2026-01-20
