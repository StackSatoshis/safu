# Security Policy

safu is a safety and privacy tool, so we take security reports seriously. Thank you
for helping keep safu and its users safe.

## Reporting a vulnerability

**Please do not open a public issue for security problems.** Report privately through
either channel:

- **GitHub Security Advisories** (preferred): use the **"Report a vulnerability"** button
  under the **Security** tab of the `StackSatoshis/safu` repository. This opens a private
  advisory only you and the maintainers can see.
- **Email:** `security@safu.sh` — encrypt with our published key if you can; otherwise send
  a description and we will arrange a secure channel.

Please include, where possible:

- safu version (`safu version`) and OS / architecture.
- A description of the issue and its impact.
- Steps to reproduce, a proof-of-concept, or the relevant command/argv/config.
- Any logs — but **redact secrets** first.

### Our commitment

- **Acknowledge** your report within **3 business days**.
- Provide an initial **assessment and triage** within **7 days**.
- Work with you on a **coordinated disclosure** timeline (default target: a fix or
  mitigation within **90 days**, sooner for actively-exploited or critical issues).
- **Credit** you in the advisory and release notes unless you prefer to remain anonymous.

## Supported versions

safu is pre-1.0. Security fixes are made against the **latest release** and `main`. Please
reproduce on the latest version before reporting.

| Version        | Supported |
| -------------- | --------- |
| latest release | ✅        |
| older releases | ❌        |

## What's in scope

safu's core promise is **local-first privacy** and **never breaking your shell**. The
following are security issues we want to hear about:

- **Privacy-contract violations.** safu makes exactly **two** kinds of outbound network
  call: the opt-out **update check** and the **package audits** you run. Audits send only a
  package coordinate (name / ecosystem / version). Reportable bugs include:
  - any outbound connection from the guard, activity log, navigation database, correction
    helper, or shell-history features (these must be firewall-safe and never network);
  - an audit transmitting anything beyond the package coordinate (e.g. file contents,
    paths, environment, or user identifiers);
  - a third-party scanner being contacted without the user supplying their own key.
  - *You can verify the firewall-safe claim yourself: `grep -rn "http\." --include="*.go"`
    should show network calls only under `internal/audit` and `internal/update`.*
- **Guard bypass of a catastrophic operation** that safu claims to block at the active
  protection level (e.g. a recursive delete that resolves into `$HOME` or a filesystem
  root slipping past the classifier), or the confirmed-malicious (`MAL-`) block being
  bypassed without the user typing `--force-malicious`.
- **The correction helper re-executing the previous command.** `safu fix`/`wtf` must only
  read stored output; re-running the prior command is a security bug.
- **Data loss / soft-delete & undo flaws.** Trash that drops files, `undo` restoring to the
  wrong location, or path-traversal in the trash store.
- **Local data exposure.** Secrets captured despite the history exclude rules, or
  safu-created files written with overly-permissive modes.
- **Code execution or injection** via a crafted registry/OSV response, crafted argv,
  crafted stderr fed to the fixer, or a crafted `config.toml`.
- **Installer / bundle integrity.** The `curl | sh` installer failing to verify the
  SHA-256 against `checksums.txt` from GitHub, the bundle writing outside its intended
  files, an irreversible change, or a broken uninstaller.

## What's out of scope

By design (see `SPEC.md` §2), safu protects against **mistakes, not a hostile local
actor**:

- **Trivial guard bypass by the operator.** `\rm`, `command rm`, calling the real binary
  directly, reordering `PATH`, or unsetting the shell integration are expected escape
  hatches, not vulnerabilities. safu is not a sandbox or kernel-level enforcement layer.
- **The user explicitly overriding a block** with a typed `--force` / `--force-malicious`.
- **Third-party services** safu queries (PyPI, npm, crates.io, Homebrew, GitHub, OSV.dev)
  or a third-party scanner you enabled with your own key — report those to the relevant
  vendor.
- **Issues only reachable with attacker-controlled write access to your dotfiles, config,
  or `~/.safu` directory** (you already have a bigger problem).
- **Windows** (support is deferred; macOS and Linux only at launch).
- Missing hardening that is documented as a non-goal, or purely theoretical issues with no
  practical impact.

## Past advisories

None yet.
