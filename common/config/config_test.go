package config

import "testing"

func TestLoadProductionConfig(t *testing.T) {
	config, err := Load("../../config/config.production.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.App.Environment != "production" {
		t.Fatalf("environment = %q, want production", config.App.Environment)
	}
	if config.Discovery.Provider != "etcd" {
		t.Fatalf("discovery provider = %q, want etcd", config.Discovery.Provider)
	}
	if config.Mongo.AuthMechanism != "SCRAM-SHA-256" || config.Mongo.AuthSource != "admin" {
		t.Fatalf("unexpected mongo authentication config: %+v", config.Mongo)
	}
}

func TestLoadAppliesMongoAuthenticationEnvironment(t *testing.T) {
	t.Setenv("GAME_MONGO_AUTH_MECHANISM", "SCRAM-SHA-256")
	t.Setenv("GAME_MONGO_AUTH_SOURCE", "admin")
	t.Setenv("GAME_MONGO_USERNAME", "game_user")
	t.Setenv("GAME_MONGO_PASSWORD", "game_password")

	config, err := Load("../../config/config.production.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Mongo.Username != "game_user" || config.Mongo.Password != "game_password" {
		t.Fatalf("mongo credentials were not applied: %+v", config.Mongo)
	}
}
