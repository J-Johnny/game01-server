# 服务端项目说明

服务端代码全部位于 `server/`。服务端采用一个 Go Module、多个业务服务的组织方式。

## 顶层结构

```text
server/
├── common/                  # 与具体业务服务无关的通用代码
├── services/                # 业务服务模块
│   ├── common/              # 多个业务服务共享的业务能力
│   ├── gateway/             # 网关服务
│   ├── lobby/               # 大厅服务
│   ├── usercenter/          # 用户中心
│   └── battle/              # 战斗服务
├── proto/                   # Protobuf 协议
│   ├── src/                 # .proto 源文件
│   └── gen/                 # 生成代码
├── go.mod
└── go.sum
```

## 目录职责

### common

`common/` 只存放可被多个业务服务复用的代码，例如：

- 配置、日志、错误码和链路追踪
- 网络传输、服务发现和 RPC 基础设施
- 数据库、缓存和消息队列适配器
- 通用生命周期、定时任务和并发工具

`common/` 不存放大厅、战斗或用户中心的具体业务规则，也不反向依赖 `services/`。

`common/reliability` 提供指数退避重试、令牌桶限流和可分类熔断器。Gateway 已将其用于 WebSocket 请求限流和 UserCenter 认证 Streaming 调用；只有连接关闭、超时等基础设施错误会触发重试/熔断。

### services

`services/` 按业务服务模块划分。唯一的 `main.go` 显式组装模块，并由 `services.<name>.enabled` 决定本进程实际启动的模块。每个模块保持独立的路由注册、生命周期与领域边界；Docker Compose 已按同一可执行文件、不同角色配置拆分部署。

建议每个服务内部按需要使用以下结构，不要求空目录预先铺满：

```text
services/<name>/
├── cmd/          # 服务启动入口
├── application/  # 用例编排
├── domain/       # 领域模型与业务规则
└── infra/        # 本服务专用的基础设施适配
```

- `common`：多个业务模块复用的业务能力和服务内协作契约。不得放入通用技术设施。
- `gateway`：连接、鉴权、会话、协议编解码、限流和业务路由。
- `lobby`：登录后的大厅会话、组队、匹配入口等。
- `usercenter`：账号安全、身份认证、认证身份绑定和账号关联；不保存玩家档案、资产或结算。
- `battle`：房间生命周期、固定 Tick、实体和权威战斗逻辑。

服务之间通过明确的 RPC/消息协议协作，不应直接引用另一个服务的内部领域包。

### proto

- `proto/src/` 保存人工维护的 `.proto` 文件，是协议的唯一事实来源。
- `proto/gen/` 保存工具生成的 Go 代码，不手工修改。
- 协议按业务域拆分，例如 `common.proto`、`lobby.proto`、`battle.proto` 和 `usercenter.proto`。
- 协议只描述跨进程或客户端/服务端契约，不泄露服务内部数据模型。

## 依赖方向

```text
services/* ──> common
services/* ──> proto/gen
proto/gen  ──> Protobuf runtime
common     -X-> services/*
```

客户端使用同一批 `.proto` 文件生成客户端语言代码，但不直接依赖任何服务端 Go 包。

Redis 由 `common/redis` 统一创建并注入依赖，Gateway 会话存储使用 `services/gateway/session.RedisStore`。本地 Compose 会自动启动 Redis，并通过 `GAME_REDIS_ADDR` 指向容器服务。

MongoDB 由 `common/mongodb` 统一创建并注入依赖。UserCenter 运行时使用官方 MongoDB Driver，通过领域 Repository 写入 `accounts`、`account_identities`、`refresh_tokens` 和 `idempotency_records` 独立集合；Lobby 使用 `players`、`player_assets`、`asset_ledger` 和 `player_snapshots` 管理玩家档案、当前资产、结算账本与恢复快照；Battle 使用 `battle_room_snapshots` 保存房间权威状态。Lobby 的玩家初始化、结算和 Battle 的房间快照使用官方 MongoDB Driver，结算与玩家数据使用 MongoDB Replica Set 本地事务；`settlement_id` 是不可变账本的唯一幂等键。Qmgo、旧模型和旧 Repository 已从运行时代码删除，不做历史数据迁移。旧 `user_center` 集合需要由运维人员显式执行一次性脚本删除，服务启动不会自动删库。

