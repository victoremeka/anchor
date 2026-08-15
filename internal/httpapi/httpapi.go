package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"anchor/internal/engine"
	"anchor/internal/registry"
)

const defaultMaxBodyBytes = 1 << 20

type Handler struct {
	engine       *engine.Engine
	log          *slog.Logger
	maxBodyBytes int64

	orgCreateLimiter *ipRateLimiter
}

type Option func(*Handler)

func WithMaxBodyBytes(n int64) Option {
	return func(h *Handler) { h.maxBodyBytes = n }
}

func New(e *engine.Engine, log *slog.Logger, opts ...Option) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{
		engine:           e,
		log:              log,
		maxBodyBytes:     defaultMaxBodyBytes,
		orgCreateLimiter: newIPRateLimiter(rate.Every(10*time.Second), 3),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Handler) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealthz)

	mux.HandleFunc("POST /organizations", rateLimit(h.orgCreateLimiter, h.handleCreateOrganization))

	mux.HandleFunc("POST /agents", h.requireAuth(h.handleCreateAgent))

	mux.HandleFunc("POST /api-keys", h.requireAuth(h.handleCreateAPIKey))
	mux.HandleFunc("GET /api-keys", h.requireAuth(h.handleListAPIKeys))
	mux.HandleFunc("DELETE /api-keys/{key_id}", h.requireAuth(h.handleRevokeAPIKey))

	mux.HandleFunc("POST /tasks", h.requireAuth(h.handleCreateTask))
	mux.HandleFunc("POST /tasks/claim", h.requireAuth(h.handleClaimTask))
	mux.HandleFunc("POST /tasks/{task_id}/complete", h.requireAuth(h.handleCompleteTask))
	mux.HandleFunc("POST /tasks/{task_id}/effects/reserve", h.requireAuth(h.handleReserveEffect))
	mux.HandleFunc("POST /effects/{idempotency_key}/complete", h.requireAuth(h.handleCompleteEffect))

	mux.HandleFunc("POST /memories", h.requireAuth(h.handleRemember))
	mux.HandleFunc("POST /memories/recall", h.requireAuth(h.handleRecall))
	mux.HandleFunc("DELETE /memories/{memory_id}", h.requireAuth(h.handleForget))

	return h.withLogging(h.limitBody(mux))
}

func (h *Handler) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (h *Handler) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		h.log.Info("http_request",
			"method", r.Method, "path", r.URL.Path, "status", sw.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(status int) {
	sw.status = status
	sw.ResponseWriter.WriteHeader(status)
}

type okResponse struct {
	OK bool `json:"ok"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorResponse{Error: err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	writeError(w, http.StatusBadRequest, err)
}

func errMissingField(name string) error { return fmt.Errorf("missing required field %q", name) }
func errBadPathValue(name string) error { return fmt.Errorf("invalid path value %q", name) }

func (h *Handler) writeEngineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrNoTaskAvailable):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, engine.ErrEffectReserveRace):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, engine.ErrEffectNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, engine.ErrMemoryNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, engine.ErrTaskNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, engine.ErrAgentNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, engine.ErrAPIKeyNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, engine.ErrLastActiveKey):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, registry.ErrTableNotRegistered):
		writeError(w, http.StatusBadRequest, err)
	default:
		h.log.Error("internal_error", "err", err)
		writeError(w, http.StatusInternalServerError, errors.New("internal error"))
	}
}
