package audit

import (
	"fmt"
	"strings"
	"time"
)

// Reason codes. These are stable identifiers; Detail carries the human text.
const (
	reasonMalicious    = "malicious"
	reasonCVECritical  = "cve-critical"
	reasonCVEHigh      = "cve-high"
	reasonCVEModerate  = "cve-moderate"
	reasonCVELow       = "cve-low"
	reasonCVEUnknown   = "cve-unknown"
	reasonYoung        = "young"
	reasonLowDownloads = "low-downloads"
	reasonNoRepo       = "no-repo"
	reasonNotFound     = "not-found"
	reasonArchived     = "archived"
	reasonStale        = "stale"
	reasonFewStars     = "few-stars-young"
	reasonTyposquat    = "typosquat"
	reasonUnverified   = "unverified"
)

// levelOf maps a reason code to the risk level it implies. CVE-by-severity
// escalation respects Config.BlockOn (§5.4): codes listed there reach danger,
// otherwise they cap at caution.
func (a *Auditor) levelOf(code string) Level {
	switch code {
	case reasonMalicious:
		return Danger
	case reasonCVECritical, reasonCVEHigh:
		if a.cfg.blocks(code) {
			return Danger
		}
		return Caution
	case reasonUnverified:
		// Fail open: an unreachable source does not by itself raise the level.
		// The Unverified flag carries the visible warning; RequireNetwork is
		// what turns this into a fail-closed danger (handled in assemble).
		return Safe
	default:
		// Everything else (young, low-downloads, no-repo, archived, stale,
		// typosquat, moderate/low/unknown CVEs, unverified, not-found) is a
		// caution-level corroborating signal.
		return Caution
	}
}

// collectReasons turns raw signals into the list of contributing reasons.
func (a *Auditor) collectReasons(pkg Package, s signals) []Reason {
	var rs []Reason

	// --- Vulnerability / malicious (authoritative) ---
	if len(s.vuln.MalIDs) > 0 {
		rs = append(rs, Reason{
			Code:   reasonMalicious,
			Detail: fmt.Sprintf("confirmed malicious by OSV (%s)", strings.Join(s.vuln.MalIDs, ", ")),
			Weight: 1000,
		})
	}
	for _, cve := range s.vuln.CVEs {
		rs = append(rs, cveReason(cve))
	}

	// --- Registry metadata ---
	if s.metaErr == errNotFound {
		rs = append(rs, Reason{
			Code:   reasonNotFound,
			Detail: "package not found in its registry",
			Weight: 40,
		})
	} else if s.metaErr == nil && s.meta.Found {
		now := a.now()
		if s.meta.FirstKnown {
			age := now.Sub(s.meta.FirstPublished)
			if age < time.Duration(a.cfg.NewPackageAgeDays)*24*time.Hour {
				rs = append(rs, Reason{
					Code:   reasonYoung,
					Detail: fmt.Sprintf("first published %d days ago (< %d-day threshold)", int(age.Hours()/24), a.cfg.NewPackageAgeDays),
					Weight: 30,
				})
			}
		}
		if s.meta.DownloadsKnown && s.meta.Downloads < a.cfg.LowDownloads {
			rs = append(rs, Reason{
				Code:   reasonLowDownloads,
				Detail: fmt.Sprintf("near-zero downloads (%d, < %d)", s.meta.Downloads, a.cfg.LowDownloads),
				Weight: 25,
			})
		}
		// Homebrew's formula API exposes a homepage, not reliably a source
		// repo, so a missing repo link there is not a meaningful signal.
		if s.meta.RepoURL == "" && pkg.Ecosystem != Homebrew {
			rs = append(rs, Reason{
				Code:   reasonNoRepo,
				Detail: "no linked source repository to audit",
				Weight: 20,
			})
		}
	}

	// --- Source-repo health ---
	if s.repoOK {
		now := a.now()
		if s.repo.Archived {
			rs = append(rs, Reason{Code: reasonArchived, Detail: "source repository is archived", Weight: 25})
		}
		if !s.repo.LastCommit.IsZero() {
			if now.Sub(s.repo.LastCommit) > time.Duration(a.cfg.StaleRepoYears)*365*24*time.Hour {
				rs = append(rs, Reason{
					Code:   reasonStale,
					Detail: fmt.Sprintf("last commit %s (> %dy ago)", s.repo.LastCommit.Format("2006-01-02"), a.cfg.StaleRepoYears),
					Weight: 20,
				})
			}
		}
		// Weak-but-corroborating: few stars AND young.
		if s.repo.Stars < a.cfg.YoungStarsMax && hasReason(rs, reasonYoung) {
			rs = append(rs, Reason{
				Code:   reasonFewStars,
				Detail: fmt.Sprintf("only %d stars on a young package", s.repo.Stars),
				Weight: 15,
			})
		}
	}

	// --- Typosquat ---
	if s.typo != nil {
		rs = append(rs, Reason{
			Code:   reasonTyposquat,
			Detail: fmt.Sprintf("name is edit-distance %d from popular package %q", s.typo.Distance, s.typo.Target),
			Weight: 35,
		})
	}

	// --- Unverified (fail-open disclosure) ---
	if (s.metaErr != nil && s.metaErr != errNotFound) || s.vulnErr != nil {
		rs = append(rs, Reason{
			Code:   reasonUnverified,
			Detail: fmt.Sprintf("could not fully verify %s — proceeding unaudited", pkg.Name),
			Weight: 10,
		})
	}

	return rs
}

func cveReason(cve CVE) Reason {
	switch cve.Severity {
	case "critical":
		return Reason{Code: reasonCVECritical, Detail: fmt.Sprintf("critical vulnerability %s", cve.ID), Weight: 200}
	case "high":
		return Reason{Code: reasonCVEHigh, Detail: fmt.Sprintf("high-severity vulnerability %s", cve.ID), Weight: 150}
	case "moderate":
		return Reason{Code: reasonCVEModerate, Detail: fmt.Sprintf("moderate vulnerability %s", cve.ID), Weight: 60}
	case "low":
		return Reason{Code: reasonCVELow, Detail: fmt.Sprintf("low-severity vulnerability %s", cve.ID), Weight: 30}
	default:
		return Reason{Code: reasonCVEUnknown, Detail: fmt.Sprintf("vulnerability %s (severity unknown)", cve.ID), Weight: 50}
	}
}

func hasReason(rs []Reason, code string) bool {
	for _, r := range rs {
		if r.Code == code {
			return true
		}
	}
	return false
}
