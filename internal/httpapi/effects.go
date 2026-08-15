package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"anchor/internal/engine"
)

type reserveEffectRequest struct {
	ToolName string `json:"tool_name"`
	CallKey  string `json:"call_key,omitempty"`
}

type reserveEffectResponse struct {
	IdempotencyKey string                `json:"idempotency_key"`
	Decision       engine.EffectDecision `json:"decision"`
	Result         json.RawMessage       `json:"result,omitempty"`
}

func (h *Handler) handleReserveEffect(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(r.PathValue("task_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadPathValue("task_id"))
		return
	}
	var req reserveEffectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.ToolName == "" {
		writeError(w, http.StatusBadRequest, errMissingField("tool_name"))
		return
	}

	key, decision, result, err := h.engine.ReserveEffect(r.Context(), orgIDFromContext(r.Context()), taskID, req.ToolName, req.CallKey)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reserveEffectResponse{IdempotencyKey: key, Decision: decision, Result: result})
}

type completeEffectRequest struct {
	Result json.RawMessage `json:"result"`
}

func (h *Handler) handleCompleteEffect(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("idempotency_key")
	if key == "" {
		writeError(w, http.StatusBadRequest, errBadPathValue("idempotency_key"))
		return
	}
	var req completeEffectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if len(req.Result) == 0 {
		req.Result = json.RawMessage(`{}`)
	}

	if err := h.engine.CompleteEffect(r.Context(), orgIDFromContext(r.Context()), key, req.Result); err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
