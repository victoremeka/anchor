package engine

import (
	"errors"
	"time"

	"anchor/internal/registry"
	"anchor/internal/store"
)

var (
	ErrNoTaskAvailable   = errors.New("no pending task available")
	ErrEffectReserveRace = errors.New("lost the race to reserve this effect")
	ErrEffectNotFound    = errors.New("no reserved effect found for idempotency key")
	ErrMemoryNotFound    = errors.New("memory not found")
	ErrTaskNotFound      = errors.New("task not found")
	ErrAgentNotFound     = errors.New("agent not found")
	ErrInvalidAPIKey     = errors.New("invalid api key")
	ErrAPIKeyNotFound    = errors.New("api key not found")
	ErrLastActiveKey     = errors.New("cannot revoke an organization's last active api key")
)

type Engine struct {
	store    *store.Store
	registry *registry.Registry
	leaseTTL time.Duration
}

func New(s *store.Store, r *registry.Registry, leaseTTL time.Duration) *Engine {
	if leaseTTL <= 0 {
		leaseTTL = 60 * time.Second
	}
	return &Engine{store: s, registry: r, leaseTTL: leaseTTL}
}
