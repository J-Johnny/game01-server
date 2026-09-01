FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/game-server .

FROM alpine:3.22
RUN addgroup -S game && adduser -S -G game game
WORKDIR /app
COPY --from=build /out/game-server /app/game-server
COPY config/ /app/config/
USER game
EXPOSE 8080
EXPOSE 9090
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD wget --spider --quiet http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/game-server"]
CMD ["--config", "/app/config/config.local.yaml"]
