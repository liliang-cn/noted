package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "noted.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsNeedNoFile(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.AI.Enabled || c.Server.Listen == "" || c.DBPath() != filepath.Join("data", "noted.db") {
		t.Fatalf("%+v", c)
	}
}

func TestAIEnabledRequiresLLM(t *testing.T) {
	_, err := Load(write(t, "[ai]\nenabled = true\n"))
	if err == nil || !strings.Contains(err.Error(), "ai.llm") {
		t.Fatalf("want a helpful error, got %v", err)
	}
}

func TestEmbeddingNeedsBothFields(t *testing.T) {
	_, err := Load(write(t, "[ai.embedding]\nmodel = \"m\"\n"))
	if err == nil {
		t.Fatal("half-configured embedding should be rejected")
	}
}

func TestEnvOverridesAndKeyExpansion(t *testing.T) {
	t.Setenv("NOTED_LISTEN", "0.0.0.0:43511")
	t.Setenv("NOTED_AI_ENABLED", "true")
	t.Setenv("MY_KEY", "sk-test")
	p := write(t, `
[ai.llm]
base_url = "http://x/v1"
model = "m"
api_key = "${MY_KEY}"
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != "0.0.0.0:43511" || !c.AI.Enabled || c.AI.LLM.APIKey != "sk-test" {
		t.Fatalf("%+v", c)
	}
}

func TestBadValuesRejected(t *testing.T) {
	for _, body := range []string{
		"time_zone = \"Mars/Base\"\n",
		"[server]\ntls_cert = \"a.crt\"\n",
		"this is not toml\n",
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestExampleConfigLoads(t *testing.T) {
	if _, err := Load("../../noted.example.toml"); err != nil {
		t.Fatal(err)
	}
}

func TestLanguage(t *testing.T) {
	c, err := Load("")
	if err != nil || c.Language != "zh" {
		t.Fatalf("default language: %q %v", c.Language, err)
	}
	if _, err := Load(write(t, "language = \"fr\"\n")); err == nil {
		t.Fatal("an unsupported language should be rejected")
	}
	t.Setenv("NOTED_LANGUAGE", "en")
	if c, err := Load(""); err != nil || c.Language != "en" {
		t.Fatalf("env override: %q %v", c.Language, err)
	}
}
