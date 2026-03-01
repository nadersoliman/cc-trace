# CI + README Badges Design

## Overview

Add GitHub Actions CI and dynamic README badges to cc-trace. All metrics computed in CI and published to a GitHub Gist for shields.io dynamic endpoint badges. Add MIT license.

## Badge Set

| Badge | Source | Type |
|-------|--------|------|
| CI status | GitHub Actions built-in | Dynamic |
| Coverage | `go test -cover` → gist | Dynamic |
| Lines of code | `wc -l` on source files → gist | Dynamic |
| Test ratio | test LOC / source LOC → gist | Dynamic |
| Go version | parsed from `go.mod` → gist | Dynamic |
| License | MIT | Static |
| Go Report Card | goreportcard.com | On-demand |

## GitHub Actions Workflow

`.github/workflows/ci.yml` triggers on push to `main` and pull requests.

### Steps

1. Checkout + setup Go
2. `go build ./...`
3. `go vet ./...`
4. `go test -race -cover -coverprofile=coverage.out ./...`
5. Parse metrics: coverage %, source LOC, test LOC, test ratio, Go version
6. Push badge data to a GitHub Gist via `schneegans/dynamic-badges-action`

### Gist Structure

One gist with multiple JSON files, each in shields.io endpoint format:

```json
{"schemaVersion":1,"label":"coverage","message":"75.6%","color":"green"}
```

Files: `coverage.json`, `loc.json`, `test-ratio.json`, `go-version.json`

### Auth

Requires `GIST_SECRET` repo secret (PAT with `gist` scope).

## README Badge Row

Badges placed at the top of README.md, after the title:

```markdown
[![CI](actions-badge-url)](actions-url)
[![Coverage](endpoint-url)](...)
[![Go Report Card](goreportcard-badge)](goreportcard-url)
[![Lines of Code](endpoint-url)](...)
[![Test Ratio](endpoint-url)](...)
[![Go Version](endpoint-url)](...)
[![License: MIT](static-badge)](LICENSE)
```

## License

MIT license file at repo root (`LICENSE`).
