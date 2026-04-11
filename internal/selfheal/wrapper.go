package selfheal

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/qiangli/ycode/internal/api"
)

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

	// Try to heal the error
	fmt.Fprintf(os.Stderr, "\nError encountered: %v\n", err)

	if !healer.CanHeal(err) {
		fmt.Fprintf(os.Stderr, "This error cannot be automatically healed.\n")
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

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

			// Try to heal the panic
			if healer.CanHeal(err) {
				fmt.Fprintf(os.Stderr, "\nPanic recovered: %v\n", r)
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
