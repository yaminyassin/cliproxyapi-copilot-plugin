package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/arthur-sommer-etc/cliproxyapi-copilot-plugin/internal/translate"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestModelsForAuthRejectsMismatchedProvider(t *testing.T) {
	t.Parallel()

	_, err := New(nil).ModelsForAuth(context.Background(), "", pluginapi.AuthModelRequest{
		AuthProvider: "other-provider",
	})
	if err == nil {
		t.Fatal("ModelsForAuth() error = nil, want mismatched provider error")
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("ModelsForAuth() error = %T, want *StatusError", err)
	}
	if got, want := statusErr.Code, "unsupported_auth_provider"; got != want {
		t.Errorf("status code = %q, want %q", got, want)
	}
}

func TestSelectEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		model     upstreamModel
		want      string
		wantError bool
	}{
		{
			name:  "responses preferred",
			model: upstreamModel{ID: "model-a", SupportedEndpoints: []string{"/chat/completions", "/responses"}},
			want:  translate.EndpointResponses,
		},
		{
			name:  "messages fallback",
			model: upstreamModel{ID: "model-b", SupportedEndpoints: []string{"messages"}},
			want:  translate.EndpointMessages,
		},
		{
			name:  "sol forced to responses",
			model: upstreamModel{ID: "gpt-5.6-sol", SupportedEndpoints: []string{"/chat/completions"}},
			want:  translate.EndpointResponses,
		},
		{
			name:  "terra forced to responses",
			model: upstreamModel{ID: "GPT-5.6-TERRA"},
			want:  translate.EndpointResponses,
		},
		{
			name:      "unsupported",
			model:     upstreamModel{ID: "embedding-model", SupportedEndpoints: []string{"/embeddings"}},
			wantError: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := selectEndpoint(test.model)
			if test.wantError {
				if err == nil {
					t.Fatal("expected an endpoint selection error")
				}
				return
			}
			if err != nil {
				t.Fatalf("select endpoint: %v", err)
			}
			if got != test.want {
				t.Fatalf("endpoint = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeModelsAddsResponsesMetadata(t *testing.T) {
	t.Parallel()

	models := normalizeModels([]upstreamModel{
		{
			ID:                 "gpt-5.6-sol",
			SupportedEndpoints: []string{"/chat/completions"},
			Capabilities: modelCapabilities{
				Supports: modelSupports{Streaming: true, ToolCalls: true, Vision: true},
				Limits:   modelLimits{MaxPromptTokens: 100, MaxOutputTokens: 20},
			},
		},
	})
	if len(models) != 1 || !contains(models[0].SupportedEndpoints, translate.EndpointResponses) {
		t.Fatalf("responses endpoint was not added: %#v", models)
	}
	info := modelInfos(models)[0]
	if !contains(info.SupportedGenerationMethods, translate.EndpointResponses) {
		t.Fatalf("model metadata omits responses endpoint: %#v", info.SupportedGenerationMethods)
	}
	if !contains(info.SupportedInputModalities, "IMAGE") {
		t.Fatalf("model metadata omits image support: %#v", info.SupportedInputModalities)
	}
}

func TestFilterModelsExcludesConfiguredPrefixes(t *testing.T) {
	t.Parallel()

	models := filterModels([]upstreamModel{
		{ID: "gpt-5.6-sol"},
		{ID: "claude-sonnet-5"},
		{ID: "Claude-Haiku-4.5"},
	}, []string{"claude-"})
	if len(models) != 1 || models[0].ID != "gpt-5.6-sol" {
		t.Fatalf("filtered models = %#v", models)
	}
}

func TestNormalizeModelPrefixes(t *testing.T) {
	t.Parallel()

	got := normalizeModelPrefixes([]string{" Claude- ", "claude-", "", "GPT-"})
	if len(got) != 2 || got[0] != "claude-" || got[1] != "gpt-" {
		t.Fatalf("normalized prefixes = %#v", got)
	}
}
