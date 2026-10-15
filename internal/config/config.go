// Package config loads noted's TOML configuration. Every value has a working
// default, so `noted serve` runs with no file at all; AI stays off until
// [ai] enabled = true.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server   Server  `toml:"server"`
	Storage  Storage `toml:"storage"`
	Auth     Auth    `toml:"auth"`
	TimeZone string  `toml:"time_zone"` // default zone for briefings and AI date math
	Language string  `toml:"language"`  // "zh" or "en": language of the sentences the server writes (suggestions)
	Notify   Notify  `toml:"notify"`
	AI       AI      `toml:"ai"`
	Cortex   Cortex  `toml:"cortexdb"`
}

type Server struct {
	Listen     string `toml:"listen"`
	Reflection bool   `toml:"reflection"` // serve gRPC reflection (grpcurl, Postman)
	TLSCert    string `toml:"tls_cert"`
	TLSKey     string `toml:"tls_key"`
	// MCPListen serves the MCP tools over Streamable HTTP at /mcp (plus GET
	// /healthz) on this address, for remote clients that connect with a URL
	// and a bearer token. Empty: off. Plain HTTP; put TLS in front of it.
	MCPListen string `toml:"mcp_listen"`
	// PublicURL is how clients reach the mcp_listen port from outside, e.g.
	// "https://noted.example.com": the base of the OAuth metadata MCP clients
	// sign in with, and of the server address in the App's pairing QR code.
	// Empty: taken from each request (X-Forwarded-Proto and the Host header).
	PublicURL string `toml:"public_url"`
}

type Storage struct {
	DataDir string `toml:"data_dir"`
}

type Auth struct {
	// Disabled runs single-user without tokens: every caller is the user
	// "default". Only sensible on localhost or behind another auth layer.
	Disabled bool `toml:"disabled"`
	// Signup lets anyone create an account with an email + password (gRPC
	// SignUp and the web portal). Off by default: on a private server, add an
	// email to an existing account instead (SetEmailPassword).
	Signup bool  `toml:"signup"`
	Apple  Apple `toml:"apple"`
}

// Apple turns on Sign in with Apple for the iOS app.
type Apple struct {
	// BundleID is the app's bundle identifier (the token's audience). Empty: off.
	BundleID string `toml:"bundle_id"`
	// Signup lets an Apple ID nobody has linked create its own user. Leave it
	// off on a public server: otherwise anyone with an Apple ID gets an account.
	Signup bool `toml:"signup"`
}

type Notify struct {
	// WebhookURL receives a JSON POST for every fired reminder.
	WebhookURL string `toml:"webhook_url"`
}

type AI struct {
	Enabled   bool     `toml:"enabled"`
	LLM       Endpoint `toml:"llm"`
	Embedding Endpoint `toml:"embedding"`
}

// Endpoint is any OpenAI-compatible API.
type Endpoint struct {
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
	Model   string `toml:"model"`
}

func (e Endpoint) Configured() bool { return e.BaseURL != "" && e.Model != "" }

// Cortex is the one-way sync of chosen users' items into a CortexDB brain
// over gRPC. It runs only with both an endpoint and at least one user.
type Cortex struct {
	// Remote is host:port of the CortexDB gRPC server. Its token is read from
	// $CORTEXDB_GRPC_TOKEN, never from this file.
	Remote string `toml:"remote"`
	TLS    bool   `toml:"tls"`
	// SyncUsers are the user names whose items are sent. Nobody else's are read.
	SyncUsers []string `toml:"sync_users"`
	// Interval between runs, a Go duration such as "15m". Default 15m, at least 1m.
	Interval string `toml:"interval"`
}

// CortexTokenEnv holds the CortexDB bearer token.
const CortexTokenEnv = "CORTEXDB_GRPC_TOKEN"

// Enabled reports whether the sync should run.
func (c Cortex) Enabled() bool { return c.Remote != "" && len(c.SyncUsers) > 0 }

// Every is the interval between runs.
func (c Cortex) Every() time.Duration {
	d, err := time.ParseDuration(c.Interval)
	if err != nil || c.Interval == "" {
		return 15 * time.Minute
	}
	return d
}

// Token is the bearer token from the environment. Never log it.
func (c Cortex) Token() string { return strings.TrimSpace(os.Getenv(CortexTokenEnv)) }

func Default() Config {
	return Config{
		Server:   Server{Listen: "127.0.0.1:43872", Reflection: true},
		Storage:  Storage{DataDir: "./data"},
		TimeZone: "UTC",
		Language: "zh",
	}
}

