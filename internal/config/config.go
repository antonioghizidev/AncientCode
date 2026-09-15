// Package config loads and validates the AncientAI configuration.
//
// Security contract (SPEC §8 and §5.4):
//   - The configuration NEVER holds an API key value — only the NAME of the
//     environment variable that holds it (api_key_env). The key is read from
//     the environment at request time by the provider.
//   - There is no way to disable TLS verification through configuration.
//     base_url must be https, except loopback addresses (self-hosted LiteLLM
//     running on the same machine).
//   - Every security option (run_user, shell_tool, approval) has a deny-by-
//     default value; relaxing them always requires an explicit configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ProviderOpenRouter = "openrouter"
	ProviderNVIDIA     = "nvidia"
	ProviderLiteLLM    = "litellm"
)

var defaultBaseURLs = map[string]string{
	ProviderOpenRouter: "https://openrouter.ai/api/v1",
	ProviderNVIDIA:     "https://integrate.api.nvidia.com/v1",
	ProviderLiteLLM:    "http://127.0.0.1:4000/v1",
}

var defaultAPIKeyEnv = map[string]string{
	ProviderOpenRouter: "OPENROUTER_API_KEY",
	ProviderNVIDIA:     "NVIDIA_API_KEY",
	ProviderLiteLLM:    "LITELLM_API_KEY",
}

var defaultToolTimeouts = map[string]string{
	"docker": "60s",
	"apt":    "180s",
	"shell":  "30s",
}

type Approval struct {
	Default   string `yaml:"default"`
	ReadOnly  string `yaml:"read_only"`
	WriteFile string `yaml:"write_file"`
}

