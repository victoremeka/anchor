package registry

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

var ErrTableNotRegistered = errors.New("linked table not registered")

// LookupFunc fetches the current value of a row by id. found=false means
// the row no longer exists.
type LookupFunc func(ctx context.Context, id string) (value json.RawMessage, found bool, err error)

type Registry struct {
	mu     sync.RWMutex
	lookup map[string]LookupFunc
}

func New() *Registry {
	return &Registry{lookup: make(map[string]LookupFunc)}
}

func (r *Registry) Register(table string, fn LookupFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookup[table] = fn
}

func (r *Registry) IsRegistered(table string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.lookup[table]
	return ok
}

func (r *Registry) Lookup(ctx context.Context, table, id string) (json.RawMessage, bool, error) {
	r.mu.RLock()
	fn, ok := r.lookup[table]
	r.mu.RUnlock()
	if !ok {
		return nil, false, ErrTableNotRegistered
	}
	return fn(ctx, id)
}