// Load reads path (skipped when empty or missing-by-default), then applies
// NOTED_* environment overrides, then validates.
func Load(path string) (Config, error) {
	c := Default()
	if path != "" {
		if _, err := toml.DecodeFile(path, &c); err != nil {
			return Config{}, fmt.Errorf("read %s: %w", path, err)
		}
	}
	applyEnv(&c)
	c.AI.LLM.APIKey = os.ExpandEnv(c.AI.LLM.APIKey)
	c.AI.Embedding.APIKey = os.ExpandEnv(c.AI.Embedding.APIKey)
	return c, c.Validate()
}

func applyEnv(c *Config) {
	str := func(dst *string, key string) {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	boolean := func(dst *bool, key string) {
		if v := os.Getenv(key); v != "" {
			*dst = v == "1" || strings.EqualFold(v, "true")
		}
	}
	str(&c.Server.Listen, "NOTED_LISTEN")
	str(&c.Server.MCPListen, "NOTED_MCP_LISTEN")
	str(&c.Server.PublicURL, "NOTED_PUBLIC_URL")
	str(&c.Storage.DataDir, "NOTED_DATA_DIR")
	str(&c.TimeZone, "NOTED_TIME_ZONE")
	str(&c.Language, "NOTED_LANGUAGE")
	str(&c.Notify.WebhookURL, "NOTED_WEBHOOK_URL")
	boolean(&c.Auth.Disabled, "NOTED_AUTH_DISABLED")
	boolean(&c.Auth.Signup, "NOTED_AUTH_SIGNUP")
	str(&c.Auth.Apple.BundleID, "NOTED_AUTH_APPLE_BUNDLE_ID")
	boolean(&c.Auth.Apple.Signup, "NOTED_AUTH_APPLE_SIGNUP")
	boolean(&c.AI.Enabled, "NOTED_AI_ENABLED")
	str(&c.AI.LLM.BaseURL, "NOTED_AI_LLM_BASE_URL")
	str(&c.AI.LLM.APIKey, "NOTED_AI_LLM_API_KEY")
	str(&c.AI.LLM.Model, "NOTED_AI_LLM_MODEL")
	str(&c.AI.Embedding.BaseURL, "NOTED_AI_EMBEDDING_BASE_URL")
	str(&c.AI.Embedding.APIKey, "NOTED_AI_EMBEDDING_API_KEY")
	str(&c.AI.Embedding.Model, "NOTED_AI_EMBEDDING_MODEL")
	str(&c.Cortex.Remote, "NOTED_CORTEXDB_REMOTE")
	str(&c.Cortex.Interval, "NOTED_CORTEXDB_INTERVAL")
	boolean(&c.Cortex.TLS, "NOTED_CORTEXDB_TLS")
	if v := os.Getenv("NOTED_CORTEXDB_SYNC_USERS"); v != "" {
		c.Cortex.SyncUsers = nil
		for _, u := range strings.Split(v, ",") {
			if u = strings.TrimSpace(u); u != "" {
				c.Cortex.SyncUsers = append(c.Cortex.SyncUsers, u)
			}
		}
	}
}

func (c Config) Validate() error {
	if c.Language != "zh" && c.Language != "en" {
		return fmt.Errorf("language %q: must be zh or en", c.Language)
	}
	if _, err := time.LoadLocation(c.TimeZone); err != nil {
		return fmt.Errorf("time_zone %q: %w", c.TimeZone, err)
	}
	if (c.Server.TLSCert == "") != (c.Server.TLSKey == "") {
		return fmt.Errorf("server.tls_cert and server.tls_key must be set together")
	}
	if u := c.Server.PublicURL; u != "" {
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" || p.RawQuery != "" || p.Fragment != "" ||
			strings.Trim(p.Path, "/") != "" {
			return fmt.Errorf("server.public_url %q: must be an http(s) origin such as https://noted.example.com", u)
		}
	}
	if c.AI.Enabled && !c.AI.LLM.Configured() {
		return fmt.Errorf("ai.enabled needs ai.llm.base_url and ai.llm.model (or turn AI off)")
	}
	if e := c.AI.Embedding; (e.BaseURL != "") != (e.Model != "") {
		return fmt.Errorf("ai.embedding needs both base_url and model")
	}
	if len(c.Cortex.SyncUsers) > 0 && c.Cortex.Remote == "" {
		return fmt.Errorf("cortexdb.sync_users needs cortexdb.remote")
	}
	if v := c.Cortex.Interval; v != "" {
		if d, err := time.ParseDuration(v); err != nil || d < time.Minute {
			return fmt.Errorf("cortexdb.interval %q: must be a duration of at least 1m, such as 15m", v)
		}
	}
	return nil
}

func (c Config) DBPath() string { return filepath.Join(c.Storage.DataDir, "noted.db") }

// AIDir holds the AI layer's own files (CortexDB index, agent state).
func (c Config) AIDir() string { return filepath.Join(c.Storage.DataDir, "ai") }

func (c Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}
