package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// TestDefaultRoundTrip ensures the commented template renders valid TOML that
// parses back to exactly the defaults.
func TestDefaultRoundTrip(t *testing.T) {
	data, err := Render(Default())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var got Config
	if err := toml.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal rendered default: %v\n---\n%s", err, data)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("round-trip mismatch:\n got = %+v\nwant = %+v", got, Default())
	}
}

func TestPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/custom/xdg/safu/config.toml"; p != want {
		t.Errorf("Path with XDG = %q, want %q", p, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	p, err = Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "safu", "config.toml"); p != want {
		t.Errorf("Path without XDG = %q, want %q", p, want)
	}
}

func TestLoadFileMissingUsesDefaults(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err != nil {
		t.Fatalf("LoadFile missing: %v", err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("missing file did not yield defaults")
	}
}

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// File overrides the default guard.level and offline.
	if err := os.WriteFile(path, []byte("[guard]\nlevel = \"paranoid\"\n\n[network]\noffline = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clear env that could interfere.
	for _, k := range []string{"SAFU_DISABLE", "SAFU_OFFLINE", "SAFU_NO_UPDATE_CHECK", "SAFU_SCANNER_KEY"} {
		t.Setenv(k, "")
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Guard.Level != "paranoid" {
		t.Errorf("file override: guard.level = %q, want paranoid", cfg.Guard.Level)
	}
	// Untouched field keeps its default.
	if !cfg.Guard.SoftDelete {
		t.Errorf("default not preserved: soft_delete should remain true")
	}

	// Env overrides the file.
	t.Setenv("SAFU_OFFLINE", "1")
	t.Setenv("SAFU_DISABLE", "1")
	cfg, err = LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Network.Offline {
		t.Errorf("env override: offline should be true")
	}
	if cfg.Guard.Level != "off" {
		t.Errorf("SAFU_DISABLE: guard.level = %q, want off", cfg.Guard.Level)
	}
	if cfg.Audit.Enabled {
		t.Errorf("SAFU_DISABLE: audit should be disabled")
	}
}

func TestValidate(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Errorf("default config invalid: %v", err)
	}
	c.Guard.Level = "bogus"
	if err := c.Validate(); err == nil {
		t.Error("expected error for invalid guard.level")
	}
	c = Default()
	c.Audit.Scanner = "nope"
	if err := c.Validate(); err == nil {
		t.Error("expected error for invalid audit.scanner")
	}
	c = Default()
	c.Log.ActivityRetentionDays = -1
	if err := c.Validate(); err == nil {
		t.Error("expected error for negative retention")
	}
}

func TestWriteDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile written: %v", err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("written config did not load back to defaults")
	}
}