删除旧 UserCenter 集合（执行前请确认 URI 和数据库）：

```powershell
mongosh "mongodb://localhost:27017/game01" scripts/drop-legacy-usercenter-collection.js
```

脚本只删除 `user_center`，不会删除 `accounts`、`account_identities`、`refresh_tokens` 或 `idempotency_records`。

## Docker

在 `server/` 目录执行：

```bash
docker compose up --build
```

本地 Compose 会定义 Nginx、两个 Gateway 实例、`usercenter`、`lobby`、`battle`、Redis、单节点 MongoDB Replica Set 与 etcd。按需指定服务名即可只启动测试所需角色；`mongo-rs-init` 负责一次性初始化 `rs0`，业务容器等待初始化成功后再启动。四个业务容器使用同一镜像，通过各自的 `config.<role>.yaml` 启动。默认由 Compose 启动 Nginx，并使用 `least_conn` 将新 WebSocket 连接分配到 `gateway` 和 `gateway-2`，客户端统一访问 `ws://localhost:8080/ws`。本地 Nginx 是可选开发模式：两个 Gateway 映射到宿主机 `18081/18082`，由 `code/nginx-1.31.4` 转发。所有服务的健康检查地址为 `/healthz`，Gateway 额外提供 `/readyz` 并继续保留 `/ping` 兼容性。镜像使用多阶段构建，运行阶段以非 root 用户 `game` 启动。

Compose 中 etcd 的客户端和 peer 广播地址使用服务名 `etcd`，不能使用 `0.0.0.0`，否则 etcd 客户端会被引导到不可连接的地址。

```powershell
docker compose up --build -d
docker compose ps
docker compose logs -f gateway gateway-2 usercenter
```

多实例测试入口（默认使用 Compose Nginx）：

```powershell
docker compose up --build -d nginx gateway gateway-2 usercenter redis mongo etcd
```

容器化 Nginx 配置位于 `nginx/nginx.conf`，默认随 Compose 启动；本地 Nginx 配置位于 `../nginx-1.31.4/conf/nginx.conf`。切换到本地 Nginx 前先停止 Compose 的 `nginx` 容器，并启动两个 Gateway：

```powershell
docker compose stop nginx
docker compose up -d gateway gateway-2
cd ..\nginx-1.31.4
.\nginx.exe -t
.\nginx.exe
```

重载配置使用 `.\nginx.exe -s reload`，停止使用 `.\nginx.exe -s stop`。切回 Compose Nginx 时先执行 `.\nginx.exe -s stop`，再执行 `docker compose up -d nginx gateway gateway-2`。开源 Nginx 使用连接失败后的被动摘除；生产环境可将同一入口替换为云负载均衡器，并启用 `/readyz` 主动健康检查。已建立的 WebSocket 不会迁移，Gateway 故障后由客户端重连并执行 Resume/Refresh。

运行真实 MongoDB Replica Set 集成测试（需要先启动 Compose 的 `mongo` 和 `mongo-rs-init`）：

```powershell
$env:GAME_MONGO_REPLICA_URI = "mongodb://localhost:27017/?replicaSet=rs0&directConnection=true"
go test ./services/usercenter/repository/mongo -run ReplicaSet -count=1
go test ./services/lobby/components -run MongoReplicaSet -count=1
```

启动日志后台面板（测试 Gateway/UserCenter 时）：

```powershell
docker compose up --build -d gateway usercenter redis mongo etcd loki alloy grafana
```

