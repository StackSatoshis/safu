<div align="center">

<img src="https://raw.githubusercontent.com/StackSatoshis/safu/main/site/icon.png" width="88" alt="safu">

# safu

### Shell commands that won't nuke your machine.

A safer, smarter shell. It **guards** destructive commands, **audits** packages before you
install them, **jumps** to where you work, and **fixes** the command you just fumbled —
all on your machine.

**Local-first: no account, no telemetry, no servers we operate.**

[**safu.sh**](https://safu.sh) · [Install](#install) · [What it does](#what-it-does) · [Privacy](#privacy) · [Releases](https://github.com/StackSatoshis/safu/releases) · [Security](SECURITY.md)

[![Release](https://img.shields.io/github/v/release/StackSatoshis/safu?color=2ecc71)](https://github.com/StackSatoshis/safu/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)
![No CGO](https://img.shields.io/badge/build-static%20Go%20binary-555)

</div>

---

## Install

```sh
# Homebrew (macOS & Linux)
brew install StackSatoshis/tap/safu

# or the shell installer
curl -fsSL https://safu.sh/install.sh | sh
```

Installing only places the binary. It then **asks** whether to enable the shell integration —
or turn it on yourself anytime:

```sh
safu setup            # interactive wizard — configures safu AND wires your shell
# or, non-interactive:
safu init --write-rc  # add the hook to your shell rc (with a timestamped backup)
```

Then restart your shell. Full docs at **[safu.sh](https://safu.sh)**.

## What it does

🛡️ **Guards destructive commands.** Intercepts `rm -rf`, `dd` to a disk, force-push, and
friends; previews exactly what they'll do and confirms before anything irreversible. Deletes
go to a trash you can bring back with `safu undo`.

📦 **Audits packages before install.** Checks `pip` / `npm` / `cargo` / `brew` installs against
registry, source-repo, and OSV malicious-package signals — and blocks confirmed malware
outright.

🧭 **Jumps where you work.** `safu z <query>` learns your directories and teleports you in a
couple of keystrokes.

🩹 **Fixes fumbled commands.** `safu fix` (or `wtf`) suggests a correction for your last
command — and *never* re-runs the failed command to figure it out.

🗒️ **Keeps a plain-text trail.** Every block, soft-delete, and audit verdict is logged as
human-readable JSONL under `~/.safu`. Opt into a fuzzy, Ctrl-R shell history too.

## Privacy

safu makes exactly **two** kinds of outbound call — an **opt-out update check** and the
**package audits you ask for** (which send only a package name + version to public registries
and OSV.dev). The guard, logs, navigation, and history **never touch the network**.

You can verify that yourself:

```sh
grep -rn "http\." --include="*.go" .   # network calls live ONLY in internal/audit + internal/update
```

Third-party scanners stay off unless you enable them with your own key. See [SECURITY.md](SECURITY.md)
for the full policy and how to report a vulnerability.

## Build from source

```sh
go build ./cmd/safu     # build
go test ./...           # run the tests
go run ./cmd/safu help  # run locally
```

Single, statically-linked Go binary (no CGO), cross-compiled to darwin/linux × amd64/arm64.

## License

[MIT](LICENSE) · © 2026 safu · [safu.sh](https://safu.sh)
