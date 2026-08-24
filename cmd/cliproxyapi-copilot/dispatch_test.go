package main

import (
	"slices"
	"testing"

	"github.com/arthur-sommer-etc/cliproxyapi-copilot-plugin/internal/provider"
)

func TestPluginRegistrationUsesProviderVersion(t *testing.T) {
	t.Parallel()

	registration := pluginRegistration()
	if got, want := registration.Metadata.Version, provider.PluginVersion; got != want {
		t.Fatalf("registration version = %q, want %q", got, want)
	}
	if !registration.Capabilities.ModelProvider || !registration.Capabilities.AuthProvider || !registration.Capabilities.Executor {
		t.Fatalf("registration capabilities = %#v, want model provider, auth provider, and executor", registration.Capabilities)
	}
	if !slices.Contains(registration.Capabilities.ExecutorInputFormats, "chat-completions") {
		t.Fatalf("executor input formats = %v, want chat-completions", registration.Capabilities.ExecutorInputFormats)
	}
	if !slices.Contains(registration.Capabilities.ExecutorOutputFormats, "chat-completions") {
		t.Fatalf("executor output formats = %v, want chat-completions", registration.Capabilities.ExecutorOutputFormats)
	}
}
