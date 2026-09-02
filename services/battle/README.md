# Battle Service

战斗服务负责房间生命周期、固定 Tick、实体管理和权威战斗逻辑。房间状态只能由 Battle 的 `RoomManager` 在串行变更入口中修改，网络协程不得直接写入快照或 Lobby 数据库。

## 权威房间快照

`domain.Room` 保存房间 Tick、状态版本、生命周期状态以及玩家的 HP 和位置。`RoomManager.Mutate` 持有房间管理锁执行状态变更，成功后递增 `tick` 和 `state_version`，并将完整快照写入 MongoDB；快照写入失败会恢复内存中的上一版本。

快照集合为 `battle_room_snapshots`：

- `room_id` 唯一，避免同一房间出现多个当前版本。
- `players.player_id` 普通索引，用于 Gateway Resume 按玩家查找所在房间。
- 快照包含完整房间状态，不记录客户端输入命令，恢复后由 Battle 继续作为唯一权威来源。

Battle 收到 `RestorePlayerStateRequest` 后根据客户端版本返回三种模式：版本一致返回 `NOOP`；版本落后且内存 Ring Buffer 存在连续变更时返回 `DELTA`（`BattleRoomDelta`）；否则返回 `FULL` 完整 `BattleRoomSnapshot`。Ring Buffer 默认保留最近 1024 个版本，进程重启后因无增量历史自动回退完整快照。Battle 自行编码公开客户端状态协议并填写 `payload_type/schema_version`；Gateway 只透传模式、元数据和 payload，客户端负责校验并应用。

## 使用方式

房间创建和状态变更由后续房间服务调用：

```go
room, err := domain.NewRoom(roomID, players, time.Now().UTC())
if err != nil {
	return err
}
if err := module.Rooms().Create(ctx, room); err != nil {
	return err
}
return module.Rooms().Mutate(ctx, roomID, func(room *domain.Room) error {
		return room.UpdatePlayer(nextPlayerState)
})
```

当前仍未实现真实房间创建协议、固定 Tick 调度、输入命令队列、战斗实体同步和房间结束结算；`LobbySettlementClient` 仅提供结算调用适配层。
