package main

import (
	"testing"

	"server/common/app"
	"server/common/config"
)

func TestEnabledModulesOnlyConstructsConfiguredServices(t *testing.T) {
	cfg := config.Defaults()
	cfg.Services = map[string]config.ServiceConfig{
		"gateway":    {Enabled: true},
		"usercenter": {Enabled: false},
		"lobby":      {Enabled: false},
		"battle":     {Enabled: false},
	}
	modules := enabledModules(app.Dependencies{Config: cfg})
	if len(modules) != 1 || modules[0].Name() != "gateway" {
		t.Fatalf("modules = %v, want only gateway", moduleNames(modules))
	}
}

func TestEnabledModulesPreservesServiceOrder(t *testing.T) {
	cfg := config.Defaults()
	modules := enabledModules(app.Dependencies{Config: cfg})
	want := []string{"usercenter", "lobby", "battle", "gateway"}
	got := moduleNames(modules)
	if len(got) != len(want) {
		t.Fatalf("modules = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("modules = %v, want %v", got, want)
		}
	}
}

func moduleNames(modules []app.Module) []string {
	names := make([]string, 0, len(modules))
	for _, module := range modules {
		names = append(names, module.Name())
	}
	return names
}