type Config struct {
	Provider  string `yaml:"provider"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
	Model     string `yaml:"model"`

	Workdir        string            `yaml:"workdir"`
	CodeRoot       string            `yaml:"code_root"`
	RunUser        string            `yaml:"run_user"`
	ShellTool      string            `yaml:"shell_tool"`
	ToolboxMode    string            `yaml:"toolbox_mode"`
	ToolTimeouts   map[string]string `yaml:"tool_timeouts"`
	OutputMaxBytes int               `yaml:"output_max_bytes"`

	Approval Approval `yaml:"approval"`

	JSONModeFallback string `yaml:"json_mode_fallback"`
}

// Load reads the optional YAML file at path, applies defaults and environment
// overrides, and validates the final configuration. A missing file is not an
// error — the configuration is then built from defaults + environment.
func Load(path string) (*Config, error) {
	cfg := &Config{}

	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("config: invalid yaml in %s: %w", path, err)
			}
		case errors.Is(err, os.ErrNotExist):
			// missing file is fine
		default:
			return nil, fmt.Errorf("config: cannot read %s: %w", path, err)
		}
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyDefaults(c *Config) {
	if c.Provider == "" {
		c.Provider = ProviderOpenRouter
	}
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURLs[c.Provider]
	}
	if c.APIKeyEnv == "" {
		c.APIKeyEnv = defaultAPIKeyEnv[c.Provider]
	}
	if c.Workdir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		c.Workdir = filepath.Join(home, "ancientai")
	}
	if c.CodeRoot == "" {
		c.CodeRoot = filepath.Join(c.Workdir, "code")
	}
	if c.RunUser == "" {
		c.RunUser = "ancientai"
	}
	if c.ShellTool == "" {
		c.ShellTool = "disabled"
	}
	if c.ToolboxMode == "" {
		c.ToolboxMode = "plan"
	}
	if c.OutputMaxBytes <= 0 {
		c.OutputMaxBytes = 65536
	}
	if c.ToolTimeouts == nil {
		c.ToolTimeouts = make(map[string]string, len(defaultToolTimeouts))
		for k, v := range defaultToolTimeouts {
			c.ToolTimeouts[k] = v
		}
	}
	if c.Approval.Default == "" {
		c.Approval.Default = "ask"
	}
	if c.Approval.ReadOnly == "" {
		c.Approval.ReadOnly = "allow"
	}
	if c.Approval.WriteFile == "" {
		c.Approval.WriteFile = "ask"
	}
	if c.JSONModeFallback == "" {
		c.JSONModeFallback = "auto"
	}
}

func applyEnvOverrides(c *Config) {
	if v := os.Getenv("ANCIENTAI_PROVIDER"); v != "" {
		c.Provider = v
	}
	if v := os.Getenv("ANCIENTAI_BASE_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("ANCIENTAI_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("ANCIENTAI_WORKDIR"); v != "" {
		c.Workdir = v
	}
}

// Validate enforces every security rule that can be derived from the
// configuration alone. Failures are deny-by-default.
func (c *Config) Validate() error {
	switch c.Provider {
	case ProviderOpenRouter, ProviderNVIDIA, ProviderLiteLLM:
	default:
		return fmt.Errorf("config: unknown provider %q (allowed: %s, %s, %s)",
			c.Provider, ProviderOpenRouter, ProviderNVIDIA, ProviderLiteLLM)
	}

	if strings.TrimSpace(c.Model) == "" {
		return errors.New("config: model is required (set `model` or ANCIENTAI_MODEL)")
	}

	if strings.TrimSpace(c.APIKeyEnv) == "" {
		return errors.New("config: api_key_env is required")
	}
	if os.Getenv(c.APIKeyEnv) == "" {
		return fmt.Errorf("config: environment variable %q (from api_key_env) is not set", c.APIKeyEnv)
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("config: invalid base_url %q: %w", c.BaseURL, err)
	}
	switch u.Scheme {
	case "https":
		// TLS verified by default in net/http; nothing to relax.
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("config: insecure base_url %q: plain http is only allowed for loopback hosts", c.BaseURL)
		}
	default:
		return fmt.Errorf("config: base_url scheme %q must be http(s)", u.Scheme)
	}

	if c.RunUser == "root" || c.RunUser == "" {
		return errors.New("config: run_user must be a non-root, non-empty account")
	}

	if c.ShellTool != "disabled" && c.ShellTool != "enabled" {
		return fmt.Errorf("config: shell_tool must be disabled or enabled, got %q", c.ShellTool)
	}
	if c.ShellTool == "enabled" && c.Approval.Default != "ask" {
		return errors.New("config: shell_tool=enabled requires approval.default=ask (SPEC §5.3)")
	}

	if c.ToolboxMode != "plan" && c.ToolboxMode != "run" {
		return fmt.Errorf("config: toolbox_mode must be plan or run, got %q", c.ToolboxMode)
	}

	for name, val := range c.Approval.approvalMap() {
		switch val {
		case "allow", "ask", "deny":
		default:
			return fmt.Errorf("config: approval.%s must be allow, ask or deny, got %q", name, val)
		}
	}

	switch c.JSONModeFallback {
	case "auto", "always", "never":
	default:
		return fmt.Errorf("config: json_mode_fallback must be auto, always or never, got %q", c.JSONModeFallback)
	}

	for name, dur := range c.ToolTimeouts {
		if _, err := time.ParseDuration(dur); err != nil {
			return fmt.Errorf("config: tool_timeouts.%s %q is not a valid duration: %w", name, dur, err)
		}
	}

	return nil
}

func (a Approval) approvalMap() map[string]string {
	return map[string]string{
		"default":    a.Default,
		"read_only":  a.ReadOnly,
		"write_file": a.WriteFile,
	}
}

func (c *Config) apiKey() string { return os.Getenv(c.APIKeyEnv) }

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ToolTimeout returns the configured timeout for a tool name as a duration.
// Unknown tools fall back to the shell default so no tool ever runs without a
// deadline.
func (c *Config) ToolTimeout(name string) (time.Duration, error) {
	raw, ok := c.ToolTimeouts[name]
	if !ok {
		if v, ok := defaultToolTimeouts[name]; ok {
			raw = v
		} else {
			raw = defaultToolTimeouts["shell"]
		}
	}
	return time.ParseDuration(raw)
}
