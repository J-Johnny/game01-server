# Lobby Service

Lobby 是玩家业务状态的唯一写入服务。UserCenter 只维护账号、身份认证和 `account_id -> player_id` 关联；Gateway 完成认证后，必须通过内部 Streaming 调用 Lobby 创建或取得默认玩家，再创建带有 `player_id` 的 Gateway Session。

## 已实现的数据职责

Lobby 使用 MongoDB 官方 Driver，并在 MongoDB Replica Set 事务中维护以下集合：

| 集合 | 职责 | 关键约束 |
| --- | --- | --- |
| `players` | 玩家档案和默认角色标记 | `player_id` 唯一；同一账号允许多个玩家，但部分唯一索引确保最多一个 `is_default=true` |
| `player_assets` | 当前资产余额 | `player_id` 唯一；资产变更递增 `asset_version` |
| `asset_ledger` | 不可变的结算账本 | `settlement_id` 唯一，作为 Battle 重试的幂等键 |
| `player_snapshots` | 断线恢复所需的大厅快照 | `player_id` 唯一；每次结算后与资产同事务更新 |

当前自动登录只会创建或选择账号的默认玩家。后续多角色创建、删除、选择和改名必须通过 Lobby 协议实现，不能由 Gateway 或 UserCenter 直接写入这些集合。

## 内部协议

- `EnsurePlayerRequest`：Gateway 认证成功后调用。首次登录会在一个 MongoDB 事务内创建玩家档案、初始资产和初始快照，再调用 UserCenter 的 `LinkPlayer` 完成账号关联。
- `SettlementRequest`：Battle 通过 `LobbySettlementClient` 提交。Lobby 在同一个事务内写入账本、原子更新资产余额并刷新快照；重复 `settlement_id` 直接返回已提交结果，余额不足时整个事务回滚。
- `RestorePlayerStateRequest`：Gateway Resume 后调用。Lobby 优先从 `player_snapshots` 返回状态，快照缺失时由玩家档案和资产重建。

服务间只使用 gRPC Streaming 的 `InternalEnvelope`。Battle 不得直接引用 Lobby Repository 或 MongoDB 集合。

## 验证

单元测试：

```powershell
go test ./services/lobby/...
```

真实 MongoDB Replica Set 集成测试需要先启动 Compose 的 `mongo` 和 `mongo-rs-init`：

```powershell
$env:GAME_MONGO_REPLICA_URI = "mongodb://localhost:27017/?replicaSet=rs0&directConnection=true"
go test ./services/lobby/components -run MongoReplicaSet -count=1
```

## 尚未实现

- 多角色创建、选择、删除和默认角色切换协议。
- 玩家昵称、头像、区域等档案变更用例及版本冲突策略。
- 大厅会话、队伍、匹配、进入战斗和结算事件投递。
- Battle 房间结束时对 `LobbySettlementClient` 的真实调用和失败补偿。
