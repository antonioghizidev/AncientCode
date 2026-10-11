package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ancientcode.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadWithKey(t *testing.T, body string) (*Config, error) {
	t.Helper()
	t.Setenv("TEST_FAKE_KEY", "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456")
	t.Setenv(defaultAPIKeyEnv[ProviderOpenRouter], "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456")
	t.Setenv(defaultAPIKeyEnv[ProviderNVIDIA], "nvapi-abcdefGHIJKLMNOPQRSTUVWXYZ123456")
	t.Setenv(defaultAPIKeyEnv[ProviderLiteLLM], "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456")
	path := writeTempConfig(t, body)
	return Load(path)
}

func TestLoadDefaultsAndMinimalFile(t *testing.T) {
	cfg, err := loadWithKey(t, "model: some/model")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != ProviderOpenRouter {
		t.Errorf("default provider = %q", cfg.Provider)
	}
	if cfg.BaseURL != defaultBaseURLs[ProviderOpenRouter] {
		t.Errorf("default base_url = %q", cfg.BaseURL)
	}
	if cfg.APIKeyEnv != defaultAPIKeyEnv[ProviderOpenRouter] {
		t.Errorf("default api_key_env = %q", cfg.APIKeyEnv)
	}
	if cfg.Approval.Default != "ask" {
		t.Errorf("approval.default = %q, want ask", cfg.Approval.Default)
	}
	if cfg.ShellTool != "disabled" {
		t.Errorf("shell_tool = %q, want disabled", cfg.ShellTool)
	}
	if cfg.RunUser != "ancientcode" {
		t.Errorf("run_user = %q, want ancientcode", cfg.RunUser)
	}
	if cfg.ToolboxMode != "plan" {
		t.Errorf("toolbox_mode = %q, want plan", cfg.ToolboxMode)
	}
}

func TestLoadApprovalTimeout(t *testing.T) {
	cfg, err := loadWithKey(t, "model: some/model\napproval:\n  timeout_seconds: 5\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Approval.TimeoutSeconds != 5 {
		t.Errorf("approval.timeout_seconds = %d, want 5", cfg.Approval.TimeoutSeconds)
	}
}

func TestLoadForEachProvider(t *testing.T) {
	for _, p := range []string{ProviderOpenRouter, ProviderNVIDIA, ProviderLiteLLM} {
		t.Run(p, func(t *testing.T) {
			cfg, err := loadWithKey(t, "provider: "+p+"\nmodel: x/y")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Provider != p {
				t.Errorf("provider = %q", cfg.Provider)
			}
			if cfg.BaseURL == "" {
				t.Error("base_url empty")
			}
		})
	}
}

func TestValidateRejectsUnknownProvider(t *testing.T) {
	_, err := loadWithKey(t, "provider: megacorp\nmodel: x/y")
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("want unknown provider error, got %v", err)
	}
}

func TestValidateRejectsEmptyModel(t *testing.T) {
	_, err := loadWithKey(t, "")
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("want model error, got %v", err)
	}
}

func TestValidateRejectsMissingAPIKey(t *testing.T) {
	path := writeTempConfig(t, "model: x/y\napi_key_env: DOES_NOT_EXIST_123456")
	if _, err := Load(path); err == nil {
		t.Fatal("Load succeeded with missing API key env var")
	}
}

func TestValidateRejectsHTTPNonLoopback(t *testing.T) {
	_, err := loadWithKey(t, "model: x/y\nbase_url: http://evil.example.com/v1")
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("want loopback error, got %v", err)
	}
}

func TestValidateAllowsHTTPLoopback(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:4000/v1", "http://localhost:4000/v1", "http://[::1]:4000/v1"} {
		if _, err := loadWithKey(t, "model: x/y\nbase_url: "+base); err != nil {
			t.Errorf("loopback %q rejected: %v", base, err)
		}
	}
}

func TestValidateRejectsInvalidScheme(t *testing.T) {
	_, err := loadWithKey(t, "model: x/y\nbase_url: ftp://files.example.com/v1")
	if err == nil {
		t.Fatal("ftp scheme accepted")
	}
}

func TestValidateRejectsRootRunUser(t *testing.T) {
	_, err := loadWithKey(t, "model: x/y\nrun_user: root")
	if err == nil || !strings.Contains(err.Error(), "non-root") {
		t.Fatalf("want non-root error, got %v", err)
	}
}

func TestValidateShellEnabledRequiresAsk(t *testing.T) {
	_, err := loadWithKey(t, "model: x/y\nshell_tool: enabled\napproval:\n  default: allow")
	if err == nil || !strings.Contains(err.Error(), "shell_tool") {
		t.Fatalf("want shell_tool error, got %v", err)
	}
	// Explicit ask must be accepted.
	if _, err := loadWithKey(t, "model: x/y\nshell_tool: enabled\napproval:\n  default: ask"); err != nil {
		t.Fatalf("shell_tool=ask rejected: %v", err)
	}
}

