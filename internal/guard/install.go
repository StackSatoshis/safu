package guard

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/StackSatoshis/safu/internal/audit"
)

// ParseInstall recognizes a package-install command and extracts the package
// coordinates to audit (SPEC.md §5.1). It returns isInstall=false for anything
// that is not a recognized install invocation.
//
// Limitations (intentional for this slice): no full PEP 508 parsing, no
// lockfiles (package-lock.json / Cargo.lock / poetry.lock), and VCS/URL/local
// specs are skipped (they can't be audited by registry coordinate). `npm
// install` with no package arguments installs from package.json and yields no
// packages to audit.
func ParseInstall(name string, argv []string, cwd string) (audit.Ecosystem, []audit.Package, bool) {
	switch filepath.Base(name) {
	case "pip", "pip3", "pipx", "uv":
		return parsePip(name, argv, cwd)
	case "npm", "pnpm", "yarn":
		return parseNpm(argv)
	case "cargo":
		return parseCargo(argv)
	case "brew":
		return parseBrew(argv)
	}
	return "", nil, false
}

// SummarizeAudit reduces a batch of verdicts to a single guard Risk plus a flag
// for any confirmed-malicious package. Per the configured policy, any
// non-safe verdict (caution or danger) prompts; malicious blocks outright
// (overridable only with a typed --force-malicious, invariant #5).
func SummarizeAudit(verdicts []audit.Verdict) (risk Risk, malicious bool) {
	risk = Safe
	for _, v := range verdicts {
		if v.Blocked {
			malicious = true
		}
		switch v.Level {
		case audit.Danger, audit.Caution:
			risk = maxRisk(risk, Warn)
		}
	}
	return risk, malicious
}

// pipValueFlags are pip options that consume the following token as a value
// (so it must not be mistaken for a package).
var pipValueFlags = map[string]bool{
	"-i": true, "--index-url": true, "--extra-index-url": true,
	"-f": true, "--find-links": true, "-t": true, "--target": true,
	"-c": true, "--constraint": true, "--python": true, "-p": true,
}

func parsePip(name string, argv []string, cwd string) (audit.Ecosystem, []audit.Package, bool) {
	rest, ok := installArgs(name, argv)
	if !ok {
		return "", nil, false
	}
	var pkgs []audit.Package
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if tok == "-r" || tok == "--requirement" {
			if i+1 < len(rest) {
				pkgs = append(pkgs, parseReqFile(rest[i+1], cwd)...)
				i++
			}
			continue
		}
		if strings.HasPrefix(tok, "-") {
			if pipValueFlags[tok] && i+1 < len(rest) {
				i++
			}
			continue
		}
		if p, ok := pipSpec(tok); ok {
			pkgs = append(pkgs, p)
		}
	}
	return audit.PyPI, pkgs, true
}

// installArgs strips the leading verb(s) and returns the operand tokens, or
// ok=false if argv is not an install command for this tool. `uv` uses
// `uv pip install ...`; the others use `<tool> install ...`.
func installArgs(name string, argv []string) ([]string, bool) {
	if filepath.Base(name) == "uv" {
		if len(argv) >= 2 && argv[0] == "pip" && argv[1] == "install" {
			return argv[2:], true
		}
		return nil, false
	}
	if len(argv) >= 1 && argv[0] == "install" {
		return argv[1:], true
	}
	return nil, false
}

func pipSpec(s string) (audit.Package, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "://") || strings.HasPrefix(s, "git+") ||
		strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
		return audit.Package{}, false
	}
	name, ver := s, ""
	if i := strings.IndexAny(s, "<>=!~;[ ("); i >= 0 {
		name = s[:i]
		if spec := s[i:]; strings.HasPrefix(spec, "==") {
			ver = cutVersion(spec[2:])
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return audit.Package{}, false
	}
	return audit.Package{Name: name, Ecosystem: audit.PyPI, Version: ver}, true
}

// cutVersion takes the leading version token, stopping at a separator.
func cutVersion(s string) string {
	if i := strings.IndexAny(s, ",; "); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func parseReqFile(path, cwd string) []audit.Package {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var pkgs []audit.Package
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "git+") || strings.Contains(line, "://") {
			continue
		}
		// take the first whitespace-delimited field (drops trailing markers)
		if f := strings.Fields(line); len(f) > 0 {
			if p, ok := pipSpec(f[0]); ok {
				pkgs = append(pkgs, p)
			}
		}
	}
	return pkgs
}

func parseNpm(argv []string) (audit.Ecosystem, []audit.Package, bool) {
	start := -1
	for i, a := range argv {
		if a == "install" || a == "i" || a == "add" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", nil, false
	}
	var pkgs []audit.Package
	for _, tok := range argv[start:] {
		if strings.HasPrefix(tok, "-") {
			continue
		}
		if p, ok := npmSpec(tok); ok {
			pkgs = append(pkgs, p)
		}
	}
	return audit.NPM, pkgs, true
}

func npmSpec(s string) (audit.Package, bool) {
	if s == "" || strings.Contains(s, "://") || strings.HasPrefix(s, "git+") ||
		strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".tgz") {
		return audit.Package{}, false
	}
	name, ver := s, ""
	if strings.HasPrefix(s, "@") {
		// @scope/name[@version]
		if at := strings.LastIndex(s, "@"); at > 0 {
			name, ver = s[:at], s[at+1:]
		}
	} else {
		if strings.Contains(s, "/") {
			return audit.Package{}, false // github shorthand or local path
		}
		if at := strings.Index(s, "@"); at > 0 {
			name, ver = s[:at], s[at+1:]
		}
	}
	return audit.Package{Name: name, Ecosystem: audit.NPM, Version: ver}, true
}

var cargoValueFlags = map[string]bool{
	"--features": true, "-F": true, "--git": true, "--branch": true,
	"--tag": true, "--rev": true, "--path": true, "--registry": true,
	"--manifest-path": true,
}

func parseCargo(argv []string) (audit.Ecosystem, []audit.Package, bool) {
	if len(argv) == 0 || (argv[0] != "add" && argv[0] != "install") {
		return "", nil, false
	}
	var pkgs []audit.Package
	rest := argv[1:]
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if tok == "--version" || tok == "--vers" {
			if i+1 < len(rest) {
				if len(pkgs) > 0 {
					pkgs[len(pkgs)-1].Version = rest[i+1]
				}
				i++
			}
			continue
		}
		if strings.HasPrefix(tok, "-") {
			if cargoValueFlags[tok] && i+1 < len(rest) {
				i++
			}
			continue
		}
		if strings.Contains(tok, "://") || strings.HasPrefix(tok, ".") {
			continue
		}
		name, ver := tok, ""
		if at := strings.Index(tok, "@"); at > 0 {
			name, ver = tok[:at], tok[at+1:]
		}
		pkgs = append(pkgs, audit.Package{Name: name, Ecosystem: audit.Crates, Version: ver})
	}
	return audit.Crates, pkgs, true
}

func parseBrew(argv []string) (audit.Ecosystem, []audit.Package, bool) {
	if len(argv) == 0 || argv[0] != "install" {
		return "", nil, false
	}
	var pkgs []audit.Package
	for _, tok := range argv[1:] {
		if strings.HasPrefix(tok, "-") {
			continue
		}
		if strings.Contains(tok, "/") || strings.Contains(tok, "://") {
			continue // taps / URLs are not audited by formula coordinate here
		}
		pkgs = append(pkgs, audit.Package{Name: tok, Ecosystem: audit.Homebrew})
	}
	return audit.Homebrew, pkgs, true
}
