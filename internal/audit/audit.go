// Package audit implements safu's package auditor: given a package coordinate
// (name, ecosystem, version), it gathers registry, source-repo, and
// vulnerability signals and returns a Verdict (safe / caution / danger) with
// the contributing reasons in plain language. See SPEC.md §5.
//
// Privacy (SPEC.md §10, CLAUDE.md invariant #1): the ONLY data this package
// sends off the machine is the package coordinate (name/ecosystem/version) to
// public registries, GitHub, and OSV.dev. It never transmits file contents,
// paths, or anything identifying the user. Every outbound call lives in this
// package's clients.
//
// Failure behavior (SPEC.md §5.5): network errors / 5xx / unreachable sources
// fail OPEN with a visible "could not verify" reason, unless
// Config.RequireNetwork is set, in which case the verdict fails closed.
package audit

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Ecosystem identifies a package registry safu understands.
type Ecosystem string

const (
	PyPI     Ecosystem = "pypi"
	NPM      Ecosystem = "npm"
	Crates   Ecosystem = "crates"
	Homebrew Ecosystem = "homebrew"
)

// Valid reports whether e is a known ecosystem.
func (e Ecosystem) Valid() bool {
	switch e {
	case PyPI, NPM, Crates, Homebrew:
		return true
	}
	return false
}

// Package is a single package coordinate to audit. Version may be empty, which
// means "the latest/unpinned release".
type Package struct {
	Name      string
	Ecosystem Ecosystem
	Version   string
}

func (p Package) key() string {
	return string(p.Ecosystem) + "/" + p.Name + "@" + p.Version
}

// Level is the overall risk classification for a package.
type Level string

const (
	Safe    Level = "safe"
	Caution Level = "caution"
	Danger  Level = "danger"
)

func rank(l Level) int {
	switch l {
	case Danger:
		return 2
	case Caution:
		return 1
	default:
		return 0
	}
}

// Reason is a single contributing signal, with a machine code, a
// plain-language explanation, and a tunable weight (weights are informational
// for now; the overall Level is category-driven — see score.go).
type Reason struct {
	Code   string
	Detail string
	Weight int
}

// Verdict is the result of auditing one package.
type Verdict struct {
	Package    Package
	Level      Level
	Reasons    []Reason
	Blocked    bool // confirmed-malicious (OSV MAL-); needs typed --force-malicious (§5.4)
	Unverified bool // at least one signal source failed and we proceeded unaudited
}

// Config holds scoring thresholds and policy. Defaults follow SPEC.md §5.4;
// later slices load these from config.toml.
type Config struct {
	NewPackageAgeDays int      // age below which a package is "young"
	LowDownloads      int64    // recent-download count below which downloads are "near-zero"
	StaleRepoYears    int      // last-commit older than this => "stale/abandoned"
	YoungStarsMax     int      // star count below which a young package is weak-but-corroborating
	BlockOn           []string // reason codes that escalate to danger (e.g. cve-critical, cve-high)
	RequireNetwork    bool     // if true, fail closed instead of open when a source is unreachable
}

// DefaultConfig returns the SPEC.md §5.4 defaults.
func DefaultConfig() Config {
	return Config{
		NewPackageAgeDays: 30,
		LowDownloads:      100,
		StaleRepoYears:    2,
		YoungStarsMax:     50,
		BlockOn:           []string{reasonMalicious, reasonCVECritical, reasonCVEHigh},
		RequireNetwork:    false,
	}
}

func (c Config) blocks(code string) bool {
	for _, b := range c.BlockOn {
		if b == code {
			return true
		}
	}
	return false
}

// Auditor wires the per-ecosystem registry clients, the OSV vulnerability
// client, the optional source-repo client, and the typosquat checker.
type Auditor struct {
	registries map[Ecosystem]registryClient
	vuln       vulnClient
	repo       repoClient
	typo       *Typosquat
	now        func() time.Time
	cfg        Config
}

// New returns an Auditor wired to the real public endpoints. If hc is nil a
// client with a sane timeout is used.
func New(hc *http.Client, cfg Config) *Auditor {
	if hc == nil {
		hc = &http.Client{Timeout: 12 * time.Second}
	}
	return newWithBases(hc, DefaultBaseURLs(), cfg)
}

// newWithBases is the shared constructor used by New and by tests (which point
// the base URLs at an httptest.Server).
func newWithBases(hc *http.Client, base BaseURLs, cfg Config) *Auditor {
	return &Auditor{
		registries: map[Ecosystem]registryClient{
			PyPI:     &pypiClient{hc: hc, base: base.PyPI, statsBase: base.PyPIStats},
			NPM:      &npmClient{hc: hc, base: base.NPM, dlBase: base.NPMDownloads},
			Crates:   &cratesClient{hc: hc, base: base.Crates},
			Homebrew: &homebrewClient{hc: hc, base: base.Homebrew},
		},
		vuln: &osvClient{hc: hc, base: base.OSV},
		repo: &githubClient{hc: hc, base: base.GitHub},
		typo: DefaultTyposquat(),
		now:  time.Now,
		cfg:  cfg,
	}
}

