package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

const embeddingDim = 1536

type rememberRequest struct {
	AgentID     uuid.UUID `json:"agent_id"`
	SubjectID   string    `json:"subject_id"`
	Content     string    `json:"content"`
	Embedding   []float32 `json:"embedding"`
	LinkedTable *string   `json:"linked_table,omitempty"`
	LinkedID    *string   `json:"linked_id,omitempty"`
}

type rememberResponse struct {
	MemoryID uuid.UUID `json:"memory_id"`
}

func (h *Handler) handleRemember(w http.ResponseWriter, r *http.Request) {
	var req rememberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.AgentID == uuid.Nil {
		writeError(w, http.StatusBadRequest, errMissingField("agent_id"))
		return
	}
	if req.SubjectID == "" {
		writeError(w, http.StatusBadRequest, errMissingField("subject_id"))
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, errMissingField("content"))
		return
	}
	if len(req.Embedding) != embeddingDim {
		writeError(w, http.StatusBadRequest, fmt.Errorf("embedding must have %d dimensions, got %d", embeddingDim, len(req.Embedding)))
		return
	}

	id, err := h.engine.Remember(r.Context(), orgIDFromContext(r.Context()), req.AgentID, req.SubjectID, req.Content, req.Embedding, req.LinkedTable, req.LinkedID)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rememberResponse{MemoryID: id})
}

type recallRequest struct {
	AgentID        uuid.UUID `json:"agent_id"`
	SubjectID      string    `json:"subject_id"`
	QueryEmbedding []float32 `json:"query_embedding"`
	TopK           int       `json:"top_k,omitempty"`
}

type recalledMemoryResponse struct {
	MemoryID          uuid.UUID       `json:"memory_id"`
	Content           string          `json:"content"`
	FreshnessVerified bool            `json:"freshness_verified"`
	LiveValue         json.RawMessage `json:"live_value,omitempty"`
}

type recallResponse struct {
	Results []recalledMemoryResponse `json:"results"`
}

func (h *Handler) handleRecall(w http.ResponseWriter, r *http.Request) {
	var req recallRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.AgentID == uuid.Nil {
		writeError(w, http.StatusBadRequest, errMissingField("agent_id"))
		return
	}
	if req.SubjectID == "" {
		writeError(w, http.StatusBadRequest, errMissingField("subject_id"))
		return
	}
	if len(req.QueryEmbedding) != embeddingDim {
		writeError(w, http.StatusBadRequest, fmt.Errorf("query_embedding must have %d dimensions, got %d", embeddingDim, len(req.QueryEmbedding)))
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}

	results, err := h.engine.Recall(r.Context(), orgIDFromContext(r.Context()), req.AgentID, req.SubjectID, req.QueryEmbedding, req.TopK)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	out := make([]recalledMemoryResponse, len(results))
	for i, m := range results {
		out[i] = recalledMemoryResponse{
			MemoryID:          m.MemoryID,
			Content:           m.Content,
			FreshnessVerified: m.FreshnessVerified,
			LiveValue:         m.LiveValue,
		}
	}
	writeJSON(w, http.StatusOK, recallResponse{Results: out})
}

func (h *Handler) handleForget(w http.ResponseWriter, r *http.Request) {
	memoryID, err := uuid.Parse(r.PathValue("memory_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadPathValue("memory_id"))
		return
	}
	if err := h.engine.Forget(r.Context(), orgIDFromContext(r.Context()), memoryID); err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
