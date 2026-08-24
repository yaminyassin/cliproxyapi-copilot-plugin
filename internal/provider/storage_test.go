package provider

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestAuthDataMetadataBridge(t *testing.T) {
	t.Parallel()

	t.Run("nil metadata gets canonical fields", func(t *testing.T) {
		data := testAuthData(t, authStorage{GitHubAccessToken: "token-one", GitHubLogin: "octocat"}, nil)

		want := map[string]any{
			"type":         providerID,
			"github_login": "octocat",
			"access_token": "token-one",
		}
		if !reflect.DeepEqual(data.Metadata, want) {
			t.Fatalf("metadata = %#v, want %#v", data.Metadata, want)
		}
	})

	t.Run("custom metadata is preserved", func(t *testing.T) {
		metadata := map[string]any{
			"quota_source":  "github",
			"account_index": 2,
			"nested":        map[string]any{"region": "us-east"},
		}
		data := testAuthData(t, authStorage{GitHubAccessToken: "token-one", GitHubLogin: "octocat"}, metadata)

		for key, want := range metadata {
			if got := data.Metadata[key]; !reflect.DeepEqual(got, want) {
				t.Errorf("metadata[%q] = %#v, want %#v", key, got, want)
			}
		}
	})

	t.Run("canonical fields override stale metadata", func(t *testing.T) {
		metadata := map[string]any{
			"type":         "stale-provider",
			"github_login": "stale-login",
			"access_token": "stale-token",
		}
		data := testAuthData(t, authStorage{GitHubAccessToken: "token-one", GitHubLogin: "octocat"}, metadata)

		if got, want := data.Metadata["type"], providerID; got != want {
			t.Errorf("metadata[type] = %#v, want %#v", got, want)
		}
		if got, want := data.Metadata["github_login"], "octocat"; got != want {
			t.Errorf("metadata[github_login] = %#v, want %#v", got, want)
		}
		if got, want := data.Metadata["access_token"], "token-one"; got != want {
			t.Errorf("metadata[access_token] = %#v, want %#v", got, want)
		}
	})

	t.Run("access token follows storage updates", func(t *testing.T) {
		metadata := map[string]any{"access_token": "stale-token"}
		storage := authStorage{GitHubAccessToken: "token-one", GitHubLogin: "octocat"}
		first := testAuthData(t, storage, metadata)
		storage.GitHubAccessToken = "token-two"
		second := testAuthData(t, storage, metadata)

		if got, want := first.Metadata["access_token"], "token-one"; got != want {
			t.Errorf("first metadata[access_token] = %#v, want %#v", got, want)
		}
		if got, want := second.Metadata["access_token"], "token-two"; got != want {
			t.Errorf("second metadata[access_token] = %#v, want %#v", got, want)
		}
	})

	t.Run("caller metadata is not mutated", func(t *testing.T) {
		metadata := map[string]any{
			"type":         "stale-provider",
			"github_login": "stale-login",
			"access_token": "stale-token",
			"custom":       "preserved",
		}
		before := cloneTestMetadata(metadata)
		data := testAuthData(t, authStorage{GitHubAccessToken: "token-one", GitHubLogin: "octocat"}, metadata)

		if !reflect.DeepEqual(metadata, before) {
			t.Fatalf("caller metadata mutated: got %#v, want %#v", metadata, before)
		}
		data.Metadata["custom"] = "changed in returned map"
		if got, want := metadata["custom"], "preserved"; got != want {
			t.Fatalf("caller metadata changed through returned map: got %#v, want %#v", got, want)
		}
	})
}

func TestAuthDataStorageJSONKeepsGitHubAccessToken(t *testing.T) {
	t.Parallel()

	data := testAuthData(t, authStorage{GitHubAccessToken: "token-one", GitHubLogin: "octocat"}, nil)
	var decoded map[string]any
	if err := json.Unmarshal(data.StorageJSON, &decoded); err != nil {
		t.Fatalf("decode storage JSON: %v", err)
	}
	if got, want := decoded["github_access_token"], "token-one"; got != want {
		t.Errorf("storage JSON github_access_token = %#v, want %#v", got, want)
	}
	if _, exists := decoded["access_token"]; exists {
		t.Fatal("storage JSON unexpectedly gained access_token")
	}
}

