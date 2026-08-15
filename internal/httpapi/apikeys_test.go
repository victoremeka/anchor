package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestHTTP_APIKey_RotateKeepsOldKeyWorking(t *testing.T) {
	org := newTestOrg(t)

	resp := org.do(t, http.MethodPost, "/api-keys", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create_api_key status = %d, want 201", resp.StatusCode)
	}
	var rotated struct {
		KeyID  uuid.UUID `json:"key_id"`
		APIKey string    `json:"api_key"`
	}
	decodeBody(t, resp, &rotated)
	if rotated.APIKey == org.apiKey {
		t.Fatalf("rotated key is identical to the original")
	}

	// Both the original and the rotated key must authenticate.
	resp = doJSON(t, rotated.APIKey, http.MethodPost, org.baseURL+"/agents", map[string]any{"name": "a", "pool": "p"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("authenticated call with rotated key: status = %d, want 201", resp.StatusCode)
	}
	resp = org.do(t, http.MethodPost, "/agents", map[string]any{"name": "a", "pool": "p"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("authenticated call with original key: status = %d, want 201", resp.StatusCode)
	}
}

func TestHTTP_APIKey_RevokeInvalidatesImmediately(t *testing.T) {
	org := newTestOrg(t)

	resp := org.do(t, http.MethodPost, "/api-keys", nil)
	var rotated struct {
		KeyID  uuid.UUID `json:"key_id"`
		APIKey string    `json:"api_key"`
	}
	decodeBody(t, resp, &rotated)

	resp = org.do(t, http.MethodDelete, "/api-keys/"+rotated.KeyID.String(), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", resp.StatusCode)
	}

	resp = doJSON(t, rotated.APIKey, http.MethodPost, org.baseURL+"/agents", map[string]any{"name": "a", "pool": "p"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("call with revoked key: status = %d, want 401", resp.StatusCode)
	}

	// The original key, never touched, must still work.
	resp = org.do(t, http.MethodPost, "/agents", map[string]any{"name": "a", "pool": "p"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("call with the untouched original key: status = %d, want 201", resp.StatusCode)
	}
}

func TestHTTP_APIKey_ListNeverExposesKeyMaterial(t *testing.T) {
	org := newTestOrg(t)
	org.do(t, http.MethodPost, "/api-keys", nil) // second key, so there's something to list

	resp := org.do(t, http.MethodGet, "/api-keys", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list_api_keys status = %d, want 200", resp.StatusCode)
	}

	var raw map[string]any
	decodeBody(t, resp, &raw)
	body, _ := raw["keys"].([]any)
	if len(body) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(body))
	}
	for _, entry := range body {
		fields, _ := entry.(map[string]any)
		if _, present := fields["api_key"]; present {
			t.Fatalf("list_api_keys response leaked a plaintext api_key field: %v", fields)
		}
		if _, present := fields["key_hash"]; present {
			t.Fatalf("list_api_keys response leaked a key_hash field: %v", fields)
		}
	}
}

func TestHTTP_APIKey_CannotRevokeLastActiveKey(t *testing.T) {
	org := newTestOrg(t)

	resp := org.do(t, http.MethodGet, "/api-keys", nil)
	var listed struct {
		Keys []struct {
			KeyID uuid.UUID `json:"key_id"`
		} `json:"keys"`
	}
	decodeBody(t, resp, &listed)
	if len(listed.Keys) != 1 {
		t.Fatalf("expected exactly 1 key right after create_organization, got %d", len(listed.Keys))
	}

	resp = org.do(t, http.MethodDelete, "/api-keys/"+listed.Keys[0].KeyID.String(), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("revoke last active key: status = %d, want 409", resp.StatusCode)
	}
}

func TestHTTP_APIKey_CrossOrgCannotRevoke(t *testing.T) {
	orgA := newTestOrg(t)
	orgB := newTestOrg(t)

	resp := orgA.do(t, http.MethodGet, "/api-keys", nil)
	var listed struct {
		Keys []struct {
			KeyID uuid.UUID `json:"key_id"`
		} `json:"keys"`
	}
	decodeBody(t, resp, &listed)

	resp = orgB.do(t, http.MethodDelete, "/api-keys/"+listed.Keys[0].KeyID.String(), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("org B revoking org A's key: status = %d, want 404", resp.StatusCode)
	}
}
