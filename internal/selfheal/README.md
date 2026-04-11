# Self-Heal Package

The `selfheal` package provides self-healing capabilities for ycode. When ycode encounters errors, instead of immediately exiting, the self-heal system can attempt to diagnose and fix the problem, then rebuild and restart the application if needed.

## Features

- **Automatic error classification**: Categorizes errors into build, runtime, config, API, and tool errors
- **Recovery attempts**: Multiple healing attempts with configurable limits
- **Auto-rebuild**: Automatically rebuild after code fixes
- **Auto-restart**: Process replacement on Unix systems for seamless recovery
- **Panic recovery**: Catches panics and attempts recovery
- **Escalation policies**: Define behavior when healing fails

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Error     │────▶│  Healer     │────▶│  Diagnose   │
│  Occurs     │     │  (WrapMain) │     │  & Classify │
└─────────────┘     └─────────────┘     └──────┬──────┘
                                               │
                         ┌─────────────────────┼─────────────────────┐
                         │                     │                     │
                         ▼                     ▼                     ▼
                  ┌─────────────┐      ┌─────────────┐      ┌─────────────┐
                  │  Build Fix  │      │ Runtime Fix │      │  Config Fix │
                  └──────┬──────┘      └──────┬──────┘      └──────┬──────┘
                         │                     │                     │
                         └─────────────────────┼─────────────────────┘
                                               │
                                               ▼
                                        ┌─────────────┐
                                        │   Rebuild   │
                                        └──────┬──────┘
                                               │
                                               ▼
                                        ┌─────────────┐
                                        │   Restart   │
                                        └─────────────┘
```

## Usage

### Automatic Self-Healing

Self-healing is enabled by default. When an error occurs, ycode will:

1. Classify the error type
2. Attempt to heal (up to `MaxAttempts` times)
3. Rebuild if necessary
4. Restart the process

Disable with environment variable:
```bash
YCODE_SELF_HEAL=0 ycode prompt "hello"
```

### CLI Commands

```bash
# View self-healing status
ycode heal status

# Test self-healing with a simulated error
ycode heal test "build failed: undefined variable"
```

### Programmatic Usage

```go
// Wrap main function with self-healing
func main() {
    os.Exit(selfheal.WrapMain(realMain, nil))
}

func realMain() error {
    // ... actual main logic
    return nil
}
```

## Configuration

```go
cfg := &selfheal.Config{
    Enabled:          true,                          // Enable self-healing
    MaxAttempts:      3,                             // Max healing attempts
    AutoRebuild:      true,                          // Rebuild after fixes
    AutoRestart:      true,                          // Restart after rebuild
    EscalationPolicy: selfheal.EscalationPolicyAsk,  // What to do on failure
    BuildCommand:     "go build -o bin/ycode ./cmd/ycode/",
    BuildTimeout:     5 * time.Minute,
    HealablePaths:    []string{"*.go", "go.mod"},
    ProtectedPaths:   []string{".git/", "vendor/"},
}
```

## Error Classification

| Error Type  | Description              | Healable |
|-------------|--------------------------|----------|
| Build       | Compilation errors       | Yes      |
| Runtime     | Panics, nil pointers     | Yes      |
| Config      | Configuration errors     | Yes      |
| API         | API connection errors    | Yes      |
| Tool        | Tool execution errors    | Yes      |
| Unknown     | Uncategorized errors     | Maybe    |

## Escalation Policies

- **`EscalationPolicyAsk`**: Prompt user for direction (default)
- **`EscalationPolicyLog`**: Log error and continue
- **`EscalationPolicyAbort`**: Exit with error code

## State Machine

```
Idle ──▶ Diagnosing ──▶ Fixing ──▶ Rebuilding ──▶ Restarting ──▶ Succeeded
  │          │            │           │              │
  │          │            │           │              └──▶ Failed
  │          │            │           │
  │          │            │           └──▶ (if build fails) ──▶ Failed
  │          │            │
  │          │            └──▶ (if fix fails) ──▶ Failed
  │          │
  │          └──▶ (if not healable) ──▶ Failed
  │
  └──▶ Reset()
```

## Future Enhancements

The `AIHealer` struct provides a skeleton for AI-driven healing:

- AI-powered error analysis
- Automatic code generation for fixes
- Structured response parsing
- Integration with the tool system

This requires additional work to:
1. Handle streaming API responses properly
2. Parse structured AI responses
3. Execute tool calls for file modifications
4. Manage iterative fix conversations

## Testing

```bash
# Run selfheal tests
go test -race ./internal/selfheal/...

# Test with simulated errors
./bin/ycode heal test "build failed: undefined: foo"
./bin/ycode heal test "panic: runtime error: nil pointer dereference"
```

## References

This implementation is inspired by:

- **aider**: Retry logic with exponential backoff for API errors
- **claw-code**: Structured recovery recipes with failure scenarios
- **OpenHands**: Runtime recovery and state management

## License

Part of ycode - see repository root for license information.
