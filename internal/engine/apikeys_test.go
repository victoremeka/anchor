package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"anchor/internal/engine"
)

func TestIntegration_APIKey_RotateKeepsOldKeyWorking(t *testing.T) {
	e := newTestEngine(t, time.Minute)
	ctx := context.Background()

	orgID, key1, err := e.CreateOrganization(ctx, t.Name())
	if err != nil {
		t.Fatalf("create_organization: %v", err)
	}

	_, key2, err := e.CreateAPIKey(ctx, orgID)
	if err != nil {
		t.Fatalf("create_api_key (rotate): %v", err)
	}
	if key2 == key1 {
		t.Fatalf("rotated key is identical to the original")
	}

	for name, key := range map[string]string{"original": key1, "rotated": key2} {
		got, err := e.AuthenticateAPIKey(ctx, key)
		if err != nil {
			t.Fatalf("authenticate (%s): %v", name, err)
		}
		if got != orgID {
			t.Fatalf("authenticate (%s) resolved to %s, want %s", name, got, orgID)
		}
	}
}

func TestIntegration_APIKey_RevokeInvalidatesImmediately(t *testing.T) {
	e := newTestEngine(t, time.Minute)
	ctx := context.Background()

	orgID, key1, err := e.CreateOrganization(ctx, t.Name())
	if err != nil {
		t.Fatalf("create_organization: %v", err)
	}
	keyID2, key2, err := e.CreateAPIKey(ctx, orgID)
	if err != nil {
		t.Fatalf("create_api_key: %v", err)
	}

	if err := e.RevokeAPIKey(ctx, orgID, keyID2); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := e.AuthenticateAPIKey(ctx, key2); !errors.Is(err, engine.ErrInvalidAPIKey) {
		t.Fatalf("authenticate with revoked key: err = %v, want ErrInvalidAPIKey", err)
	}
	if _, err := e.AuthenticateAPIKey(ctx, key1); err != nil {
		t.Fatalf("authenticate with the untouched original key: %v", err)
	}
}

func TestIntegration_APIKey_RevokeIsIdempotent(t *testing.T) {
	e := newTestEngine(t, time.Minute)
	ctx := context.Background()

	orgID, _, err := e.CreateOrganization(ctx, t.Name())
	if err != nil {
		t.Fatalf("create_organization: %v", err)
	}
	keyID2, _, err := e.CreateAPIKey(ctx, orgID)
	if err != nil {
		t.Fatalf("create_api_key: %v", err)
	}

	if err := e.RevokeAPIKey(ctx, orgID, keyID2); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if err := e.RevokeAPIKey(ctx, orgID, keyID2); err != nil {
		t.Fatalf("second revoke (should be a no-op success): %v", err)
	}
}

func TestIntegration_APIKey_CannotRevokeLastActiveKey(t *testing.T) {
	e := newTestEngine(t, time.Minute)
	ctx := context.Background()

	orgID, _, err := e.CreateOrganization(ctx, t.Name())
	if err != nil {
		t.Fatalf("create_organization: %v", err)
	}
	keys, err := e.ListAPIKeys(ctx, orgID)
	if err != nil {
		t.Fatalf("list_api_keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 key right after create_organization, got %d", len(keys))
	}

	if err := e.RevokeAPIKey(ctx, orgID, keys[0].KeyID); !errors.Is(err, engine.ErrLastActiveKey) {
		t.Fatalf("revoke last active key: err = %v, want ErrLastActiveKey", err)
	}

	// With a second active key, revoking the first must now succeed.
	if _, _, err := e.CreateAPIKey(ctx, orgID); err != nil {
		t.Fatalf("create_api_key: %v", err)
	}
	if err := e.RevokeAPIKey(ctx, orgID, keys[0].KeyID); err != nil {
		t.Fatalf("revoke with a second key present: %v", err)
	}
}

func TestIntegration_APIKey_CrossOrgCannotRevoke(t *testing.T) {
	e := newTestEngine(t, time.Minute)
	ctx := context.Background()

	orgA, _, err := e.CreateOrganization(ctx, t.Name()+"-a")
	if err != nil {
		t.Fatalf("create_organization (a): %v", err)
	}
	orgB, _, err := e.CreateOrganization(ctx, t.Name()+"-b")
	if err != nil {
		t.Fatalf("create_organization (b): %v", err)
	}
	keysA, err := e.ListAPIKeys(ctx, orgA)
	if err != nil {
		t.Fatalf("list_api_keys (a): %v", err)
	}

	if err := e.RevokeAPIKey(ctx, orgB, keysA[0].KeyID); !errors.Is(err, engine.ErrAPIKeyNotFound) {
		t.Fatalf("org B revoking org A's key: err = %v, want ErrAPIKeyNotFound", err)
	}
}

func TestIntegration_APIKey_ListReflectsRevocation(t *testing.T) {
	e := newTestEngine(t, time.Minute)
	ctx := context.Background()

	orgID, _, err := e.CreateOrganization(ctx, t.Name())
	if err != nil {
		t.Fatalf("create_organization: %v", err)
	}
	keyID2, _, err := e.CreateAPIKey(ctx, orgID)
	if err != nil {
		t.Fatalf("create_api_key: %v", err)
	}

	keys, err := e.ListAPIKeys(ctx, orgID)
	if err != nil {
		t.Fatalf("list_api_keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	for _, k := range keys {
		if k.CreatedAt.IsZero() {
			t.Fatalf("created_at was never set for key %s", k.KeyID)
		}
		if k.RevokedAt != nil {
			t.Fatalf("key %s already shows revoked before any revoke call", k.KeyID)
		}
	}

	if err := e.RevokeAPIKey(ctx, orgID, keyID2); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	keys, err = e.ListAPIKeys(ctx, orgID)
	if err != nil {
		t.Fatalf("list_api_keys after revoke: %v", err)
	}
	var sawRevoked bool
	for _, k := range keys {
		if k.KeyID == keyID2 {
			if k.RevokedAt == nil {
				t.Fatalf("revoked key %s still shows revoked_at = nil", keyID2)
			}
			sawRevoked = true
		}
	}
	if !sawRevoked {
		t.Fatalf("revoked key %s missing from list_api_keys entirely", keyID2)
	}
}
