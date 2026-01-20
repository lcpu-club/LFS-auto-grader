# LFS Auto Grader

基于 Docker 的自动评测系统，与 AOI (Azukiiro) 平台集成。

## 项目结构

```
lfs-auto-grader/
├── cmd/
│   ├── manager/          # 主服务入口
│   └── utility/          # CLI 工具
├── internal/
│   ├── config/           # 配置定义
│   ├── executor/         # Docker 执行器
│   │   ├── executor.go   # 执行器接口
│   │   └── docker.go     # Docker 实现
│   ├── manager/          # 核心管理器
│   │   ├── manager.go    # Manager 结构体
│   │   ├── poll.go       # 任务轮询
│   │   ├── protocol.go   # 消息处理
│   │   ├── ratelimit.go  # 速率限制
│   │   ├── redis.go      # Redis 操作
│   │   ├── running.go    # 评测执行
│   │   └── session.go    # 会话管理
│   └── utils/            # 工具函数
├── pkg/
│   ├── aoiclient/        # AOI API 客户端
│   ├── framework/        # 评测框架
│   └── judgerproto/      # 通信协议
├── Dockerfile
├── go.mod
└── justfile
```

## 技术栈

- **Go 1.23+**
- **Docker** - 容器化评测隔离
- **Redis** - 分布式状态管理和速率限制
- **AOI/Azukiiro** - 后端平台 API

## 快速开始

### 前置要求

- Go 1.23+
- Docker
- Redis

### 构建

```bash
# 构建所有
just build

# 仅构建 manager
just build-manager

# 构建 Docker 镜像
just build-image
```

### 运行

```bash
# 设置环境变量
export RUNNER_ID="your-runner-id"
export RUNNER_KEY="your-runner-key"

# 运行
./build/manager \
  -redis-config="redis://localhost:6379" \
  -endpoint="https://your-aoi-endpoint.com" \
  -rate-limit=32
```

### 配置参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-listen` | `:8080` | 服务监听地址 |
| `-redis-config` | `redis://localhost:6379` | Redis 连接字符串 |
| `-endpoint` | `https://hpcgame.pku.edu.cn` | AOI 平台 API 地址 |
| `-runner-id` | 环境变量 `RUNNER_ID` | Runner ID |
| `-runner-key` | 环境变量 `RUNNER_KEY` | Runner Key |
| `-rate-limit` | `64` | 最大并发评测数 |
| `-shared-volume-path` | `/data` | 共享数据目录 |

## 评测配置

评测任务配置通过 AOI 平台的 `problemConfig.judge.config` 字段传递：

```json
{
  "image": "judge-image:latest",
  "command": ["/judge"],
  "timeout": 300,
  "memoryLimit": 512,
  "cpuLimit": 1.0,
  "env": {
    "CUSTOM_VAR": "value"
  },
  "variables": {}
}
```

### 配置字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `image` | string | Docker 镜像名称 |
| `command` | string[] | 执行命令 |
| `timeout` | int64 | 超时时间（秒），默认 300 |
| `memoryLimit` | int64 | 内存限制（MB），默认 512 |
| `cpuLimit` | float64 | CPU 限制（核心数） |
| `env` | object | 额外环境变量 |
| `variables` | object | 自定义变量 |

### 自动注入的环境变量

评测容器会自动注入以下环境变量：

| 变量名 | 说明 |
|--------|------|
| `SOLUTION_ID` | 解答 ID |
| `TASK_ID` | 任务 ID |
| `USER_ID` | 用户 ID |
| `SOLUTION_DATA_URL` | 解答数据下载 URL |
| `SOLUTION_DATA_HASH` | 解答数据哈希 |
| `PROBLEM_DATA_URL` | 题目数据下载 URL |
| `PROBLEM_DATA_HASH` | 题目数据哈希 |
| `JUDGE_VARIABLES` | 自定义变量 (JSON) |

## 评测协议

评测容器通过标准输出与 Manager 通信，使用 JSON 格式：

```json
{"t": "2024-01-01T00:00:00Z", "a": "p", "b": {"score": 100, "status": "Accepted"}}
```

### 消息类型

| Action | 说明 |
|--------|------|
| `0` | Greet - 启动确认 |
| `p` | Patch - 更新分数和状态 |
| `d` | Detail - 保存详细结果 |
| `c` | Complete - 完成评测 |
| `q` | Quit - 退出 |
| `l` | Log - 日志 |
| `e` | Error - 错误 |

## 评测状态

| 状态 | 说明 |
|------|------|
| `Accepted` | 答案正确 |
| `Wrong Answer` | 答案错误 |
| `Time Limit Exceeded` | 超时 |
| `Memory Limit Exceeded` | 内存超限 |
| `Runtime Error` | 运行时错误 |
| `Compile Error` | 编译错误 |
| `Error` | 系统错误 |

## License

MIT
