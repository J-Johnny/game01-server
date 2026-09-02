# Gateway Service

网关服务负责客户端连接、鉴权、会话、协议编解码、限流和业务路由。

网关不持有大厅、用户或战斗的权威业务状态，只将请求转换为对应模块的调用或命令。

WebSocket 连接成功认证后会绑定 `session_id`。连接关闭时，Gateway 使用 `connection_id` 条件更新 Redis Session 为 `reconnecting`，并设置 `reconnect_grace_period` 作为 Resume 宽限期；旧连接在新连接已 Resume 后触发关闭不会覆盖新连接状态。
