package discovery

import "context"

type Registration struct {
	Service  string `json:"service"`
	Instance string `json:"instance_id"`
	Address  string `json:"address"`
	Version  string `json:"version,omitempty"`
	Weight   int    `json:"weight,omitempty"`
	Zone     string `json:"zone,omitempty"`
}

type EventType string

const (
	EventPut    EventType = "put"
	EventDelete EventType = "delete"
)

type Event struct {
	Type         EventType
	Registration Registration
}

type CloseFunc func() error

type Registry interface {
	Register(context.Context, Registration) (CloseFunc, error)
	Discover(context.Context, string) ([]Registration, error)
	Watch(context.Context, string) (<-chan Event, error)
}
