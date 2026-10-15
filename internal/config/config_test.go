package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestMCPListen(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.MCPListen != "" {
		t.Fatalf("remote MCP must be off by default, got %q", c.Server.MCPListen)
	}
	c, err = Load(write(t, "[server]\nmcp_listen = \"127.0.0.1:43638\"\n"))
	if err != nil || c.Server.MCPListen != "127.0.0.1:43638" {
		t.Fatalf("%v %+v", err, c.Server)
	}
	t.Setenv("NOTED_MCP_LISTEN", "0.0.0.0:43639")
	if c, _ = Load(""); c.Server.MCPListen != "0.0.0.0:43639" {
		t.Fatalf("env override: %+v", c.Server)
	}
}

func TestPublicURL(t *testing.T) {
	c, err := Load(write(t, "[server]\npublic_url = \"https://noted.example.com/\"\n"))
	if err != nil || c.Server.PublicURL != "https://noted.example.com/" {
		t.Fatalf("%v %+v", err, c.Server)
	}
	for _, bad := range []string{"noted.example.com", "ftp://noted.example.com", "https://noted.example.com/sub", "https://x?y=1"} {
		if _, err := Load(write(t, "[server]\npublic_url = \""+bad+"\"\n")); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	t.Setenv("NOTED_PUBLIC_URL", "http://127.0.0.1:43638")
	if c, _ = Load(""); c.Server.PublicURL != "http://127.0.0.1:43638" {
		t.Fatalf("env override: %+v", c.Server)
	}
}

func TestCortexSyncIsOptIn(t *testing.T) {
	t.Setenv("CORTEXDB_REMOTE", "10.0.0.5:46017") // the shared brain's own variable is not read
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Cortex.Enabled() || c.Cortex.Remote != "" || c.Cortex.Every() != 15*time.Minute {
		t.Fatalf("sync must be off by default: %+v", c.Cortex)
	}
	c, err = Load(write(t, "[cortexdb]\nremote = \"10.0.0.5:46017\"\n"))
	if err != nil || c.Cortex.Enabled() {
		t.Fatalf("a remote without users syncs nothing: %v %+v", err, c.Cortex)
	}
	c, err = Load(write(t, "[cortexdb]\nremote = \"10.0.0.5:46017\"\nsync_users = [\"apple-b0d825f8\"]\ninterval = \"5m\"\n"))
	if err != nil || !c.Cortex.Enabled() || c.Cortex.Every() != 5*time.Minute || c.Cortex.SyncUsers[0] != "apple-b0d825f8" {
		t.Fatalf("%v %+v", err, c.Cortex)
	}
	for _, bad := range []string{
		"[cortexdb]\nsync_users = [\"a\"]\n",                           // users but nowhere to send them
		"[cortexdb]\nremote = \"h:46017\"\ninterval = \"10s\"\n",       // too often
		"[cortexdb]\nremote = \"h:46017\"\ninterval = \"fortnight\"\n", // not a duration
	} {
		if _, err := Load(write(t, bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestCortexEnv(t *testing.T) {
	t.Setenv("NOTED_CORTEXDB_REMOTE", "10.0.0.5:46017")
	t.Setenv("NOTED_CORTEXDB_SYNC_USERS", " apple-b0d825f8, ,bob ")
	t.Setenv("NOTED_CORTEXDB_INTERVAL", "30m")
	t.Setenv("CORTEXDB_GRPC_TOKEN", " secret ")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Cortex.Enabled() || len(c.Cortex.SyncUsers) != 2 || c.Cortex.SyncUsers[1] != "bob" || c.Cortex.Every() != 30*time.Minute || c.Cortex.Token() != "secret" {
		t.Fatalf("%+v", c.Cortex)
	}
}
