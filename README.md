<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License">
  <img src="https://img.shields.io/badge/Node--aware-brightgreen" alt="Node Aware">
</p>

# packwatch

An interactive CLI that checks your `package-lock.json` for outdated npm packages, scans for security vulnerabilities, and lets you pick which ones to update — all from your terminal. Node-version-aware: if the latest version of a package requires a newer Node than what's active, it suggests the best compatible version instead.

## What it does

1. Detects your active Node version (`.nvmrc` → `.node-version` → `node --version`)
2. Reads `.npmrc` for custom registry configuration (scoped registries, auth tokens)
3. Parses `package-lock.json` (v1/v2/v3) and `package.json` for direct dependencies
4. Fetches the npm registry concurrently for newer versions
5. Checks security advisories via the GitHub Advisory Database
6. Presents an interactive TUI to select packages for update
7. Generates and optionally executes `npm install` commands (splitting prod/dev)

## Installation

### Homebrew (macOS / Linux)

```bash
brew install jee4nc/tap/packwatch
```

### Scoop (Windows)

```powershell
scoop bucket add packwatch https://github.com/jee4nc/scoop-bucket
scoop install packwatch
```

### Debian / Ubuntu (.deb)

Download the `.deb` package from the [Releases](../../releases) page:

```bash
sudo dpkg -i packwatch_*.deb
```

### Pre-built binaries

Download from the [Releases](../../releases) page for your platform:

| Platform         | Archive                         |
|------------------|---------------------------------|
| macOS arm64      | `packwatch_darwin_arm64.tar.gz` |
| macOS amd64      | `packwatch_darwin_amd64.tar.gz` |
| Linux arm64      | `packwatch_linux_arm64.tar.gz`  |
| Linux amd64      | `packwatch_linux_amd64.tar.gz`  |
| Windows amd64    | `packwatch_windows_amd64.zip`   |
| Windows arm64    | `packwatch_windows_arm64.zip`   |

### From source

```bash
# Build and install to $GOPATH/bin
make install

# Or just build
make build
```

## Requirements

- Node.js installed on your system
- A `package-lock.json` in the current directory

## Quick start

```bash
# Run in any Node.js project
packwatch

# Only check production dependencies
packwatch --prod-only

# Only check dev dependencies
packwatch --dev-only

# Output as JSON (no interactive TUI)
packwatch --json

# Disable colors
packwatch --no-color

# Show version
packwatch --version
```

## Example output

```
  📦 packwatch v1.0.0

  ⬢  Node 20.11.0  from .nvmrc
  🔒 Lockfile v3 — 42 direct dependencies

  🔍 Checking npm registry for 42 packages...
  ████████████████████████████████ 42/42

  🛡️  Checking security advisories for 42 packages...
  ████████████████████████████████ 42/42

  📊 5 updates available · 2 vulnerable (1 HIGH, 1 MEDIUM)

  ┌─────────────────────────────────────────────────────────┐
  │  ▶ express          4.18.2 → 4.21.0     minor          │
  │    lodash           4.17.20 → 4.17.21   patch   🛡️ HIGH│
  │    axios            1.6.0 → 1.7.9       minor          │
  │    typescript       5.3.3 → 5.7.2       minor     dev  │
  │    webpack          5.89.0 → 5.97.0     minor     dev  │
  │  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │
  │    react            18.3.1               up-to-date     │
  └─────────────────────────────────────────────────────────┘
```

## Node version awareness

When the latest version of a package requires a newer Node than what's active, packwatch won't blindly suggest it. Instead, it walks versions newest-to-oldest and suggests the best one compatible with your Node:

```
  ⚠️  next  14.2.3 → 15.1.0
     latest (15.1.0) requires Node >=18.18.0; suggesting 14.2.28 instead
```

## JSON mode

Use `--json` for CI pipelines or scripting:

```bash
# List all vulnerable packages
packwatch --json | jq '.packages[] | select(.vulnCount > 0)'

# List major updates only
packwatch --json | jq '.packages[] | select(.updateType=="major")'
```

## Build from source

```bash
# Build for current platform
make build

# Run tests
make test

# Lint (fmt + vet)
make lint

# Cross-compile for all platforms
make release
```

Binaries are output to `bin/` with optimized flags (`-s -w`) for minimal size.

## License

[MIT](LICENSE)
