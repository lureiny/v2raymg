package tools

import (
	"testing"
)

func TestGitHubReleaseClient_New(t *testing.T) {
	client := NewGitHubReleaseClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.BaseURL == "" {
		t.Error("expected non-empty BaseURL")
	}
}

func TestNormalizeTag(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "latest"},
		{"v1.8.4", "1.8.4"},
		{"1.8.4", "1.8.4"},
		{"  latest  ", "latest"},
		{"v2.0.0", "2.0.0"},
	}

	for _, tt := range tests {
		result := NormalizeTag(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeTag(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
