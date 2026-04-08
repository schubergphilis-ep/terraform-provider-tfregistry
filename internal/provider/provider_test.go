package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestValueOrEnv_ConfigValue(t *testing.T) {
	t.Setenv("TEST_VALUE_OR_ENV", "from-env")
	got := valueOrEnv(types.StringValue("from-config"), "TEST_VALUE_OR_ENV", "fallback")
	if got != "from-config" {
		t.Errorf("expected 'from-config', got %q", got)
	}
}

func TestValueOrEnv_EnvVar(t *testing.T) {
	t.Setenv("TEST_VALUE_OR_ENV", "from-env")
	got := valueOrEnv(types.StringNull(), "TEST_VALUE_OR_ENV", "fallback")
	if got != "from-env" {
		t.Errorf("expected 'from-env', got %q", got)
	}
}

func TestValueOrEnv_Fallback(t *testing.T) {
	t.Setenv("TEST_VALUE_OR_ENV", "")
	got := valueOrEnv(types.StringNull(), "TEST_VALUE_OR_ENV", "fallback")
	if got != "fallback" {
		t.Errorf("expected 'fallback', got %q", got)
	}
}

func TestValueOrEnv_EmptyConfigUsesEnv(t *testing.T) {
	t.Setenv("TEST_VALUE_OR_ENV", "from-env")
	got := valueOrEnv(types.StringValue(""), "TEST_VALUE_OR_ENV", "fallback")
	if got != "from-env" {
		t.Errorf("expected 'from-env', got %q", got)
	}
}

func TestValueOrEnv_UnknownUsesEnv(t *testing.T) {
	t.Setenv("TEST_VALUE_OR_ENV", "from-env")
	got := valueOrEnv(types.StringUnknown(), "TEST_VALUE_OR_ENV", "fallback")
	if got != "from-env" {
		t.Errorf("expected 'from-env', got %q", got)
	}
}

func TestGetTokenFromCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials.tfrc.json")
	content := `{"credentials":{"app.terraform.io":{"token":"my-secret-token"},"custom.host":{"token":"custom-token"}}}`
	if err := os.WriteFile(credFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Point TF_DATA_DIR to our temp dir so credentialsFilePath() finds it
	t.Setenv("TF_DATA_DIR", dir)

	tests := []struct {
		hostname string
		want     string
	}{
		{"app.terraform.io", "my-secret-token"},
		{"custom.host", "custom-token"},
		{"unknown.host", ""},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			got := getTokenFromCredentialsFile(tt.hostname)
			if got != tt.want {
				t.Errorf("getTokenFromCredentialsFile(%q) = %q, want %q", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestGetTokenFromCredentialsFile_MissingFile(t *testing.T) {
	t.Setenv("TF_DATA_DIR", t.TempDir())
	got := getTokenFromCredentialsFile("app.terraform.io")
	if got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
}

func TestGetTokenFromCredentialsFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials.tfrc.json")
	if err := os.WriteFile(credFile, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TF_DATA_DIR", dir)

	got := getTokenFromCredentialsFile("app.terraform.io")
	if got != "" {
		t.Errorf("expected empty string for invalid JSON, got %q", got)
	}
}

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
}