func TestValidateDockerAndAptRequireAsk(t *testing.T) {
	// Mutating operating tools are deny-by-default: enabling them requires ask.
	for _, tc := range []struct {
		yaml string
		name string
	}{
		{"docker_tool: enabled", "docker_tool"},
		{"apt_tool: enabled", "apt_tool"},
	} {
		if _, err := loadWithKey(t, "model: x/y\napproval:\n  default: allow\n"+tc.yaml); err == nil || !strings.Contains(err.Error(), tc.name) {
			t.Fatalf("want %s error with allow, got %v", tc.name, err)
		}
	}
	if _, err := loadWithKey(t, "model: x/y\napproval:\n  default: ask\ndocker_tool: enabled\napt_tool: enabled"); err != nil {
		t.Fatalf("enabled with ask rejected: %v", err)
	}
	// Invalid toggle value is rejected.
	if _, err := loadWithKey(t, "model: x/y\ndocker_tool: sometimes"); err == nil || !strings.Contains(err.Error(), "docker_tool") {
		t.Fatalf("want docker_tool error, got %v", err)
	}
}

func TestValidateRejectsBadApprovalValue(t *testing.T) {
	_, err := loadWithKey(t, "model: x/y\napproval:\n  default: maybe")
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("want approval error, got %v", err)
	}
}

// M15 removeu o invariante "code_root dentro de workdir" (SPEC §10.3). Este
// teste foi SUBSTITUÍDO por workspace_config_test.go, que registra o contrato
// novo: a raiz do projeto e a casa do agente são irmãs e nenhuma contém a
// outra. O teste antigo é preservado aqui como histórico do que mudou:
//
//	code_root == workdir  -> recusado  (agente trabalharia sobre sessions/)
//	code_root fora do wd   -> recusado  (só porque não estava dentro)
//	code_root dentro do wd -> aceito
//
// A recusa que SOBREVIVEU é a primeira, e agora vive em internal/workspace,
// verificada a cada /cd em vez de uma vez no load.
func TestValidateCodeRootInsideWorkdir(t *testing.T) {
	t.Skip("removido no M15: o invariante code_root ⊂ workdir foi retirado em favor " +
		"da checagem em internal/workspace (a raiz do projeto não pode ser, estar " +
		"dentro nem conter a casa do agente). Ver workspace_config_test.go")
}

// TestDefaultSkillsDir: M7 skills_dir defaults to <home>/.config/ancientcode/skills
// (SPEC §11.1) and honors an explicit override.
func TestDefaultSkillsDir(t *testing.T) {
	base, err := os.MkdirTemp("", "aai-skills-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	wd := filepath.Join(base, "wd")
	if err := os.MkdirAll(wd, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadWithKey(t, "model: x/y\nworkdir: "+wd)
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "ancientcode", "skills")
	if cfg.SkillsDir != want {
		t.Errorf("default skills_dir = %q, want %q", cfg.SkillsDir, want)
	}
	custom := filepath.Join(wd, "skills-custom")
	cfg2, err := loadWithKey(t, "model: x/y\nworkdir: "+wd+"\nskills_dir: "+custom)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.SkillsDir != custom {
		t.Errorf("explicit skills_dir = %q, want %q", cfg2.SkillsDir, custom)
	}
}

func TestValidateRejectsBadTimeout(t *testing.T) {
	_, err := loadWithKey(t, "model: x/y\ntool_timeouts:\n  docker: banana")
	if err == nil || !strings.Contains(err.Error(), "docker") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("ANCIENTCODE_MODEL", "env/model")
	t.Setenv("ANCIENTCODE_BASE_URL", "https://override.example.com/v1")
	cfg, err := loadWithKey(t, "model: file/model")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "env/model" {
		t.Errorf("model = %q, want env/model", cfg.Model)
	}
	if cfg.BaseURL != "https://override.example.com/v1" {
		t.Errorf("base_url = %q, want env override", cfg.BaseURL)
	}
}

func TestMissingConfigFileIsNotAnError(t *testing.T) {
	t.Setenv(defaultAPIKeyEnv[ProviderOpenRouter], "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456")
	t.Setenv("ANCIENTCODE_MODEL", "env/model")
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "env/model" {
		t.Errorf("model = %q", cfg.Model)
	}
	if cfg.Provider != ProviderOpenRouter {
		t.Errorf("provider = %q", cfg.Provider)
	}
}

func TestMissingModelIsRejected(t *testing.T) {
	t.Setenv(defaultAPIKeyEnv[ProviderOpenRouter], "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456")
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("want model error, got %v", err)
	}
}

func TestToolTimeout(t *testing.T) {
	cfg, err := loadWithKey(t, "model: x/y")
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := cfg.ToolTimeout("docker"); d.Seconds() != 60 {
		t.Errorf("docker timeout = %v", d)
	}
	if d, err := cfg.ToolTimeout("bogus"); err != nil {
		t.Errorf("default timeout for unknown tool errored: %v", err)
	} else if d.Seconds() != 30 {
		t.Errorf("default timeout = %v", d)
	}
}