// signals is the raw evidence collected for one package before scoring.
type signals struct {
	meta    RegistryMeta
	metaErr error
	vuln    VulnResult
	vulnErr error
	repo    RepoSignals
	repoOK  bool
	typo    *TyposquatHit
}

// Audit audits a batch of packages and returns one Verdict per input package,
// in the same order. Registry metadata is gathered first (concurrently) so an
// unpinned package can be scoped to its latest version; OSV is then queried
// once for the whole batch (minimal disclosure, low latency, §5.2). Audit
// itself does not return an error for per-package source failures — those
// surface as Unverified verdicts (fail open, §5.5).
func (a *Auditor) Audit(ctx context.Context, pkgs []Package) ([]Verdict, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}
	n := len(pkgs)

	// Phase 1: registry metadata, concurrently.
	metas := make([]RegistryMeta, n)
	metaErrs := make([]error, n)
	var wg sync.WaitGroup
	for i, pkg := range pkgs {
		rc, ok := a.registries[pkg.Ecosystem]
		if !ok {
			continue
		}
		wg.Add(1)
		go func(i int, pkg Package, rc registryClient) {
			defer wg.Done()
			metas[i], metaErrs[i] = rc.Meta(ctx, pkg)
		}(i, pkg, rc)
	}
	wg.Wait()

	// Phase 2: one batched OSV query. Unpinned packages are scoped to the
	// registry's latest version so OSV returns only advisories that affect what
	// would actually be installed (not every historical CVE). Homebrew has no
	// OSV coverage and is excluded inside QueryBatch.
	osvPkgs := make([]Package, n)
	for i, pkg := range pkgs {
		osvPkgs[i] = pkg
		if pkg.Version == "" && metaErrs[i] == nil && metas[i].LatestVersion != "" {
			osvPkgs[i].Version = metas[i].LatestVersion
		}
	}
	vulnByKey, vulnErr := a.vuln.QueryBatch(ctx, osvPkgs)

	// Phase 3: repo enrichment + typosquat + scoring, concurrently.
	verdicts := make([]Verdict, n)
	for i, pkg := range pkgs {
		wg.Add(1)
		go func(i int, pkg Package) {
			defer wg.Done()

			s := signals{
				meta:    metas[i],
				metaErr: metaErrs[i],
				vuln:    vulnByKey[osvPkgs[i].key()],
				vulnErr: vulnErr,
			}

			// Source-repo enrichment only when the registry linked a repo. A
			// repo-signal failure is non-fatal: we still have a linked repo, so
			// it is not "no-repo"; we simply skip those signals.
			if s.metaErr == nil && s.meta.RepoURL != "" && a.repo != nil {
				if rs, err := a.repo.Signals(ctx, s.meta.RepoURL); err == nil {
					s.repo, s.repoOK = rs, true
				}
			}

			s.typo = a.typo.Check(pkg.Ecosystem, pkg.Name)

			verdicts[i] = a.assemble(pkg, s)
		}(i, pkg)
	}
	wg.Wait()
	return verdicts, nil
}

// assemble turns collected signals into a Verdict (see score.go for the rules).
func (a *Auditor) assemble(pkg Package, s signals) Verdict {
	reasons := a.collectReasons(pkg, s)

	v := Verdict{Package: pkg, Level: Safe, Reasons: reasons}

	// Determine the highest level among reasons and whether we're blocked.
	for _, r := range reasons {
		if r.Code == reasonMalicious {
			v.Blocked = true
		}
		if l := a.levelOf(r.Code); rank(l) > rank(v.Level) {
			v.Level = l
		}
	}

	// Unverified if any consulted source errored.
	if s.metaErr != nil && s.metaErr != errNotFound {
		v.Unverified = true
	}
	if s.vulnErr != nil {
		v.Unverified = true
	}

	// Fail closed when the operator demands network verification (§5.5).
	if v.Unverified && a.cfg.RequireNetwork && rank(Danger) > rank(v.Level) {
		v.Level = Danger
	}

	// Stable, readable reason order: highest-impact first.
	sort.SliceStable(v.Reasons, func(i, j int) bool {
		return rank(a.levelOf(v.Reasons[i].Code)) > rank(a.levelOf(v.Reasons[j].Code))
	})
	return v
}
