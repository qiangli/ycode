package selfheal

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

// panicHooks are invoked synchronously before the panic recovery path
// proceeds, so subsystems with buffered exporters (OTEL batch log/span
// processors, in particular) can flush their state to durable storage
// before the process exits. Without this, the panic's slog calls are
// lost — exactly the moment they're most needed.
var (
	panicHooksMu sync.Mutex
	panicHooks   []func()
)

// RegisterPanicHook registers fn to run synchronously when WrapMain's
// recovery path traps a panic. Hooks run in registration order, before
// healing is attempted. Safe for concurrent registration; not intended
// for de-registration.
func RegisterPanicHook(fn func()) {
	if fn == nil {
		return
	}
	panicHooksMu.Lock()
	panicHooks = append(panicHooks, fn)
	panicHooksMu.Unlock()
}

func runPanicHooks() {
	panicHooksMu.Lock()
	hooks := append([]func(){}, panicHooks...)
	panicHooksMu.Unlock()
	for _, fn := range hooks {
		func() {
			defer func() { _ = recover() }()
			fn()
		}()
	}
}

// MainFunc is the type of the main function to wrap.
type MainFunc func() error

// WrapMainOptions configures the self-healing wrapper.
type WrapMainOptions struct {
	// Config controls healing behavior. Defaults to DefaultConfig().
	Config *Config
	// Provider enables AI-driven healing when set.
	Provider api.Provider
	// AIConfig controls AI-specific options. Defaults to DefaultAIConfig().
	AIConfig *AIConfig
}

// WrapMain wraps a main function with self-healing capabilities.
// If the main function returns an error, it attempts to heal and restart.
//
// Usage in cmd/ycode/main.go:
//
//	func main() {
//		os.Exit(selfheal.WrapMain(realMain, nil))
//	}
//
//	func realMain() error {
//		// ... actual main logic
//		return nil
//	}
func WrapMain(mainFn MainFunc, cfg *Config) int {
	return WrapMainWithOptions(mainFn, &WrapMainOptions{Config: cfg})
}

