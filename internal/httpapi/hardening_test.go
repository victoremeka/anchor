package httpapi_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestHTTP_BodyTooLarge_Rejected(t *testing.T) {
	org := newTestOrg(t)

	huge := strings.Repeat("x", 2<<20)
	body := `{"name":"a","pool":"` + huge + `"}`

	req, err := http.NewRequest(http.MethodPost, org.baseURL+"/agents", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+org.apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestHTTP_CreateOrganization_RateLimited(t *testing.T) {
	srv := newTestServer(t)

	var lastStatus int
	for i := 0; i < 4; i++ {
		resp := doJSON(t, "", http.MethodPost, srv.URL+"/organizations", map[string]any{"name": "org"})
		lastStatus = resp.StatusCode
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("4th create_organization in quick succession: status = %d, want 429", lastStatus)
	}
}
