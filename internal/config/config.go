// Package config defines safu's on-disk configuration schema (config.toml),
// its built-in defaults, loading with precedence, and writing a commented
// default file. See SPEC.md §9.
//
// Precedence (SPEC.md §9.3): CLI flag > environment variable > config file >
// built-in default. This package implements file-over-default and
// env-over-file; the CLI layer applies flags on top.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/StackSatoshis/safu/internal/audit"
	toml "github.com/pelletier/go-toml/v2"
)

// Config is the full schema (SPEC.md §9.2).
type Config struct {
	Guard      Guard      `toml:"guard"`
	Audit      Audit      `toml:"audit"`
	Navigation Navigation `toml:"navigation"`
	Log        Log        `toml:"log"`
	TUI        TUI        `toml:"tui"`
	Network    Network    `toml:"network"`
	Fix        Fix        `toml:"fix"`
}

type Guard struct {
	Level              string   `toml:"level"` // off | light | standard | paranoid
	SoftDelete         bool     `toml:"soft_delete"`
	TrashDir           string   `toml:"trash_dir"`
	TrashRetentionDays int      `toml:"trash_retention_days"`
	Wrapped            []string `toml:"wrapped"`
}

type Audit struct {
	Enabled           bool     `toml:"enabled"`
	NewPackageAgeDays int      `toml:"new_package_age_days"`
	BlockOn           []string `toml:"block_on"`
	WarnOn            []string `toml:"warn_on"`
	DeepScan          bool     `toml:"deep_scan"`
	Scanner           string   `toml:"scanner"` // "" | virustotal | cloudmersive
	ScannerKey        string   `toml:"scanner_key"`
	RequireNetwork    bool     `toml:"require_network"`
}

type Navigation struct {
	Enabled    bool     `toml:"enabled"`
	Cmd        string   `toml:"cmd"`
	DataDir    string   `toml:"data_dir"`
	Exclude    []string `toml:"exclude"`
	MaxEntries int      `toml:"max_entries"`
}

type Log struct {
	Enabled               bool     `toml:"enabled"`
	Dir                   string   `toml:"dir"`
	ActivityRetentionDays int      `toml:"activity_retention_days"`
	History               bool     `toml:"history"`
	HistoryRetentionDays  int      `toml:"history_retention_days"`
	HistoryExclude        []string `toml:"history_exclude"`
}

type TUI struct {
	Enabled bool `toml:"enabled"`
}

type Network struct {
	UpdateCheck bool `toml:"update_check"`
	Offline     bool `toml:"offline"`
}

type Fix struct {
	Enabled             bool     `toml:"enabled"`
	Aliases             []string `toml:"aliases"`
	RequireConfirmation bool     `toml:"require_confirmation"`
}

// Default returns the built-in defaults (SPEC.md §9.2).
func Default() Config {
	return Config{
		Guard: Guard{
			Level:              "standard",
			SoftDelete:         true,
			TrashDir:           "~/.safu/trash",
			TrashRetentionDays: 7,
			Wrapped:            []string{"rm", "git-push-force", "dd", "mkfs", "chmod-r", "chown-r", "pip", "npm", "cargo", "brew"},
		},
		Audit: Audit{
			Enabled:           true,
			NewPackageAgeDays: 30,
			BlockOn:           []string{"malicious", "cve-critical", "cve-high"},
			WarnOn:            []string{"young", "no-repo", "archived", "typosquat", "low-downloads"},
			DeepScan:          false,
			Scanner:           "",
			ScannerKey:        "",
			RequireNetwork:    false,
		},
		Navigation: Navigation{
			Enabled:    false,
			Cmd:        "z",
			DataDir:    "~/.safu/nav",
			Exclude:    []string{"$HOME", "$HOME/private/*"},
			MaxEntries: 10000,
		},
		Log: Log{
			Enabled:               true,
			Dir:                   "~/.safu/log",
			ActivityRetentionDays: 90,
			History:               false,
			HistoryRetentionDays:  90,
			// Commands matching any of these (case-insensitive substring) are
			// never recorded. Curated to catch common secrets without being so
			// broad it drops ordinary commands.
			HistoryExclude: []string{
				"*token*", "*secret*", "*password*", "*passwd*", "*credential*",
				"*api_key*", "*apikey*", "*api-key*", "*access_key*", "*private_key*",
				"*bearer*", "*ghp_*", "*github_pat*", "*xoxb-*", "*xoxp-*",
				"*aws_secret*", "*-----begin*",
			},
		},
		TUI:     TUI{Enabled: true},
		Network: Network{UpdateCheck: true, Offline: false},
		Fix:     Fix{Enabled: false, Aliases: []string{"fix", "wtf"}, RequireConfirmation: true},
	}
}

