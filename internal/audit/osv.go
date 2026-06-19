package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// osvClient queries OSV.dev. It uses /v1/querybatch to detect, in one request,
// both confirmed-malicious packages (IDs prefixed "MAL-", SPEC.md §5.2) and
// known vulnerabilities. querybatch returns only vuln IDs; for non-MAL IDs we
// fetch /v1/vulns/{id} to read severity. Severity lookups are best-effort: a
// failure downgrades a CVE to "severity unknown" (caution) rather than failing
// the whole audit.
type osvClient struct {
	hc   *http.Client
	base string // https://api.osv.dev
}

// VulnResult is the OSV outcome for one package.
type VulnResult struct {
	MalIDs []string // confirmed-malicious advisory IDs (MAL-...)
	CVEs   []CVE    // non-malicious vulnerabilities
}

// CVE is a single non-malicious vulnerability with a resolved severity bucket.
type CVE struct {
	ID       string
	Severity string // "critical" | "high" | "moderate" | "low" | "" (unknown)
}

// osvEcosystem maps safu ecosystems to OSV ecosystem names. Homebrew has no
// OSV coverage and returns "" (excluded from the batch).
func osvEcosystem(e Ecosystem) string {
	switch e {
	case PyPI:
		return "PyPI"
	case NPM:
		return "npm"
	case Crates:
		return "crates.io"
	default:
		return ""
	}
}

type osvBatchRequest struct {
	Queries []osvQuery `json:"queries"`
}

type osvQuery struct {
	Package osvPackage `json:"package"`
	Version string     `json:"version,omitempty"`
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvBatchResponse struct {
	Results []struct {
		Vulns []struct {
			ID string `json:"id"`
		} `json:"vulns"`
	} `json:"results"`
}

func (c *osvClient) QueryBatch(ctx context.Context, pkgs []Package) (map[string]VulnResult, error) {
	out := make(map[string]VulnResult)

	// Build the batch, tracking which input package each query maps back to.
	var req osvBatchRequest
	var owners []Package
	for _, p := range pkgs {
		eco := osvEcosystem(p.Ecosystem)
		if eco == "" {
			continue
		}
		req.Queries = append(req.Queries, osvQuery{
			Package: osvPackage{Name: p.Name, Ecosystem: eco},
			Version: p.Version,
		})
		owners = append(owners, p)
	}
	if len(req.Queries) == 0 {
		return out, nil
	}

	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	var resp osvBatchResponse
	if err := postJSON(ctx, c.hc, c.base+"/v1/querybatch", bytes.NewReader(body), &resp); err != nil {
		return out, err
	}

	for i, res := range resp.Results {
		if i >= len(owners) {
			break
		}
		var vr VulnResult
		for _, v := range res.Vulns {
			if strings.HasPrefix(strings.ToUpper(v.ID), "MAL-") {
				vr.MalIDs = append(vr.MalIDs, v.ID)
				continue
			}
			vr.CVEs = append(vr.CVEs, CVE{ID: v.ID, Severity: c.severity(ctx, v.ID)})
		}
		out[owners[i].key()] = vr
	}
	return out, nil
}

type osvVulnDetail struct {
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
}

// severity resolves a CVE's severity bucket. Best-effort: on any failure it
// returns "" (unknown), which scores as caution.
func (c *osvClient) severity(ctx context.Context, id string) string {
	var d osvVulnDetail
	if err := getJSON(ctx, c.hc, c.base+"/v1/vulns/"+id, &d); err != nil {
		return ""
	}
	return severityBucket(d)
}

// severityBucket prefers the human-readable database_specific.severity string
// (present on GHSA-sourced entries) and falls back to a CVSS base score parsed
// from the vector.
func severityBucket(d osvVulnDetail) string {
	switch strings.ToUpper(d.DatabaseSpecific.Severity) {
	case "CRITICAL":
		return "critical"
	case "HIGH":
		return "high"
	case "MODERATE", "MEDIUM":
		return "moderate"
	case "LOW":
		return "low"
	}
	for _, s := range d.Severity {
		if score, ok := cvssBaseScore(s.Score); ok {
			return bucketFromScore(score)
		}
	}
	return ""
}

func bucketFromScore(score float64) string {
	switch {
	case score >= 9.0:
		return "critical"
	case score >= 7.0:
		return "high"
	case score >= 4.0:
		return "moderate"
	case score > 0:
		return "low"
	default:
		return ""
	}
}

// cvssBaseScore extracts a numeric base score from an OSV severity score
// field. Some entries store a plain number ("9.8"); others store a full CVSS
// vector string ("CVSS:3.1/AV:N/..."). Computing a base score from the vector
// requires the full CVSS formula, which we deliberately don't implement here —
// such entries return ok=false and fall through to "severity unknown".
func cvssBaseScore(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(strings.ToUpper(s), "CVSS:") {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