Grafana 地址为 `http://localhost:3000`，默认账号为 `admin`，默认密码为 `admin`（生产环境必须通过 Secret 覆盖）。Loki 地址为 `http://localhost:3100`，通常通过 Grafana 查询，不直接暴露给客户端。

如果之前启动过单体 `game-server`，先清理旧的 Compose 容器并强制重建角色容器，避免旧容器占用 `8080`：

```powershell
docker compose down --remove-orphans
docker compose up --build --force-recreate -d gateway usercenter
```

默认 Gateway 入口地址为 `http://localhost:8080`，WebSocket 地址为 `ws://localhost:8080/ws`，也可以通过 `GAME_GATEWAY_HTTP_PORT` 修改 Compose Nginx 宿主机端口。使用本地 Nginx 时，Gateway 后端端口由 `GAME_GATEWAY_BACKEND_1_PORT`（默认 `18081`）和 `GAME_GATEWAY_BACKEND_2_PORT`（默认 `18082`）控制，仅用于本机反向代理，不应在生产环境直接暴露。

Gateway 可靠性配置位于 `gateway` 配置段：`rate_limit_burst`、`rate_limit_per_second`、`retry_attempts`、`circuit_failures` 和 `circuit_reset_timeout`。

### Production

生产部署使用独立的 `docker-compose.production.yml`，依赖外部 Redis、MongoDB 和 etcd，并启动四个角色容器。`.env.production.example` 要求为每个角色分别注入镜像版本、实例 ID、节点号、私网 gRPC 地址和数据库凭据：

```powershell
Copy-Item .env.production.example .env.production
# 编辑 .env.production，替换所有占位值
docker compose --env-file .env.production -f docker-compose.production.yml up -d
```

生产 Compose 只暴露 Gateway HTTP 端口；Redis、MongoDB、etcd 和内部 gRPC 不发布到公网。所有 `GAME_<ROLE>_ID_NODE_ID` 必须在全部角色及副本之间唯一，密钥应由部署平台的 Secret Store 注入，不要提交 `.env.production`。

### Logging and Observability

服务使用 JSON `slog` 输出到容器标准输出。Gin HTTP 请求、WebSocket 连接和内部 gRPC Streaming 连接/消息均带有结构化字段，并通过 `X-Request-ID` 关联请求。密码、Refresh Token、Authorization 和完整 Protobuf 内容不会写入日志。

本地观测组件为 Grafana、Loki 和 Grafana Alloy：Alloy 读取 Docker 容器日志并发送到 Loki，Grafana 自动配置 Loki 数据源和 `Game01 Logs` 面板。日志标签只保留低基数字段（服务、环境、协议、级别）；`request_id`、账号和会话字段保留在 JSON 内容中用于查询。

幂等请求在执行期间会自动续租；UserCenter 启动时会清理已过期的 pending 记录，使业务事务已提交但进程在幂等 Complete 前崩溃的请求可以在下一次重试时重新执行并完成幂等记录。

## Internal gRPC

服务端默认监听 `:9090` 提供 `ServiceStream.Connect` 双向流。`grpc.listen_address` 用于本地监听，`grpc.advertise_address` 用于写入服务发现，二者在容器或多网卡环境中应分别配置。

启用的业务模块会以 `/services/game01/<service>/<instance_id>` 注册到配置的 Registry。开发环境默认使用 Static Registry；Docker Compose 使用 etcd。

## ID Generator

`account_id` 和 `player_id` 使用 `common/idgen.NewUUID` 生成 UUIDv7 字符串，适合跨服务、客户端和持久化边界传递。Snowflake 格式的 `uint64` ID 保留给房间、战斗、结算等高频服务端对象：41 bit 时间戳、10 bit 节点 ID、12 bit 毫秒内序列号。通过 `id_generator.node_id` 或 `GAME_ID_NODE_ID` 配置节点号；同一部署范围内每个运行实例必须使用不同节点号。
