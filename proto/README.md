# Protobuf Protocols

- `src/`：人工维护的 `.proto` 协议源文件。
- `gen/`：协议工具生成的 Go 代码，禁止手工编辑。

客户端应从 `src/` 中的同一协议源生成对应语言的代码。

## Generate

在 `server/` 目录运行以下命令生成 Go 与 Unity C# 协议代码：

```powershell
.\proto\generate.ps1
```

Go 输出位于 `proto/gen/`，其中内部协议包为 `proto/gen/internalpb/`；Unity C# 输出位于 `client/Assets/Scripts/Protocol/Generated/`。

客户端协议位于 `src/client/`，生成 Go 和 Unity C# 代码。服务间协议位于 `src/internal/`，生成 Go Protobuf 与 gRPC 代码；内部协议不输出到客户端。

客户端状态恢复协议位于 `src/client/state/`。Lobby 和 Battle 各自将领域状态编码为公开状态消息后放入 `RestorePlayerStateResponse.snapshot`；Gateway 只原样转发 payload。Unity 客户端只依赖 `Game01.Protocol.Client.State`，不直接引用内部服务协议。

## Protocol Rules

- `.proto` 源文件是协议唯一事实来源；`gen/` 和 Unity `Generated/` 目录只存放生成文件。
- 已发布字段编号不可复用；删除字段必须保留编号。
- 客户端 `Envelope.message_id` 使用 `ClientMessageId` 枚举。请求使用 `1-99`，响应使用 `100-899`，框架错误使用 `900-999`。
- 内部 `InternalEnvelope` 的 `request_id` 用于请求/响应关联；`EVENT` 不要求响应。
- 内部请求使用 `target_service + message_id` 路由；每个 Handler 的响应必须使用其声明的响应 `message_id`。
- 第一阶段客户端仅启用 `AUTH_PROVIDER_GUEST`；Steam、Apple、Google、微信枚举仅为未来兼容预留，Gateway 会拒绝尚未启用的 Provider。
- 业务 `payload` 必须是对应 `message_id` 定义的 Protobuf 消息，不能混用 JSON。
- `RestorePlayerStateResponse` 的 `snapshot` 必须使用公开客户端状态协议编码；业务服务必须填写匹配的 `payload_type` 和 `schema_version`。Gateway 不得解析或转换该 payload。
- 登录、刷新登录及内部认证请求支持可选 `idempotency_key`；同一操作使用相同键和参数时回放首次响应，参数变化会返回幂等冲突。
