package httpapi

import (
	"net/http"

	"github.com/google/uuid"
)

type createOrganizationRequest struct {
	Name string `json:"name"`
}

type createOrganizationResponse struct {
	OrgID  uuid.UUID `json:"org_id"`
	APIKey string    `json:"api_key"`
}

func (h *Handler) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req createOrganizationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, errMissingField("name"))
		return
	}

	orgID, apiKey, err := h.engine.CreateOrganization(r.Context(), req.Name)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createOrganizationResponse{OrgID: orgID, APIKey: apiKey})
}

type createAgentRequest struct {
	Name string `json:"name"`
	Pool string `json:"pool"`
}

type createAgentResponse struct {
	AgentID uuid.UUID `json:"agent_id"`
}

func (h *Handler) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, errMissingField("name"))
		return
	}
	if req.Pool == "" {
		writeError(w, http.StatusBadRequest, errMissingField("pool"))
		return
	}

	id, err := h.engine.CreateAgent(r.Context(), orgIDFromContext(r.Context()), req.Name, req.Pool)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createAgentResponse{AgentID: id})
}