// WrapMainWithOptions wraps a main function with self-healing, including
// optional AI-driven healing via a provider.
func WrapMainWithOptions(mainFn MainFunc, opts *WrapMainOptions) int {
	if opts == nil {
		opts = &WrapMainOptions{}
	}
	cfg := opts.Config
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Check if this is a restart after healing
	if os.Getenv("YCODE_SELF_HEAL_RESTART") == "1" {
		fmt.Fprintln(os.Stderr, "ycode restarted after self-healing")
		os.Unsetenv("YCODE_SELF_HEAL_RESTART")
	}

	healer := NewHealer(cfg)

	// Wire up AI healer if a provider is available
	if opts.Provider != nil {
		aiCfg := opts.AIConfig
		if aiCfg == nil {
			aiCfg = DefaultAIConfig()
			aiCfg.Config = cfg
		}
		aiHealer := NewAIHealer(aiCfg, opts.Provider)
		healer.SetAIHealer(aiHealer)
	}

	// Run the main function with panic recovery
	err := runWithRecovery(mainFn, healer)

	if err == nil {
		return 0
	}

	// Decide upfront whether healing will actually be attempted. When it
	// won't (e.g. build error but no AI provider configured), we skip the
	// "Error encountered" / "Attempting self-healing..." / "cannot be
	// automatically healed" ceremony and just surface the error verbatim.
	// Those ceremony lines were the user-visible noise on every podman
	// build failure when no AI provider is wired up.
	if !healer.CanHeal(err) {
		switch ClassifyError(err) {
		case FailureTypeNotInstalled:
			// Verbatim message; the error already tells the user to
			// reinstall ycode. No "cannot be healed" noise.
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		case FailureTypePortInUse:
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintln(os.Stderr, "\nA port required by ycode is already in use. Options:")
			fmt.Fprintln(os.Stderr, "  - `ycode serve stop` (graceful — needs the pidfile)")
			fmt.Fprintln(os.Stderr, "  - `lsof -nP -iTCP:<port> -sTCP:LISTEN` to find the PID, then kill it")
			fmt.Fprintln(os.Stderr, "  - reconfigure the port in settings.json or set it negative to allocate ephemerally")
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Healing path — chatty by design; the user opted in by configuring
	// an AI provider.
	fmt.Fprintf(os.Stderr, "\nError encountered: %v\n", err)
	fmt.Fprintln(os.Stderr, "Attempting self-healing...")

	errInfo := ErrorInfo{
		Type:      ClassifyError(err),
		Error:     err,
		Message:   err.Error(),
		Context:   make(map[string]string),
		Timestamp: time.Now(),
	}

	// Get stack trace if available
	if stack := debug.Stack(); len(stack) > 0 {
		errInfo.StackTrace = string(stack)
	}

	ctx := context.Background()
	success, healErr := healer.AttemptHealing(ctx, errInfo)

	if !success {
		fmt.Fprintf(os.Stderr, "Self-healing failed: %v\n", healErr)

		switch cfg.EscalationPolicy {
		case EscalationPolicyAsk:
			fmt.Fprintln(os.Stderr, "\nHealing failed. Original error:")
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		case EscalationPolicyLog:
			fmt.Fprintf(os.Stderr, "[LOG] Healing failed: %v\n", err)
			return 1
		case EscalationPolicyAbort:
			fmt.Fprintln(os.Stderr, "Healing failed, aborting.")
			return 2
		}
		return 1
	}

	fmt.Fprintln(os.Stderr, "Self-healing succeeded!")

	if cfg.AutoRestart {
		fmt.Fprintln(os.Stderr, "Restarting ycode...")
		if restartErr := healer.Restart(); restartErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to restart: %v\n", restartErr)
			return 1
		}
		// If we get here, restart didn't work as expected
		return 1
	}

	return 0
}

// runWithRecovery runs the main function and recovers from panics.
func runWithRecovery(mainFn MainFunc, healer *Healer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// Convert panic to error
			var ok bool
			if err, ok = r.(error); !ok {
				err = fmt.Errorf("panic: %v", r)
			}

			// Add stack trace
			stack := debug.Stack()
			errInfo := ErrorInfo{
				Type:       FailureTypeRuntime,
				Error:      err,
				Message:    err.Error(),
				StackTrace: string(stack),
				Context:    make(map[string]string),
				Timestamp:  time.Now(),
			}

			// Print the stack trace to stderr immediately so it lands
			// in the operator's log regardless of whether healing
			// succeeds or whether buffered OTEL exporters can flush.
			fmt.Fprintf(os.Stderr, "\nPanic recovered: %v\n%s\n", r, stack)

			// Flush any registered exporters (OTEL log/span batches,
			// etc.) before healing or exit. The panic interrupts the
			// goroutine running mainFn but the BatchLogProcessor's
			// queued records would otherwise be dropped with the
			// process.
			runPanicHooks()

			// Try to heal the panic
			if healer.CanHeal(err) {
				fmt.Fprintln(os.Stderr, "Attempting self-healing...")

				ctx := context.Background()
				if success, healErr := healer.AttemptHealing(ctx, errInfo); success {
					fmt.Fprintln(os.Stderr, "Self-healing succeeded!")
					if healer.Config().AutoRestart {
						fmt.Fprintln(os.Stderr, "Restarting ycode...")
						if restartErr := healer.Restart(); restartErr != nil {
							err = fmt.Errorf("healing succeeded but restart failed: %w", restartErr)
						} else {
							err = nil
						}
					}
				} else {
					err = fmt.Errorf("panic: %v (healing failed: %w)", r, healErr)
				}
			}
		}
	}()

	return mainFn()
}

// RunWithHealing runs a function with self-healing enabled.
// This is useful for wrapping specific operations that might fail.
func RunWithHealing(ctx context.Context, fn func() error, cfg *Config) error {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	healer := NewHealer(cfg)

	err := fn()
	if err == nil {
		return nil
	}

	if !healer.CanHeal(err) {
		return err
	}

	errInfo := ErrorInfo{
		Type:      ClassifyError(err),
		Error:     err,
		Message:   err.Error(),
		Context:   make(map[string]string),
		Timestamp: time.Now(),
	}

	success, healErr := healer.AttemptHealing(ctx, errInfo)
	if !success {
		return fmt.Errorf("%w (healing failed: %w)", err, healErr)
	}

	if cfg.AutoRestart {
		if restartErr := healer.Restart(); restartErr != nil {
			return fmt.Errorf("healing succeeded but restart failed: %w", restartErr)
		}
	}

	return nil
}
