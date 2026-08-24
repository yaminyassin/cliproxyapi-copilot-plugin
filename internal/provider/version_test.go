package provider

import "testing"

func TestUserAgentUsesPluginVersion(t *testing.T) {
	t.Parallel()

	want := "CLIProxyAPI-Copilot-Plugin/" + PluginVersion
	if got := userAgent(); got != want {
		t.Fatalf("userAgent() = %q, want %q", got, want)
	}
}
