// Package config loads noted's TOML configuration. Every value has a working
// default, so `noted serve` runs with no file at all; AI stays off until
// [ai] enabled = true.
package config

import (
	"fmt"
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
	Notify   Notify  `toml:"notify"`
	AI       AI      `toml:"ai"`
}

type Server struct {
	Listen     string `toml:"listen"`
	Reflection bool   `toml:"reflection"` // serve gRPC reflection (grpcurl, Postman)
	TLSCert    string `toml:"tls_cert"`
	TLSKey     string `toml:"tls_key"`
}

type Storage struct {
	DataDir string `toml:"data_dir"`
}

type Auth struct {
	// Disabled runs single-user without tokens: every caller is the user
	// "default". Only sensible on localhost or behind another auth layer.
	Disabled bool `toml:"disabled"`
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

func Default() Config {
	return Config{
		Server:   Server{Listen: "127.0.0.1:43872", Reflection: true},
		Storage:  Storage{DataDir: "./data"},
		TimeZone: "UTC",
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
	str(&c.Storage.DataDir, "NOTED_DATA_DIR")
	str(&c.TimeZone, "NOTED_TIME_ZONE")
	str(&c.Notify.WebhookURL, "NOTED_WEBHOOK_URL")
	boolean(&c.Auth.Disabled, "NOTED_AUTH_DISABLED")
	boolean(&c.AI.Enabled, "NOTED_AI_ENABLED")
	str(&c.AI.LLM.BaseURL, "NOTED_AI_LLM_BASE_URL")
	str(&c.AI.LLM.APIKey, "NOTED_AI_LLM_API_KEY")
	str(&c.AI.LLM.Model, "NOTED_AI_LLM_MODEL")
	str(&c.AI.Embedding.BaseURL, "NOTED_AI_EMBEDDING_BASE_URL")
	str(&c.AI.Embedding.APIKey, "NOTED_AI_EMBEDDING_API_KEY")
	str(&c.AI.Embedding.Model, "NOTED_AI_EMBEDDING_MODEL")
}

func (c Config) Validate() error {
	if _, err := time.LoadLocation(c.TimeZone); err != nil {
		return fmt.Errorf("time_zone %q: %w", c.TimeZone, err)
	}
	if (c.Server.TLSCert == "") != (c.Server.TLSKey == "") {
		return fmt.Errorf("server.tls_cert and server.tls_key must be set together")
	}
	if c.AI.Enabled && !c.AI.LLM.Configured() {
		return fmt.Errorf("ai.enabled needs ai.llm.base_url and ai.llm.model (or turn AI off)")
	}
	if e := c.AI.Embedding; (e.BaseURL != "") != (e.Model != "") {
		return fmt.Errorf("ai.embedding needs both base_url and model")
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
