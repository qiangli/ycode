---
name: build
description: Build ycode binary with full quality checks (tidy, fmt, vet, unit tests)
user_invocable: true
---

# /build — Build ycode

Build the ycode binary from source with full quality gate checks. This skill works in both serve mode and client mode.

## Instructions

Run the following steps **sequentially** in the project root `/Users/qiangli/projects/poc/ai/ycode`. Stop on the first failure — do NOT continue to later steps if an earlier one fails. Report the failure clearly and suggest a fix.

### Step 1: Dependency hygiene

```bash
go mod tidy
```

If `go.mod` or `go.sum` changed, report the diff so the user is aware.

### Step 2: Format

```bash
go fmt $(go list ./... | grep -v '/priorart/')
```

Report any files that were reformatted.

### Step 3: Static analysis

```bash
go vet $(go list ./... | grep -v '/priorart/')
```

If vet reports issues, show them and stop.

### Step 4: Unit tests

```bash
go test -race -count=1 $(go list ./... | grep -v '/priorart/')
```

Run with race detector enabled. If any test fails, show the failure output and stop.

### Step 5: Build binary

```bash
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o bin/ycode ./cmd/ycode/
```

### Step 6: Verify

Confirm the binary exists and print its version:

```bash
bin/ycode version
```

### On success

Report a one-line summary: binary path, version, test count, and total elapsed time.

### On failure

- Show the exact error output from the failing step.
- If the error is a common issue (missing dependency, syntax error, failing test), suggest the fix.
- Do NOT proceed to later steps.
