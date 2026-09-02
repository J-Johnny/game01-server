# User Center Service

用户中心负责账号安全、身份认证和账号关联。

UserCenter 的业务 API 放在 `components/` 目录中，并按模块拆分 component 文件；领域模型放在 `domain/`，业务语义 Repository 接口放在 `repository/`，MongoDB 适配器和持久化 Document/Mapper 放在 `repository/mongo/`，`module.go` 只负责组装和生命周期管理。

当前 `components/domain_auth_component.go` 提供游客认证、用户名密码认证、Refresh Token 轮换和撤销的内部 Streaming Handler。游客账号以 `install_id` 作为身份主体；用户名密码账号以唯一用户名作为身份主体，密码只保存 BCrypt 哈希。`account_id` 使用 UUID，Refresh Token 仅以哈希形式持久化。

Gateway 通过内部 gRPC Streaming 调用这些 Handler；当 Registry 尚未发现可用 UserCenter 实例时，认证请求会返回服务不可用并由 Gateway 转换为认证失败响应。UserCenter 运行时只使用官方 MongoDB Driver 的领域 Repository。旧 `repository/models`、旧 `repository/entities`、Qmgo 和 `LegacyAccountRepository` 已删除，不做历史数据迁移。旧 `user_center` 集合由 `server/scripts/drop-legacy-usercenter-collection.js` 提供一次性显式删除入口，服务启动不会自动删除数据；新集合保持不变。

Refresh Token 轮换通过 `common/mongodb.UnitOfWork` 使用 MongoDB 事务，要求 MongoDB Replica Set。幂等请求由 MongoDB 的 pending/completed 状态记录协调，执行期间自动续租；UserCenter 启动时回收过期 pending 记录，进程在业务事务提交后崩溃时，下一次重试可重新执行并完成记录。真实持久化集成测试位于 `repository/mongo/integration_test.go`，设置 `GAME_MONGO_REPLICA_URI` 后执行；未设置时测试会跳过。
