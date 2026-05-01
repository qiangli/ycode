---
name: audit
description: Run security and compliance audits — external dependencies, licenses, telemetry
user_invocable: true
---

# /audit — Security & Compliance Audit

Run three audit checks against the ycode codebase and report findings. Fix violations or flag them for review.

## ARGS

Optional: `deps`, `licenses`, `telemetry`, or `all` (default: `all`).

- `/audit` or `/audit all` — run all three checks
- `/audit deps` — external binary dependency check only
- `/audit licenses` — license compliance check only
- `/audit telemetry` — third-party telemetry/phone-home check only

---

## Check 1: External Binary Dependencies

**Goal:** Verify ycode requires zero external binaries at runtime for its core agent loop.

### Procedure

1. Search all production code (exclude `external/`, `priorart/`, `*_test.go`) for:
   - `exec.Command` and `exec.CommandContext` calls
   - `os/exec` imports

2. For each finding, classify:
   - **Required** — no fallback exists, core functionality breaks without it
   - **Optional** — has graceful degradation (fallback to container, native impl, or skip)
   - **User-triggered** — only invoked by explicit user action (test runners, browser open)

3. Report format:
   ```
   EXTERNAL BINARY AUDIT
   =====================
   Required:     <count> (should be 0 for core agent loop)
   Optional:     <count>
   User-only:    <count>

   Details:
   [OK]   <binary> — <file>:<line> — <fallback description>
   [FLAG] <binary> — <file>:<line> — NO FALLBACK
   ```

4. **Pass criteria:** Zero required external binaries for the core agent loop (conversation runtime → tool dispatch → response). The `sh` binary for TTY and `git` for gitserver are acceptable as optional with container fallback.

---

## Check 2: License Compliance

**Goal:** Verify all dependencies use permissive licenses (MIT, Apache-2.0, BSD, ISC, MPL-2.0 only). Flag any GPL, AGPL, SSPL, CPAL, or unknown licenses.

### Procedure

1. Scan all module dependencies:
   ```bash
   go list -m -json all 2>/dev/null
   ```

2. For each module, read LICENSE/COPYING file and classify:
   - MIT, Apache-2.0, BSD, ISC, MPL-2.0, CC0, Unlicense → PASS
   - GPL, AGPL, SSPL, CPAL, or any copyleft → FAIL
   - Unknown/missing → FLAG for manual review

3. Also scan vendored `external/*/LICENSE` files directly.

4. Report format:
   ```
   LICENSE AUDIT
   =============
   Total modules:  <count>
   Permissive:     <count>
   Flagged:        <count>

   Breakdown:
   Apache-2.0: <n>  MIT: <n>  BSD: <n>  MPL-2.0: <n>  ISC: <n>  Other: <n>

   Violations:
   [FAIL] <module>@<version> — <license type>
   [FLAG] <module>@<version> — unknown license (manual review needed)
   ```

5. **Pass criteria:** Zero GPL/AGPL/SSPL/CPAL licenses. All modules must have identifiable permissive licenses.

---

## Check 3: Telemetry & Phone-Home

**Goal:** Verify no third-party dependency sends telemetry, usage data, or makes network calls to external services without explicit user configuration.

### Procedure

1. Search vendored `external/` directories for:
   - Hardcoded external URLs (telemetry services, analytics endpoints)
   - Update checkers (version check endpoints)
   - Phone-home patterns (device ID, usage reporting)
   - Known services: datadog, newrelic, sentry, bugsnag, honeycomb, segment, mixpanel, posthog

2. For each finding, determine if ycode's integration triggers it:
   - Check if the package/function is imported in `internal/` or `cmd/`
   - Check if the code path is reachable from ycode's initialization

3. Check ycode's own telemetry configuration:
   - Verify OTEL exports only to local files by default
   - Verify no external endpoints are hardcoded in production code

