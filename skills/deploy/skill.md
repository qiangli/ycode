---
name: deploy
description: Deploy ycode to localhost or remote host, ensuring a successful build first
user_invocable: true
---

# /deploy — Deploy ycode

Deploy the ycode binary to a target host and start the `ycode serve` server. Ensures `make build` succeeds before deploying. Defaults to `localhost:58080` for development unless the user specifies a different `<host>:<port>`.

## Arguments

The user may provide arguments in these forms:
- `/deploy` — deploy to localhost:58080 (default)
- `/deploy <host>` — deploy to remote host on port 58080
- `/deploy <host>:<port>` — deploy to remote host on specified port
- `/deploy :<port>` — deploy to localhost on specified port

Parse the argument accordingly. If no argument is given, use `localhost:58080`.

## Instructions

### Pre-flight: Ensure successful build

Run the `/build` skill first. This ensures the binary is compiled, tests pass, and any fixes are committed. If `/build` fails after its retry cycles, stop — do NOT deploy a broken build.

### Determine target

Parse the user's argument to determine:
- **HOST**: defaults to `localhost`
- **PORT**: defaults to `58080`
- **IS_REMOTE**: true if HOST is not `localhost` and not `127.0.0.1`

---

### For localhost deployment

#### Step 1: Kill existing instances

```bash
# Find and kill any existing ycode serve processes on the target port
lsof -ti :${PORT} | xargs kill -TERM 2>/dev/null || true
sleep 1
# Force kill if still running
lsof -ti :${PORT} | xargs kill -9 2>/dev/null || true
```

Also check for a PID file:
```bash
PID_FILE="$HOME/.ycode/serve.pid"
if [ -f "$PID_FILE" ]; then
    kill -TERM $(cat "$PID_FILE") 2>/dev/null || true
    rm -f "$PID_FILE"
fi
```

#### Step 2: Start server

```bash
bin/ycode serve --port ${PORT} --detach
```

#### Step 3: Verify

Wait 2 seconds, then check health:
```bash
curl -sf http://127.0.0.1:${PORT}/healthz
```

If health check fails, check the serve log for errors:
```bash
cat ~/.ycode/observability/serve.log | tail -20
```

Report success with the URL: `http://localhost:${PORT}/`

---

### For remote host deployment

#### Step 1: Check SSH connectivity

```bash
ssh -o BatchMode=yes -o ConnectTimeout=5 ${HOST} "echo ok" 2>&1
```

If this fails, the user needs to set up passwordless SSH. Guide them through it:

1. **Check if they have an SSH key:**
   ```bash
   ls -la ~/.ssh/id_*.pub
   ```

2. **If no key exists, generate one:**
   ```bash
   ssh-keygen -t ed25519 -C "ycode-deploy" -f ~/.ssh/id_ed25519 -N ""
   ```

3. **Copy the public key to the remote host:**
   ```bash
   ssh-copy-id ${HOST}
   ```
   This will prompt for the remote password one time.

4. **Verify the connection now works:**
   ```bash
   ssh -o BatchMode=yes ${HOST} "echo ok"
   ```

5. If it still fails, suggest checking:
   - `~/.ssh/config` for host aliases
   - Firewall rules on the remote host
   - That `sshd` is running on the remote host

**Stop here** after SSH setup — ask the user to run `/deploy ${HOST}:${PORT}` again once SSH is working.

#### Step 2: Detect remote architecture

```bash
REMOTE_OS=$(ssh ${HOST} "uname -s" | tr '[:upper:]' '[:lower:]')
REMOTE_ARCH=$(ssh ${HOST} "uname -m")
```

Map `REMOTE_ARCH`: `x86_64` → `amd64`, `aarch64` → `arm64`.

If the remote platform differs from the local one, cross-compile:
```bash
GOOS=${REMOTE_OS} GOARCH=${REMOTE_ARCH} go build -ldflags "-X main.version=$(git describe --tags --always --dirty) -X main.commit=$(git rev-parse --short HEAD)" -o bin/ycode-${REMOTE_OS}-${REMOTE_ARCH} ./cmd/ycode/
```

Use the cross-compiled binary for upload.

#### Step 3: Upload binary

```bash
ssh ${HOST} "mkdir -p ~/ycode/bin"
scp bin/ycode${SUFFIX} ${HOST}:~/ycode/bin/ycode
ssh ${HOST} "chmod +x ~/ycode/bin/ycode"
```

Where `${SUFFIX}` is `-${REMOTE_OS}-${REMOTE_ARCH}` if cross-compiled, empty otherwise.

#### Step 4: Kill existing instances on remote

```bash
ssh ${HOST} "lsof -ti :${PORT} | xargs kill -TERM 2>/dev/null; sleep 1; lsof -ti :${PORT} | xargs kill -9 2>/dev/null; rm -f ~/.ycode/serve.pid; true"
```

#### Step 5: Start server on remote

```bash
ssh ${HOST} "cd ~/ycode && nohup bin/ycode serve --port ${PORT} > ~/.ycode/serve.log 2>&1 & echo \$!"
```

#### Step 6: Verify

Wait 3 seconds, then:
```bash
ssh ${HOST} "curl -sf http://127.0.0.1:${PORT}/healthz"
```

Report success with the URL: `http://${HOST}:${PORT}/`

---

### On failure

- Show the exact error output.
- For SSH failures: guide through passwordless SSH setup as described above.
- For port conflicts: show what process is using the port (`lsof -i :${PORT}`).
- For build failures: the `/build` skill should have already handled this — report what `/build` reported.
