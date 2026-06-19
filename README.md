# safu

A single, statically-linked Go binary: a safer, smarter shell. It guards destructive
commands, audits packages before you install them, speeds up navigation, keeps a
plain-text activity log, and helps you fix the command you just fumbled.

**Local-first: no account, no telemetry, no servers we operate.** safu makes exactly two
kinds of outbound call — an opt-out update check and the package audits you ask for — and
both can be turned off.

## Install

```sh
# macOS & Linux
curl -fsSL https://safu.sh/install.sh | sh

# Homebrew
brew install StackSatoshis/tap/safu
```

The installer only places the binary. Enabling the shell integration is a separate,
explicit step:

```sh
safu init            # print the shell hook (add it to your rc, or use --write-rc)
safu setup           # interactive setup wizard
```

## What it does

- **Guard** — intercepts destructive commands (`rm -rf`, `dd`, force-push, …), previews
  what they'll do, and confirms before anything irreversible. Deletes go to a trash you can
  `safu undo`.
- **Audit** — checks packages against registry, source-repo, and OSV malicious-package
  signals before `pip`/`npm`/`cargo`/`brew` install them.
- **Navigate** — `safu z <query>` jumps to your most-used directories.
- **Fix** — `safu fix` / `wtf` suggests a correction for your last command (it never
  re-runs the failed command).
- **Log & history** — a plain-text activity log of what safu did, and an opt-in,
  fuzzy-searchable shell history.

Everything is local; the guard, logs, navigation, and history never touch the network.

## Repository layout

```
.
├── cmd/safu/    # CLI entry point
├── internal/    # implementation packages
├── go.mod       # module github.com/StackSatoshis/safu
└── site/        # static marketing site (deployed to GitHub Pages)
```

## Build from source

```sh
go build ./cmd/safu     # build
go test ./...           # run the tests
go run ./cmd/safu help  # run locally
```

## Security

Found a vulnerability? Please report it privately — see [SECURITY.md](SECURITY.md).

You can verify the privacy claim yourself: network calls live only in `internal/audit`
(package audits) and `internal/update` (the opt-out update check).

## License

[MIT](LICENSE).
