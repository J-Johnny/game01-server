package logging

import (
	"log/slog"
	"os"
)

func New(service string, instanceID string, environment string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler).With(
		slog.String("service", service),
		slog.String("instance_id", instanceID),
		slog.String("environment", environment),
	)
}
