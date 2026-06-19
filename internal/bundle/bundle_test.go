package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/StackSatoshis/safu/internal/config"
	"github.com/StackSatoshis/safu/internal/shell"
)

func comp(m Manifest, key string) (Component, bool) {
	for _, c := range m.Components {
		if c.Key == key {
			return c, true
		}
	}
	return Component{}, false
}

func TestBuildProfiles(t *testing.T) {
	base := config.Default()

	min, err := Build(Minimal, shell.Zsh, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if min.Config.Navigation.Enabled {
		t.Error("minimal should not enable navigation")
	}
	if !min.Config.Fix.Enabled || !min.Config.Guard.SoftDelete {
		t.Error("minimal should enable fix + soft-delete")
	}
	if _, ok := comp(min, "prompt"); ok {
		t.Error("minimal should not list a prompt component")
	}
	if !strings.Contains(min.RCBlock, "safu guard --as") {
		t.Error("rc block missing guard integration")
	}

	std, err := Build(Standard, shell.Zsh, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !std.Config.Navigation.Enabled {
		t.Error("standard should enable navigation")
	}
	for _, want := range []string{"alias ll=", "PROMPT=", "safu z --add"} {
		if !strings.Contains(std.RCBlock, want) {
			t.Errorf("standard rc block missing %q", want)
		}
	}
}

func TestBuildSkip(t *testing.T) {
	m, err := Build(Standard, shell.Bash, config.Default(), map[string]bool{"prompt": true, "nav": true})
	if err != nil {
		t.Fatal(err)
	}
	if m.Config.Navigation.Enabled {
		t.Error("nav skipped but still enabled in config")
	}
	if strings.Contains(m.RCBlock, "PS1=") {
		t.Error("prompt was skipped but appears in rc block")
	}
	if c, _ := comp(m, "prompt"); c.Included {
		t.Error("prompt component should be marked not included")
	}
	if !strings.Contains(m.RCBlock, "alias ll=") {
		t.Error("aliases should still be present")
	}
}

func TestBuildRejectsFish(t *testing.T) {
	if _, err := Build(Standard, shell.Fish, config.Default(), nil); err == nil {
		t.Error("fish should be rejected by the bundle")
	}
}

func TestInstallUninstallRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(rc, []byte("export EDITOR=vim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	safuDir := filepath.Join(dir, ".safu")

	m, err := Build(Standard, shell.Zsh, config.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)
	res, err := Install(m, Paths{RC: rc, Config: cfgPath, SafuDir: safuDir}, now)
	if err != nil {
		t.Fatal(err)
	}

	rcData, _ := os.ReadFile(rc)
	if !IsInstalled(string(rcData)) {
		t.Fatal("bundle block not installed")
	}
	if !strings.Contains(string(rcData), "export EDITOR=vim") {
		t.Error("pre-existing rc content lost")
	}
	if res.RCBackup == "" {
		t.Error("expected an rc backup")
	}
	if _, err := os.Stat(res.Uninstaller); err != nil {
		t.Errorf("uninstaller not written: %v", err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config not written: %v", err)
	}

	// Re-install is idempotent: still exactly one block.
	if _, err := Install(m, Paths{RC: rc, Config: cfgPath, SafuDir: safuDir}, now); err != nil {
		t.Fatal(err)
	}
	rcData, _ = os.ReadFile(rc)
	if n := strings.Count(string(rcData), markerStart); n != 1 {
		t.Errorf("re-install left %d bundle blocks, want 1", n)
	}

	// Uninstall removes the block, preserves other content.
	removed, err := Uninstall(rc)
	if err != nil || !removed {
		t.Fatalf("uninstall: removed=%v err=%v", removed, err)
	}
	rcData, _ = os.ReadFile(rc)
	if IsInstalled(string(rcData)) {
		t.Error("bundle block still present after uninstall")
	}
	if !strings.Contains(string(rcData), "export EDITOR=vim") {
		t.Error("uninstall removed unrelated content")
	}

	// Uninstall again is a no-op.
	if removed, _ := Uninstall(rc); removed {
		t.Error("second uninstall should report nothing removed")
	}
}

func TestUninstallScriptIsPureShell(t *testing.T) {
	s := uninstallScript("/home/u/.zshrc")
	if !strings.HasPrefix(s, "#!/bin/sh") {
		t.Error("uninstaller should be a /bin/sh script")
	}
	if !strings.Contains(s, "/home/u/.zshrc") || !strings.Contains(s, "awk") {
		t.Error("uninstaller should bake in the rc path and use awk")
	}
	// Must not *invoke* the safu binary (mentioning it in echo text is fine).
	if strings.Contains(s, "command safu") || strings.Contains(s, "$(safu") {
		t.Error("uninstaller must not invoke the safu binary")
	}
}