func TestAuthDataStorageContractKeepsRefreshMaterialOutOfMetadata(t *testing.T) {
	t.Parallel()

	data := testAuthData(t, authStorage{
		Type:                  providerID,
		GitHubAccessToken:     "access-token",
		GitHubRefreshToken:    "refresh-token",
		TokenType:             "bearer",
		Scope:                 "read:user",
		ExpiresAt:             1_700_000_000,
		RefreshTokenExpiresAt: 1_800_000_000,
		GitHubLogin:           "octocat",
		GitHubUserID:          123,
		OAuthClientID:         "client-id",
		UpdatedAt:             "2026-08-24T00:00:00Z",
	}, nil)

	var persisted map[string]any
	if err := json.Unmarshal(data.StorageJSON, &persisted); err != nil {
		t.Fatalf("decode storage JSON: %v", err)
	}
	for key, want := range map[string]any{
		"type":                     providerID,
		"github_access_token":      "access-token",
		"github_refresh_token":     "refresh-token",
		"token_type":               "bearer",
		"scope":                    "read:user",
		"expires_at":               float64(1_700_000_000),
		"refresh_token_expires_at": float64(1_800_000_000),
		"github_login":             "octocat",
		"github_user_id":           float64(123),
		"oauth_client_id":          "client-id",
		"updated_at":               "2026-08-24T00:00:00Z",
	} {
		if got := persisted[key]; got != want {
			t.Errorf("storage JSON field %q = %#v, want %#v", key, got, want)
		}
	}
	for _, key := range []string{"access_token", "refresh_token"} {
		if _, exists := persisted[key]; exists {
			t.Errorf("storage JSON unexpectedly contains generic %q", key)
		}
	}

	if got, want := data.Metadata["access_token"], "access-token"; got != want {
		t.Errorf("metadata access_token = %#v, want %#v", got, want)
	}
	for _, key := range []string{"github_access_token", "github_refresh_token", "refresh_token"} {
		if _, exists := data.Metadata[key]; exists {
			t.Errorf("metadata unexpectedly contains provider secret field %q", key)
		}
	}
}

func TestParseAuthContract(t *testing.T) {
	t.Parallel()

	raw, errMarshal := json.Marshal(authStorage{
		Type:               providerID,
		GitHubAccessToken:  "access-token",
		GitHubRefreshToken: "refresh-token",
		GitHubLogin:        "octocat",
		UpdatedAt:          "2026-08-24T00:00:00Z",
	})
	if errMarshal != nil {
		t.Fatalf("encode auth storage: %v", errMarshal)
	}

	tests := []struct {
		name        string
		request     pluginapi.AuthParseRequest
		wantHandled bool
		wantError   bool
	}{
		{
			name: "recognized copilot storage",
			request: pluginapi.AuthParseRequest{
				FileName: "copilot-octocat.json",
				RawJSON:  raw,
			},
			wantHandled: true,
		},
		{
			name: "provider mismatch is not handled",
			request: pluginapi.AuthParseRequest{
				Provider: "anthropic",
				RawJSON:  raw,
			},
		},
		{
			name: "other storage is not handled",
			request: pluginapi.AuthParseRequest{
				RawJSON: []byte(`{"type":"other","access_token":"access-token"}`),
			},
		},
		{
			name: "recognized malformed storage returns an error",
			request: pluginapi.AuthParseRequest{
				RawJSON: []byte(`{"type":"copilot"}`),
			},
			wantError: true,
		},
	}

	service := New(nil)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			response, errParse := service.ParseAuth(test.request)
			if (errParse != nil) != test.wantError {
				t.Fatalf("ParseAuth() error = %v, want error: %v", errParse, test.wantError)
			}
			if response.Handled != test.wantHandled {
				t.Fatalf("ParseAuth() handled = %v, want %v", response.Handled, test.wantHandled)
			}
			if !test.wantHandled || test.wantError {
				return
			}
			if response.Auth.Provider != providerID {
				t.Errorf("parsed provider = %q, want %q", response.Auth.Provider, providerID)
			}
			if got, want := response.Auth.Metadata["access_token"], "access-token"; got != want {
				t.Errorf("parsed metadata access_token = %#v, want %#v", got, want)
			}
			for _, key := range []string{"github_refresh_token", "refresh_token"} {
				if _, exists := response.Auth.Metadata[key]; exists {
					t.Errorf("parsed metadata unexpectedly contains %q", key)
				}
			}
		})
	}
}

func testAuthData(t *testing.T, storage authStorage, metadata map[string]any) pluginapi.AuthData {
	t.Helper()
	data, err := authData(storage, "auth-id", "auth.json", "", "", false, metadata, nil)
	if err != nil {
		t.Fatalf("authData: %v", err)
	}
	return data
}

func cloneTestMetadata(metadata map[string]any) map[string]any {
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
