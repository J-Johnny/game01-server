# Performance Testing

Performance testing uses a separate Compose project so its Redis, MongoDB, etcd, volumes, and MongoDB database are not shared with the development environment.

The performance override uses `mongo:7` so the isolated environment remains compatible with cloud hosts running Linux kernel 6.19 or newer. The standard development Compose configuration continues to use its declared MongoDB version.

## Start the Isolated Environment

Run from `server/`:

```powershell
docker compose -p game01-perf -f docker-compose.yml -f docker-compose.perf.yml up --build -d nginx usercenter lobby
```

The public Gateway endpoint is `ws://127.0.0.1:18080/ws`. The two direct Gateway metrics endpoints are `http://127.0.0.1:28081/metrics` and `http://127.0.0.1:28082/metrics`. The services use the `game01_perf` MongoDB database and isolated Compose volumes.

Do not run production load tests against the development or production Compose project.

## Test Order

1. Check `http://127.0.0.1:18080/readyz` returns `204`.
2. Run `connect-hold` at 100 connections for five minutes.
3. Run a password-login warmup with a unique `username-prefix` and stable `run-id`.
4. Repeat the same password-login command to measure existing-account authentication.
5. Run `resume-storm` and then repeat with increasing connection counts.
6. Collect both Gateway metrics and `docker stats` during every run.
7. Perform Gateway, UserCenter, and Lobby failure injection only after a stable baseline has been captured.

## Commands

```powershell
go run ./cmd/loadtest -scenario connect-hold -target ws://127.0.0.1:18080/ws -connections 100 -ramp-per-second 25 -duration 5m
```

```powershell
go run ./cmd/loadtest -scenario password-login -target ws://127.0.0.1:18080/ws -connections 100 -ramp-per-second 20 -duration 2m -username-prefix perf-password -run-id baseline-001
```

Run that password command once to create the test accounts. Run it again with the same `username-prefix` and `run-id` to exercise existing-account BCrypt verification.

```powershell
go run ./cmd/loadtest -scenario resume-storm -target ws://127.0.0.1:18080/ws -connections 100 -ramp-per-second 20 -duration 5m -resume-interval 1s
```

## Cleanup

The following removes only the dedicated `game01-perf` project and its named volumes:

```powershell
docker compose -p game01-perf -f docker-compose.yml -f docker-compose.perf.yml down --volumes
```