// Path returns the config file location: $XDG_CONFIG_HOME/safu/config.toml,
// falling back to ~/.config/safu/config.toml (SPEC.md §9.1).
func Path() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "safu", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "safu", "config.toml"), nil
}

// Load resolves the config path, loads it (or defaults if absent), applies env
// overrides, and validates.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Default(), err
	}
	return LoadFile(path)
}

// ReadFile loads a config from path by overlaying it on the defaults, WITHOUT
// applying env overrides or validating. Use this when you intend to edit and
// re-save the file (so env-derived values are not baked in). A missing file is
// not an error — the defaults are returned.
func ReadFile(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse %s: %w", path, err)
		}
	case errors.Is(err, fs.ErrNotExist):
		// keep defaults
	default:
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, nil
}

// LoadFile loads from a specific path, applies env overrides on top, and
// validates. A missing file is not an error — the defaults are used.
func LoadFile(path string) (Config, error) {
	cfg, err := ReadFile(path)
	if err != nil {
		return cfg, err
	}
	ApplyEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// ApplyEnv overlays environment-variable overrides (SPEC.md §9.3). These are
// the documented kill switches and the scanner key.
func ApplyEnv(c *Config) {
	if envTrue("SAFU_DISABLE") {
		// Master kill switch: guard off, auditor off.
		c.Guard.Level = "off"
		c.Audit.Enabled = false
	}
	if envTrue("SAFU_OFFLINE") {
		c.Network.Offline = true
	}
	if envTrue("SAFU_NO_UPDATE_CHECK") {
		c.Network.UpdateCheck = false
	}
	if k := os.Getenv("SAFU_SCANNER_KEY"); k != "" {
		c.Audit.ScannerKey = k
	}
}

func envTrue(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

var (
	validLevels   = map[string]bool{"off": true, "light": true, "standard": true, "paranoid": true}
	validScanners = map[string]bool{"": true, "virustotal": true, "cloudmersive": true}
)

// Validate checks enumerated and numeric fields.
func (c Config) Validate() error {
	if !validLevels[c.Guard.Level] {
		return fmt.Errorf("guard.level %q invalid (want off|light|standard|paranoid)", c.Guard.Level)
	}
	if !validScanners[c.Audit.Scanner] {
		return fmt.Errorf("audit.scanner %q invalid (want \"\"|virustotal|cloudmersive)", c.Audit.Scanner)
	}
	for name, v := range map[string]int{
		"guard.trash_retention_days":  c.Guard.TrashRetentionDays,
		"audit.new_package_age_days":  c.Audit.NewPackageAgeDays,
		"navigation.max_entries":      c.Navigation.MaxEntries,
		"log.activity_retention_days": c.Log.ActivityRetentionDays,
		"log.history_retention_days":  c.Log.HistoryRetentionDays,
	} {
		if v < 0 {
			return fmt.Errorf("%s must be >= 0 (got %d)", name, v)
		}
	}
	return nil
}

// AuditConfig adapts the on-disk audit settings to the audit engine's runtime
// Config (slice 1).
func (c Config) AuditConfig() audit.Config {
	ac := audit.DefaultConfig()
	if c.Audit.NewPackageAgeDays > 0 {
		ac.NewPackageAgeDays = c.Audit.NewPackageAgeDays
	}
	if len(c.Audit.BlockOn) > 0 {
		ac.BlockOn = c.Audit.BlockOn
	}
	ac.RequireNetwork = c.Audit.RequireNetwork
	return ac
}

// Expand resolves a leading "~" and environment variables in a path/value.
func Expand(p string) string {
	if p == "" {
		return p
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + p[1:]
		}
	}
	return os.ExpandEnv(p)
}
