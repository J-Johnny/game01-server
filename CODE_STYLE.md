# 服务端代码规范

本文档适用于 `server/` 下的 Go 代码、Protobuf 协议和服务端配置。代码评审、提交前检查以及新增服务模块都应遵守这些约定。

## 1. 工程边界

服务端只有根目录的 `main.go` 作为进程入口。所有业务模块由 `main.go` 显式注册，再根据配置决定是否启用；业务包不得自行创建第二个进程入口。

```text
server/
├── common/                 # 通用技术基础设施，不依赖具体业务
├── services/               # 业务服务模块
│   ├── common/             # 多个业务服务共享的业务能力
│   ├── gateway/
│   ├── lobby/
│   ├── usercenter/
│   └── battle/
├── proto/
│   ├── src/                # 手工维护的 .proto 源文件
│   └── gen/                # 生成代码
└── main.go
```

- `common/` 不得反向依赖 `services/`，也不得放入大厅、战斗或用户中心的业务规则。
- `services/<name>/components/` 存放该服务对外暴露的业务 API 组件。
- 服务之间通过 Protobuf/gRPC 契约通信，不直接引用其他服务的领域模型、Repository 或数据库对象。
- Domain 模型不依赖 MongoDB、BSON、Redis 或 gRPC 类型；基础设施适配放在服务的 `repository`、`infra` 等边界内。

## 2. 命名与格式

- 使用 Go 官方命名习惯：导出标识符使用 PascalCase，包名使用简短的小写单词，不使用下划线。
- 局部变量和私有字段使用 camelCase；缩写按 Go 约定书写，例如 `ID`、`HTTP`、`URL`。
- 错误变量使用 `Err` 前缀，接口名称优先以行为命名，例如 `Registry`、`Authenticator`。
- 文件名使用小写下划线或项目已有的命名风格；一个文件应围绕单一职责组织。
- 所有 Go 文件必须通过 `gofmt`；导入分组和排序交给 `gofmt`/工具处理。
- 函数与函数、函数与结构体、结构体与结构体之间必须保留一个空行。相关方法可以按类型连续排列，但不同逻辑区块之间应留空行。

结构体复合字面量使用多行、逐字段赋值并保留尾逗号的风格：

```go
return &Module{
	name:        name,
	serviceType: serviceType,
	deps:        deps,
}
```

禁止将同一复合字面量压缩成一行，也不要依赖字段顺序进行隐式赋值。

## 3. 依赖与生命周期

- 依赖通过构造函数、模块注册接口或配置注入；业务包不在函数内部隐式创建全局数据库、Redis、etcd 或 gRPC 客户端。
- 启动顺序遵循：加载配置 -> 初始化日志 -> 初始化基础设施 -> 注册/发现服务 -> 启动 gRPC/HTTP -> 启动业务模块。
- 每个资源都必须有明确的关闭路径，使用 `context.Context` 取消和 `defer` 释放连接、游标及流。
- `context.Context` 作为需要上下文的函数的第一个参数，不保存到结构体中，不传递 `nil`。
- 后台 goroutine 必须有退出条件；重连、Watch 和定时任务需要响应 context 取消。

## 4. 协议与网络

- `proto/src/` 是协议唯一事实来源；修改 `.proto` 后重新生成 Go/C# 代码。
- `proto/gen/` 下的生成文件禁止手工修改，也不在生成文件中添加业务逻辑。
- 客户端 Gateway 使用 WebSocket 二进制帧和 Protobuf `Envelope`；服务间统一使用 gRPC 双向 streaming。
- 业务消息按 `target_service` 和 `message_id` 分派到对应 Handler；Handler 只处理自己的消息类型，并返回明确的业务错误。
- 协议字段编号一经发布不得复用；删除字段使用 `reserved` 保留编号和名称。
- 外部请求需要校验长度、编码、认证状态和幂等键，不能信任客户端传入的身份字段。

## 5. 错误、日志与安全

- 使用 `%w` 包装底层错误；可被调用方判断的业务失败使用领域 sentinel error 或明确错误码。
- 错误信息应包含操作上下文，但不得泄露密码、Refresh Token、Authorization、完整 Protobuf 或数据库凭据。
- 使用 JSON `slog` 输出结构化日志；HTTP、WebSocket、gRPC Streaming 请求应携带 `request_id`，低基数字段优先作为日志属性。
- 认证日志只记录结果、原因分类和必要的账号标识摘要，不记录密码和 Token 原文。
- 所有外部输入都要做边界校验；数据库查询使用参数化 API，不拼接查询字符串。
- 配置中的密钥通过环境变量或 Secret 注入，禁止提交真实凭据和生产配置文件。

## 6. UserCenter 与持久化

- UserCenter 只负责账号安全、身份认证、认证身份绑定和账号关联；玩家档案、资产、结算入账及基础玩家查询属于 Lobby。
- `account_id -> player_id` 是一对多关系，ID 使用 UUID 字符串生成器。
- Domain Entity、持久化 Document 和传输 DTO 分离；MongoDB/BSON 标签只能出现在 `repository/mongo` 的 Document 中。
- MongoDB 复合索引使用有序 `bson.D`，不要使用多字段 `map[string]int`，避免索引字段顺序不确定。
- 事务边界由 `common/mongodb.UnitOfWork` 统一管理；事务内只执行同一 MongoDB Replica Set 支持的操作。
- Repository 返回领域对象或明确的存储错误，不把 qmgo、Mongo Driver 类型泄露到领域层。
- 服务启动禁止自动删除或迁移数据；破坏性清理必须由明确的一次性运维脚本执行。

## 7. 测试与提交前检查

新增功能至少覆盖正常路径、参数校验、认证失败、超时/取消和依赖异常等关键分支。涉及协议、Repository 或跨服务调用时，应补充集成测试。

提交前执行：

```powershell
gofmt -w .
go test ./...
go vet ./...
git diff --check
```

提交信息应简洁说明行为变化，避免把生成文件、临时日志、编译产物或本地配置提交到仓库。

