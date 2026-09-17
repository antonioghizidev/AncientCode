package config

import "testing"

func TestProviderDefaultKnown(t *testing.T) {
	base, env, ok := (&Config{}).ProviderDefault("NVIDIA")
	if !ok {
		t.Fatal("nvidia should resolve")
	}
	if base != "https://integrate.api.nvidia.com/v1" {
		t.Errorf("base_url = %q", base)
	}
	if env != "NVIDIA_API_KEY" {
		t.Errorf("api_key_env = %q", env)
	}
}

func TestProviderDefaultUnknownFailClosed(t *testing.T) {
	if _, _, ok := (&Config{}).ProviderDefault("custom"); ok {
		t.Fatal("unknown provider resolved; must fail closed")
	}
}

func TestProviderNamesSorted(t *testing.T) {
	names := (&Config{}).ProviderNames()
	if len(names) != 3 {
		t.Fatalf("len(ProviderNames) = %d, want 3", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("ProviderNames not sorted: %v", names)
		}
	}
}
