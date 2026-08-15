package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

type createTaskRequest struct {
	AgentPool string          `json:"agent_pool"`
	Payload   json.RawMessage `json:"payload"`
	DedupKey  *string         `json:"dedup_key,omitempty"`
}

type createTaskResponse struct {
	TaskID uuid.UUID `json:"task_id"`
}

func (h *Handler) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.AgentPool == "" {
		writeError(w, http.StatusBadRequest, errMissingField("agent_pool"))
		return
	}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage(`{}`)
	}

	id, err := h.engine.CreateTask(r.Context(), orgIDFromContext(r.Context()), req.AgentPool, req.Payload, req.DedupKey)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createTaskResponse{TaskID: id})
}

type claimTaskRequest struct {
	AgentPool string    `json:"agent_pool"`
	WorkerID  uuid.UUID `json:"worker_id"`
}

type taskResponse struct {
	TaskID    uuid.UUID       `json:"task_id"`
	AgentPool string          `json:"agent_pool"`
	Payload   json.RawMessage `json:"payload"`
	ClaimedBy uuid.UUID       `json:"claimed_by"`
}

func (h *Handler) handleClaimTask(w http.ResponseWriter, r *http.Request) {
	var req claimTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.AgentPool == "" {
		writeError(w, http.StatusBadRequest, errMissingField("agent_pool"))
		return
	}
	if req.WorkerID == uuid.Nil {
		writeError(w, http.StatusBadRequest, errMissingField("worker_id"))
		return
	}

	task, err := h.engine.ClaimTask(r.Context(), orgIDFromContext(r.Context()), req.AgentPool, req.WorkerID)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, taskResponse{
		TaskID:    task.ID,
		AgentPool: task.AgentPool,
		Payload:   task.Payload,
		ClaimedBy: task.ClaimedBy,
	})
}

type completeTaskRequest struct {
	ClaimedBy uuid.UUID       `json:"claimed_by"`
	Result    json.RawMessage `json:"result"`
}

func (h *Handler) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(r.PathValue("task_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadPathValue("task_id"))
		return
	}
	var req completeTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.ClaimedBy == uuid.Nil {
		writeError(w, http.StatusBadRequest, errMissingField("claimed_by"))
		return
	}
	if len(req.Result) == 0 {
		req.Result = json.RawMessage(`{}`)
	}

	ok, err := h.engine.CompleteTask(r.Context(), orgIDFromContext(r.Context()), taskID, req.ClaimedBy, req.Result)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: ok})
}
