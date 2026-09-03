# Gateway Load Test

`cmd/loadtest` uses the generated client Protobuf definitions and sends binary WebSocket frames to the public Gateway endpoint. It is intended for isolated local or performance environments only.

## Scenarios

- `connect-hold`: establish and retain WebSocket connections.
- `guest-login`: authenticate unique guest installs and retain the connections.
- `password-login`: authenticate password accounts and retain the connections. UserCenter creates an account when a username is first seen.
- `resume-storm`: create a guest session, repeatedly disconnect, and Resume within the configured grace period.

## Examples

Run from `server/` against Compose Nginx:

```powershell
go run ./cmd/loadtest -scenario connect-hold -target ws://127.0.0.1:8080/ws -connections 100 -ramp-per-second 25 -duration 5m
```

Create a stable set of password accounts, then repeat the exact command with the same `run-id` to measure existing-account BCrypt verification instead of account creation:

```powershell
go run ./cmd/loadtest -scenario password-login -target ws://127.0.0.1:8080/ws -connections 100 -ramp-per-second 20 -duration 2m -username-prefix perf-password -run-id baseline-001
```

```powershell
go run ./cmd/loadtest -scenario resume-storm -target ws://127.0.0.1:8080/ws -connections 100 -ramp-per-second 20 -duration 5m -resume-interval 1s
```

Observe `http://127.0.0.1:3000`, `http://127.0.0.1:9091`, container resource use, and both Gateway `/metrics` endpoints during a run. Use a separate MongoDB database and a unique `username-prefix`/`run-id` for every performance test campaign.
