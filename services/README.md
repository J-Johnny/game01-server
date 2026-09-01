# Services

该目录按业务服务组织。所有模块由 `server/main.go` 显式创建、注入和启动；服务间通信只通过内部 Protobuf/gRPC 协议完成。

```text
services/
├── common/      # 共享模块运行时、内部状态与协作契约
├── gateway/     # WebSocket、会话和客户端协议分发
├── lobby/       # 大厅、队伍和匹配
├── usercenter/  # 账号安全、身份认证和账号关联
└── battle/      # 房间、Tick 和战斗权威状态
```

每个业务服务在自己的目录维护 `module.go`，负责服务构造、HTTP 路由（如有）和内部消息注册。业务服务不得直接导入另一业务服务的内部实现。

业务服务 API 统一放在对应服务的 `components/` 目录中，按照业务模块拆分为独立的 component 文件。例如 `services/usercenter/components/auth_component.go`、`services/lobby/components/match_component.go`。Component 负责 API 接口、请求校验和用例调用；`module.go` 只负责依赖注入、Component 组装、路由或消息注册以及生命周期管理。
