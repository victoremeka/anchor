package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

type createAPIKeyResponse struct {
	KeyID  uuid.UUID `json:"key_id"`
	APIKey string    `json:"api_key"`
}

// Mints a new key for the caller's org without touching any existing one. 
func (h *Handler) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID, apiKey, err := h.engine.CreateAPIKey(r.Context(), orgIDFromContext(r.Context()))
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createAPIKeyResponse{KeyID: keyID, APIKey: apiKey})
}

type apiKeyInfoResponse struct {
	KeyID     uuid.UUID  `json:"key_id"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type listAPIKeysResponse struct {
	Keys []apiKeyInfoResponse `json:"keys"`
}


func (h *Handler) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.engine.ListAPIKeys(r.Context(), orgIDFromContext(r.Context()))
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	out := make([]apiKeyInfoResponse, len(keys))
	for i, k := range keys {
		out[i] = apiKeyInfoResponse{KeyID: k.KeyID, CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt}
	}
	writeJSON(w, http.StatusOK, listAPIKeysResponse{Keys: out})
}

func (h *Handler) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID, err := uuid.Parse(r.PathValue("key_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadPathValue("key_id"))
		return
	}
	if err := h.engine.RevokeAPIKey(r.Context(), orgIDFromContext(r.Context()), keyID); err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
