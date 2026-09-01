# User Center Service

用户中心负责账号安全、身份认证和账号关联。

UserCenter 的业务 API 放在 `components/` 目录中，并按模块拆分 component 文件；Repository 和数据模型分别放在 `repository/`、`repository/models/` 中，`module.go` 只负责组装和生命周期管理。

当前 `components/auth_component.go` 已提供游客认证、用户名密码认证、Refresh Token 轮换和撤销的内部 Streaming Handler。游客账号以 `install_id` 作为身份主体；用户名密码账号以唯一用户名作为身份主体，密码只保存 BCrypt 哈希。`account_id` 使用 UUID，Refresh Token 仅以哈希形式持久化。

Gateway 通过内部 gRPC Streaming 调用这些 Handler；当 Registry 尚未发现可用 UserCenter 实例时，认证请求会返回服务不可用并由 Gateway 转换为认证失败响应。
