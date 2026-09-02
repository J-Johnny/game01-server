package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

type Config struct {
	App         AppConfig                `yaml:"app"`
	HTTP        HTTPConfig               `yaml:"http"`
	GRPC        GRPCConfig               `yaml:"grpc"`
	Gateway     GatewayConfig            `yaml:"gateway"`
	UserCenter  UserCenterConfig         `yaml:"usercenter"`
	Redis       RedisConfig              `yaml:"redis"`
	Mongo       MongoConfig              `yaml:"mongo"`
	Discovery   DiscoveryConfig          `yaml:"discovery"`
	IDGenerator IDGeneratorConfig        `yaml:"id_generator"`
	Services    map[string]ServiceConfig `yaml:"services"`
	Battle      BattleConfig             `yaml:"battle"`
}

type AppConfig struct {
	Name            string        `yaml:"name"`
	InstanceID      string        `yaml:"instance_id"`
	Environment     string        `yaml:"environment"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type HTTPConfig struct {
	Address      string        `yaml:"address"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type GRPCConfig struct {
	ListenAddress    string `yaml:"listen_address"`
	AdvertiseAddress string `yaml:"advertise_address"`
}

type GatewayConfig struct {
	SessionTTL        time.Duration `yaml:"session_ttl"`
	ReconnectGrace    time.Duration `yaml:"reconnect_grace_period"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	HeartbeatTimeout  time.Duration `yaml:"heartbeat_timeout"`
}

type UserCenterConfig struct {
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl"`
	IdempotencyTTL  time.Duration `yaml:"idempotency_ttl"`
}

type RedisConfig struct {
	Address  string `yaml:"address"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type MongoConfig struct {
	URI            string        `yaml:"uri"`
	Database       string        `yaml:"database"`
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
	AuthMechanism  string        `yaml:"auth_mechanism"`
	AuthSource     string        `yaml:"auth_source"`
	Username       string        `yaml:"username"`
	Password       string        `yaml:"password"`
}

type DiscoveryConfig struct {
	Provider  string   `yaml:"provider"`
	Endpoints []string `yaml:"endpoints"`
	Namespace string   `yaml:"namespace"`
	LeaseTTL  int64    `yaml:"lease_ttl"`
}

type IDGeneratorConfig struct {
	NodeID uint16 `yaml:"node_id"`
}

type ServiceConfig struct {
	Enabled bool `yaml:"enabled"`
}

type BattleConfig struct {
	TickRate          int `yaml:"tick_rate"`
	MaxPlayersPerRoom int `yaml:"max_players_per_room"`
}

func Defaults() Config {
	return Config{
		App:         AppConfig{"game-server", "game-server-local", "local", 15 * time.Second},
		HTTP:        HTTPConfig{":8080", 10 * time.Second, 10 * time.Second},
		GRPC:        GRPCConfig{":9090", "127.0.0.1:9090"},
		Gateway:     GatewayConfig{24 * time.Hour, 30 * time.Second, 10 * time.Second, 30 * time.Second},
		UserCenter:  UserCenterConfig{30 * 24 * time.Hour, 24 * time.Hour},
		Redis:       RedisConfig{"127.0.0.1:6379", "", 0},
		Mongo:       MongoConfig{"mongodb://127.0.0.1:27017", "game01", 5 * time.Second, "", "", "", ""},
		Discovery:   DiscoveryConfig{"static", []string{"http://127.0.0.1:2379"}, "/services/game01", 10},
		IDGenerator: IDGeneratorConfig{1},
		Services:    map[string]ServiceConfig{"gateway": {true}, "usercenter": {true}, "lobby": {true}, "battle": {true}},
		Battle:      BattleConfig{20, 8},
	}
}

func PathFromFlags() string {
	p := flag.String("config", "./config/config.local.yaml", "configuration file path")
	flag.Parse()
	return *p
}

func Load(path string) (Config, error) {
	c := Defaults()
	b, e := os.ReadFile(path)
	if e != nil {
		return c, fmt.Errorf("read config: %w", e)
	}
	if e = yaml.Unmarshal(b, &c); e != nil {
		return c, fmt.Errorf("parse config: %w", e)
	}
	applyEnv(&c)
	return c, Validate(c)
}

func Validate(c Config) error {
	if c.HTTP.Address == "" {
		return errors.New("http.address is required")
	}
	if c.GRPC.ListenAddress == "" || c.GRPC.AdvertiseAddress == "" {
		return errors.New("grpc.listen_address and grpc.advertise_address are required")
	}
	if c.App.ShutdownTimeout <= 0 {
		return errors.New("app.shutdown_timeout must be positive")
	}
	if c.App.InstanceID == "" {
		return errors.New("app.instance_id is required")
	}
	if c.Gateway.SessionTTL <= 0 || c.Gateway.ReconnectGrace <= 0 || c.Gateway.HeartbeatInterval <= 0 || c.Gateway.HeartbeatTimeout <= 0 {
		return errors.New("gateway session and heartbeat durations must be positive")
	}
	if c.UserCenter.RefreshTokenTTL <= 0 || c.UserCenter.IdempotencyTTL <= 0 {
		return errors.New("usercenter.refresh_token_ttl and usercenter.idempotency_ttl must be positive")
	}
	if c.Redis.Address == "" {
		return errors.New("redis.address is required")
	}
	if c.Mongo.URI == "" || c.Mongo.Database == "" {
		return errors.New("mongo.uri and mongo.database are required")
	}
	if c.Mongo.ConnectTimeout <= 0 {
		return errors.New("mongo.connect_timeout must be positive")
	}
	if c.Discovery.Provider != "static" && c.Discovery.Provider != "etcd" {
		return errors.New("discovery.provider must be static or etcd")
	}
	if c.Discovery.Provider == "etcd" && len(c.Discovery.Endpoints) == 0 {
		return errors.New("discovery.endpoints is required for etcd")
	}
	if c.IDGenerator.NodeID > 1023 {
		return errors.New("id_generator.node_id must be between 0 and 1023")
	}
	if c.Battle.TickRate <= 0 || c.Battle.TickRate > 120 {
		return errors.New("battle.tick_rate must be between 1 and 120")
	}
	if c.Battle.MaxPlayersPerRoom <= 0 {
		return errors.New("battle.max_players_per_room must be positive")
	}
	return nil
}

func applyEnv(c *Config) {
	if v := os.Getenv("GAME_HTTP_ADDRESS"); v != "" {
		c.HTTP.Address = v
	}
	if v := os.Getenv("GAME_GRPC_LISTEN_ADDRESS"); v != "" {
		c.GRPC.ListenAddress = v
	}
	if v := os.Getenv("GAME_GRPC_ADVERTISE_ADDRESS"); v != "" {
		c.GRPC.AdvertiseAddress = v
	}
	if v := os.Getenv("GAME_ENVIRONMENT"); v != "" {
		c.App.Environment = v
	}
	if v := os.Getenv("GAME_INSTANCE_ID"); v != "" {
		c.App.InstanceID = v
	}
	if v := os.Getenv("GAME_ID_NODE_ID"); v != "" {
		if nodeID, err := strconv.ParseUint(v, 10, 16); err == nil {
			c.IDGenerator.NodeID = uint16(nodeID)
		}
	}
	if v := os.Getenv("GAME_BATTLE_TICK_RATE"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			c.Battle.TickRate = n
		}
	}
	if v := os.Getenv("GAME_REDIS_ADDR"); v != "" {
		c.Redis.Address = v
	}
	if v := os.Getenv("GAME_REDIS_PASSWORD"); v != "" {
		c.Redis.Password = v
	}
	if v := os.Getenv("GAME_MONGO_URI"); v != "" {
		c.Mongo.URI = v
	}
	if v := os.Getenv("GAME_MONGO_DATABASE"); v != "" {
		c.Mongo.Database = v
	}
	if v := os.Getenv("GAME_MONGO_AUTH_MECHANISM"); v != "" {
		c.Mongo.AuthMechanism = v
	}
	if v := os.Getenv("GAME_MONGO_AUTH_SOURCE"); v != "" {
		c.Mongo.AuthSource = v
	}
	if v := os.Getenv("GAME_MONGO_USERNAME"); v != "" {
		c.Mongo.Username = v
	}
	if v := os.Getenv("GAME_MONGO_PASSWORD"); v != "" {
		c.Mongo.Password = v
	}
	if v := os.Getenv("GAME_DISCOVERY_PROVIDER"); v != "" {
		c.Discovery.Provider = v
	}
	if v := os.Getenv("GAME_DISCOVERY_ENDPOINTS"); v != "" {
		c.Discovery.Endpoints = strings.Split(v, ",")
	}
}
