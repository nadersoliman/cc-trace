# CI + README Badges Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add GitHub Actions CI workflow with dynamic README badges for coverage, LOC, test ratio, Go version, plus Go Report Card and MIT license.

**Architecture:** CI workflow runs build/test/vet, computes metrics via shell, publishes badge JSON to a GitHub Gist using `schneegans/dynamic-badges-action`. Shields.io reads from the gist dynamically. Badge markdown in README points at the shields.io endpoint URLs.

**Tech Stack:** GitHub Actions, shields.io endpoint badges, `schneegans/dynamic-badges-action@v1.7.0`, goreportcard.com

---

### Task 1: Create the GitHub Gist for Badge Data

This is a manual prerequisite — the gist must exist before CI can write to it.

**Step 1: Create a gist**

Go to https://gist.github.com and create a new gist:
- **Filename:** `coverage.json`
- **Content:** `{"schemaVersion":1,"label":"coverage","message":"pending","color":"lightgrey"}`
- Set to **Public**
- Click "Create public gist"

**Step 2: Note the gist ID**

From the URL `https://gist.github.com/nadersoliman/<GIST_ID>`, copy the alphanumeric gist ID.

**Step 3: Create a PAT with gist scope**

Go to https://github.com/settings/tokens and create a fine-grained or classic token with the `gist` scope only.

**Step 4: Add repo secret**

Go to `https://github.com/nadersoliman/cc-trace/settings/secrets/actions` and add:
- **Name:** `GIST_SECRET`
- **Value:** the token from Step 3

**Step 5: Record the gist ID**

You will need the gist ID in Task 3. Keep it handy.

---

### Task 2: Add MIT License

**Files:**
- Create: `LICENSE`

**Step 1: Create the LICENSE file**

```text
MIT License

Copyright (c) 2026 Nader Soliman

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

**Step 2: Commit**

```bash
git add LICENSE
git commit -m "Add MIT license" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Create GitHub Actions CI Workflow

**Files:**
- Create: `.github/workflows/ci.yml`

**Step 1: Create the workflow file**

Replace `YOUR_GIST_ID` with the actual gist ID from Task 1.

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  build-and-test:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Build
        run: go build ./...

      - name: Vet
        run: go vet ./...

      - name: Test
        run: |
          go test -race -cover -coverprofile=coverage.out ./... 2>&1 | tee test-output.txt
          # Extract coverage percentage
          COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
          echo "COVERAGE=${COVERAGE}" >> $GITHUB_ENV

          # Determine coverage badge color
          COV_INT=${COVERAGE%.*}
          if [ "$COV_INT" -ge 80 ]; then
            echo "COV_COLOR=brightgreen" >> $GITHUB_ENV
          elif [ "$COV_INT" -ge 60 ]; then
            echo "COV_COLOR=green" >> $GITHUB_ENV
          elif [ "$COV_INT" -ge 40 ]; then
            echo "COV_COLOR=yellow" >> $GITHUB_ENV
          else
            echo "COV_COLOR=red" >> $GITHUB_ENV
          fi

      - name: Compute metrics
        run: |
          # Source LOC (non-test .go files)
          SRC_LOC=$(cat $(find . -name '*.go' ! -name '*_test.go' -not -path './vendor/*') | wc -l | tr -d ' ')
          echo "SRC_LOC=${SRC_LOC}" >> $GITHUB_ENV

          # Test LOC
          TEST_LOC=$(cat $(find . -name '*_test.go' -not -path './vendor/*') | wc -l | tr -d ' ')
          echo "TEST_LOC=${TEST_LOC}" >> $GITHUB_ENV

          # Test ratio (test LOC / source LOC)
          if [ "$SRC_LOC" -gt 0 ]; then
            RATIO=$(awk "BEGIN {printf \"%.1f\", ${TEST_LOC}/${SRC_LOC}}")
          else
            RATIO="0.0"
          fi
          echo "TEST_RATIO=${RATIO}" >> $GITHUB_ENV

          # Go version from go.mod
          GO_VERSION=$(grep '^go ' go.mod | awk '{print $2}')
          echo "GO_VERSION=${GO_VERSION}" >> $GITHUB_ENV

      - name: Update coverage badge
        if: github.ref == 'refs/heads/main'
        uses: schneegans/dynamic-badges-action@v1.7.0
        with:
          auth: ${{ secrets.GIST_SECRET }}
          gistID: YOUR_GIST_ID
          filename: coverage.json
          label: coverage
          message: "${{ env.COVERAGE }}%"
          color: ${{ env.COV_COLOR }}

      - name: Update LOC badge
        if: github.ref == 'refs/heads/main'
        uses: schneegans/dynamic-badges-action@v1.7.0
        with:
          auth: ${{ secrets.GIST_SECRET }}
          gistID: YOUR_GIST_ID
          filename: loc.json
          label: lines of code
          message: "${{ env.SRC_LOC }}"
          color: informational

      - name: Update test ratio badge
        if: github.ref == 'refs/heads/main'
        uses: schneegans/dynamic-badges-action@v1.7.0
        with:
          auth: ${{ secrets.GIST_SECRET }}
          gistID: YOUR_GIST_ID
          filename: test-ratio.json
          label: test ratio
          message: "${{ env.TEST_RATIO }}x"
          color: informational

      - name: Update Go version badge
        if: github.ref == 'refs/heads/main'
        uses: schneegans/dynamic-badges-action@v1.7.0
        with:
          auth: ${{ secrets.GIST_SECRET }}
          gistID: YOUR_GIST_ID
          filename: go-version.json
          label: go
          message: "${{ env.GO_VERSION }}"
          color: "00ADD8"
```

**Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "Add GitHub Actions CI workflow with badge metrics" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Add Badge Row to README

**Files:**
- Modify: `README.md:1-3`

**Step 1: Add badges after the title**

Replace `YOUR_GIST_ID` with the actual gist ID from Task 1. Insert after the `# cc-trace` line:

```markdown
# cc-trace

[![CI](https://github.com/nadersoliman/cc-trace/actions/workflows/ci.yml/badge.svg)](https://github.com/nadersoliman/cc-trace/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/nadersoliman/YOUR_GIST_ID/raw/coverage.json)](https://github.com/nadersoliman/cc-trace/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/nadersoliman/cc-trace)](https://goreportcard.com/report/github.com/nadersoliman/cc-trace)
[![Lines of Code](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/nadersoliman/YOUR_GIST_ID/raw/loc.json)](https://github.com/nadersoliman/cc-trace)
[![Test Ratio](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/nadersoliman/YOUR_GIST_ID/raw/test-ratio.json)](https://github.com/nadersoliman/cc-trace)
[![Go Version](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/nadersoliman/YOUR_GIST_ID/raw/go-version.json)](https://github.com/nadersoliman/cc-trace)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "Add dynamic badges to README" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Verify End-to-End

**Step 1: Ensure gist ID is set**

Verify `YOUR_GIST_ID` has been replaced in both `.github/workflows/ci.yml` and `README.md`.

Run: `grep -r 'YOUR_GIST_ID' .github/ README.md`
Expected: No matches (all replaced)

**Step 2: Push and verify CI**

Push the branch and check that CI runs on the PR. Badge updates only happen on push to `main`, so the gist-based badges won't update until the PR is merged. The CI status badge will work immediately.

**Step 3: Trigger Go Report Card**

Visit `https://goreportcard.com/report/github.com/nadersoliman/cc-trace` once to generate the initial report. It runs automatically after that on each visit.

**Step 4: Verify badge rendering**

After merging to main and CI runs, check the README on GitHub to confirm all 7 badges render correctly.
