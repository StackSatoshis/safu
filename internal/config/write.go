package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// configTemplate renders a fully-commented config.toml from a Config value. We
// use a hand-written template (not the TOML encoder) so the option docs are
// preserved for hand-editing — the file is meant to be read by humans.
const configTemplate = `# safu configuration. See SPEC.md §9 for the full reference.
# Precedence: CLI flag > environment variable > this file > built-in default.

[guard]
level = "{{.Guard.Level}}"                 # off | light | standard | paranoid
soft_delete = {{.Guard.SoftDelete}}              # route deletes to trash with undo
trash_dir = "{{.Guard.TrashDir}}"
trash_retention_days = {{.Guard.TrashRetentionDays}}
wrapped = {{strs .Guard.Wrapped}}

[audit]
enabled = {{.Audit.Enabled}}                  # audit packages before install (default tier only)
new_package_age_days = {{.Audit.NewPackageAgeDays}}
block_on = {{strs .Audit.BlockOn}}
warn_on = {{strs .Audit.WarnOn}}
deep_scan = {{.Audit.DeepScan}}               # Tier 1: local-only static scan
scanner = "{{.Audit.Scanner}}"                    # Tier 2: "" | virustotal | cloudmersive (user key)
scanner_key = "{{.Audit.ScannerKey}}"                # or via SAFU_SCANNER_KEY
require_network = {{.Audit.RequireNetwork}}         # if true, fail closed when audit can't run

[navigation]
enabled = {{.Navigation.Enabled}}                # smart-jump opt-in
cmd = "{{.Navigation.Cmd}}"
data_dir = "{{.Navigation.DataDir}}"
exclude = {{strs .Navigation.Exclude}}
max_entries = {{.Navigation.MaxEntries}}

[log]
enabled = {{.Log.Enabled}}                   # safu's own activity log
dir = "{{.Log.Dir}}"
activity_retention_days = {{.Log.ActivityRetentionDays}}
history = {{.Log.History}}                   # opt-in: record general shell history
history_retention_days = {{.Log.HistoryRetentionDays}}
history_exclude = {{strs .Log.HistoryExclude}}

[tui]
enabled = {{.TUI.Enabled}}                   # built-in interactive UI; flag fallbacks always exist

[network]
update_check = {{.Network.UpdateCheck}}             # opt-out; SAFU_NO_UPDATE_CHECK=1 also disables
offline = {{.Network.Offline}}                  # master kill switch for all outbound calls

[fix]
enabled = {{.Fix.Enabled}}                 # opt-in: correction helper (safu fix / wtf) + stderr capture
aliases = {{strs .Fix.Aliases}}
require_confirmation = {{.Fix.RequireConfirmation}}
`

var tmpl = template.Must(template.New("config").Funcs(template.FuncMap{
	"strs": tomlStrings,
}).Parse(configTemplate))

// tomlStrings renders a string slice as a TOML array literal.
func tomlStrings(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// Render returns the commented TOML for a Config.
func Render(c Config) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, c); err != nil {
		return nil, fmt.Errorf("render config: %w", err)
	}
	return buf.Bytes(), nil
}

// Write renders c and writes it to path, creating parent directories. It
// overwrites unconditionally; callers decide whether to clobber.
func Write(c Config, path string) error {
	data, err := Render(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// WriteDefault writes the commented default config to path.
func WriteDefault(path string) error { return Write(Default(), path) }
