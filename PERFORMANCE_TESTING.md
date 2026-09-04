# 性能压测说明

性能压测使用独立的 Compose 项目。该项目拥有独立的 Redis、MongoDB、etcd、数据卷和 MongoDB 数据库，不会与开发环境共享数据。

压测环境使用 `mongo:8.0`，与当前性能 Compose 配置和 MongoDB 副本集初始化方式保持一致。

## 启动独立压测环境

在 `server/` 目录执行：

```powershell
docker compose -p game01-perf -f docker-compose.yml -f docker-compose.perf.yml up --build -d nginx usercenter lobby
```

公共 Gateway 地址：`ws://127.0.0.1:18080/ws`。

两个 Gateway 实例的指标地址：

```text
http://127.0.0.1:28081/metrics
http://127.0.0.1:28082/metrics
```

压测服务使用 `game01_perf` 数据库和独立的 Compose 数据卷。

不要对开发环境或生产环境的 Compose 项目直接执行压力测试。

## 阶梯压测

每个并发梯度开始前，应先清理并重启专用压测环境：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\reset-perf-stair.ps1
```

该脚本只操作 `game01-perf` 项目，依次启动基础设施、停止游戏服务、删除 `game01_perf` 数据库、清空 Redis，再重启游戏服务并等待健康检查通过。

运行单个阶梯：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\run-perf-stair.ps1 `
  -Scenario password-login `
  -Connections 500 `
  -RampPerSecond 25 `
  -DurationSeconds 20
```

支持的场景为 `guest-login`（游客登录）和 `password-login`（密码登录）。

## 推荐测试顺序

1. 确认 `http://127.0.0.1:18080/readyz` 返回 `204`。
2. 使用 100 个连接运行 `connect-hold`，持续 5 分钟，验证长连接稳定性。
3. 使用唯一的 `username-prefix` 和 `run-id` 执行密码登录预热，创建测试账号。
4. 使用相同的 `username-prefix` 和 `run-id` 再次执行，测试已存在账号的 BCrypt 校验。
5. 执行 `resume-storm`，逐步提高连接数。
6. 每次测试同时采集 Gateway 指标和 `docker stats`。
7. 稳定基线采集完成后，再执行 Gateway、UserCenter 和 Lobby 故障注入。

## 常用命令

保持 100 个连接 5 分钟：

```powershell
go run ./cmd/loadtest -scenario connect-hold -target ws://127.0.0.1:18080/ws -connections 100 -ramp-per-second 25 -duration 5m
```

首次密码登录，用于创建测试账号：

```powershell
go run ./cmd/loadtest -scenario password-login -target ws://127.0.0.1:18080/ws -connections 100 -ramp-per-second 20 -duration 2m -username-prefix perf-password -run-id baseline-001
```

再次执行相同命令，可测试已存在账号的密码校验。

Resume 压测：

```powershell
go run ./cmd/loadtest -scenario resume-storm -target ws://127.0.0.1:18080/ws -connections 100 -ramp-per-second 20 -duration 5m -resume-interval 1s
```

## 清理压测环境

以下命令只删除专用的 `game01-perf` 项目及其命名数据卷：

```powershell
docker compose -p game01-perf -f docker-compose.yml -f docker-compose.perf.yml down --volumes
```