4. Report format:
   ```
   TELEMETRY AUDIT
   ================
   External endpoints in vendored code:  <count>
   Triggered by ycode:                   <count>
   User data exposed:                    <count>

   Details:
   [SAFE]    <source> — <endpoint> — not triggered by ycode
   [OK]      <source> — <endpoint> — triggered but no user data (read-only)
   [WARNING] <source> — <endpoint> — sends user data to external service
   [FAIL]    <source> — <endpoint> — sends user data without opt-in
   ```

5. **Pass criteria:** Zero endpoints that send user data to external services without explicit user opt-in. Read-only metadata calls (e.g., version checks) are acceptable if they don't transmit identifying information.

---

## Check 4: Vulnerability Scan

**Goal:** Verify no known CVEs in dependencies.

### Procedure

1. Run Go vulnerability checker:
   ```bash
   govulncheck ./...
   ```

2. If `govulncheck` is not installed, fall back to:
   ```bash
   go list -m -json all | grep -i '"Path"' | # check against known vuln databases
   ```

3. Report format:
   ```
   VULNERABILITY AUDIT
   ====================
   Total modules:    <count>
   Vulnerabilities:  <count>

   [VULN] <module>@<version> — <CVE-ID> — <description> — <fix version>
   ```

4. **Pass criteria:** Zero vulnerabilities with known exploits affecting ycode's usage. Informational/disputed CVEs may be accepted with justification.

---

## Check 5: Secrets & Credentials

**Goal:** Verify no hardcoded secrets, API keys, tokens, or credentials in source code.

### Procedure

1. Search all Go source files (excluding `external/`, `priorart/`, `*_test.go`) for:
   - Patterns: `sk-ant-`, `sk-`, `gho_`, `ghp_`, `AKIA`, `password\s*=\s*"`, `secret\s*=\s*"`, `token\s*=\s*"`
   - High-entropy strings (base64-encoded, 32+ chars of mixed case/digits)
   - Hardcoded URLs with embedded credentials (`https://user:pass@`)

2. Also scan:
   - `.env` files checked into git
   - Config files with credential-like values
   - YAML/JSON with `password`, `secret`, `token`, `key` fields containing non-placeholder values

3. Report format:
   ```
   SECRETS AUDIT
   ==============
   Potential secrets found:  <count>

   [FLAG] <file>:<line> — <pattern matched> — <context>
   [OK]   No hardcoded secrets found in production code
   ```

4. **Pass criteria:** Zero hardcoded secrets in production code. Test fixtures with obviously fake values (e.g., `"test-token"`, `"sk-ant-test"`) are acceptable.

---

## Check 6: Build Integrity

**Goal:** Verify the binary builds correctly without CGO and cross-compiles to all target platforms.

### Procedure

1. Verify CGO-free build:
   ```bash
   CGO_ENABLED=0 go build -tags "sqlite,sqlite_unlock_notify,bindata,containers_image_openpgp" -o /dev/null ./cmd/ycode/
   ```

2. Verify cross-compilation (if `make cross` exists):
   ```bash
   make cross
   ```

3. Verify go.sum integrity:
   ```bash
   go mod verify
   ```

4. Report format:
   ```
   BUILD INTEGRITY AUDIT
   ======================
   CGO_ENABLED=0 build:  PASS/FAIL
   go mod verify:        PASS/FAIL
   Cross-compile:        PASS/FAIL (or SKIP if not configured)
   ```

5. **Pass criteria:** Binary builds without CGO. Module checksums verify. Cross-compilation succeeds for all configured platforms.

---

## Output

After running all checks, update `docs/architecture.md` if any findings differ from the documented state. Summarize:

```
AUDIT SUMMARY
=============
External Dependencies: PASS/FAIL
License Compliance:    PASS/FAIL
Telemetry Safety:      PASS/FAIL

Last audited: <date>
Modules scanned: <count>
```

If all checks pass, report success. If any check fails, list the violations and suggest fixes.
