// Package ycodecli is ycode's command line — the engine that drives agents
// declared in YAML — as a library: cmd/ycode is a thin wrapper around Main,
// and bashy mounts the same Main as `bashy ycode`.
package ycodecli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/qiangli/ycode/examples"
	"github.com/qiangli/ycode/internal/buildinfo"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/yoke/pkg/telemetry"
	"gopkg.in/yaml.v3"
)

// Build identity; cmd/ycode stamps it from its -ldflags via SetBuild.
var (
	version = "dev"
	commit  = "unknown"
)

// SetBuild records the build identity the CLI reports.
func SetBuild(v, c string) {
	if v != "" {
		version = v
	}
	if c != "" {
		commit = c
	}
}

// Main runs the ycode CLI with args (without the program name) and returns
// the process exit status.
func Main(args []string) int {
	buildinfo.Set(version, commit)
	if err := realMain(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return 1
	}
	return 0
}

func realMain(args []string) error {
	shutdown := telemetry.Init(context.Background())
	defer func() { _ = shutdown(context.Background()) }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	doc, err := discoverCLI(args)
	if err != nil {
		return err
	}
	cmd, err := harnesscli.New(doc, dispatchCLI, harnesscli.Options{
		Version: version, Commit: commit, IsTerminal: stdinIsTerminal(), LookupEnv: os.LookupEnv,
	})
	if err != nil {
		return err
	}
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

// Discovery selects a document. The embedded authored YAML owns bootstrap
// flags and the entire offline command surface. Explicit paths fail closed.
func discoverCLI(args []string) (*harnessspec.Document, error) {
	var seed harnessspec.Document
	if err := yaml.Unmarshal(examples.Agent(), &seed); err != nil {
		return nil, err
	}
	configurationError := func(err error) error {
		return &harnesscli.Error{Class: "configuration", Code: seed.Spec.Interfaces.CLI.ExitCodes.Configuration, Prefix: seed.Spec.Interfaces.CLI.Presentation.Errors.Prefix, Err: err}
	}
	path, explicit, err := selectedConfig(seed, args)
	if err != nil {
		return nil, configurationError(err)
	}
	doc, err := harnessspec.Load(path)
	if err == nil {
		return doc, nil
	}
	if explicit || !errors.Is(err, os.ErrNotExist) {
		return nil, configurationError(err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	doc, err = harnessspec.Compile(abs, examples.Agent())
	if err != nil {
		return nil, configurationError(err)
	}
	return doc, nil
}

// HasExplicitConfig reports whether discovery must retain the caller's config
// selection. Embedders use it before supplying a built-in agent. Invalid flags
// and inaccessible local files remain with discovery so they fail closed.
func HasExplicitConfig(args []string) bool {
	var seed harnessspec.Document
	if err := yaml.Unmarshal(examples.Agent(), &seed); err != nil {
		return true
	}
	path, explicit, err := selectedConfig(seed, args)
	if err != nil || explicit {
		return true
	}
	_, err = os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

func selectedConfig(seed harnessspec.Document, args []string) (string, bool, error) {
	bootstrap := seed.Spec.Interfaces.CLI.Bootstrap
	path, explicit := bootstrap.DefaultFile, false
	profile := ""
	if bootstrap.Env != "" {
		if value, ok := os.LookupEnv(bootstrap.Env); ok && value != "" {
			path, explicit = value, true
		}
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		name, value, hasValue := strings.Cut(args[i], "=")
		if candidate, ok := bootstrap.ProfileFlags[name]; ok {
			enabled := true
			if hasValue {
				var err error
				enabled, err = strconv.ParseBool(value)
				if err != nil {
					return "", true, fmt.Errorf("%s requires a boolean value", name)
				}
			}
			if enabled {
				if profile != "" && profile != candidate {
					return "", true, errors.New("configuration profile flags conflict")
				}
				profile = candidate
			} else if profile == candidate {
				profile = ""
			}
			continue
		}
		selected := false
		for _, flag := range bootstrap.ConfigFlags {
			if args[i] == flag {
				if i+1 == len(args) {
					return "", true, fmt.Errorf("%s requires a value", flag)
				}
				i++
				path, explicit, selected = args[i], true, true
				break
			}
			if strings.HasPrefix(args[i], flag+"=") {
				path, explicit, selected = strings.TrimPrefix(args[i], flag+"="), true, true
				break
			}
			if !strings.HasPrefix(flag, "--") && strings.HasPrefix(args[i], flag) && len(args[i]) > len(flag) {
				path, explicit, selected = strings.TrimPrefix(args[i], flag), true, true
				break
			}
		}
		if selected {
			continue
		}
		// A value belonging to another authored flag cannot select a config.
		// In particular, shell code and docs search text may begin with -f.
		if consumesCLIValue(seed.Spec.Interfaces.CLI.Root, args[i]) && i+1 < len(args) {
			i++
		}
	}
	if profile != "" {
		if explicit {
			return "", true, errors.New("configuration profile flag cannot be combined with --file or the configuration environment variable")
		}
		return profile, true, nil
	}
	return path, explicit, nil
}

func consumesCLIValue(command harnessspec.CLICommand, arg string) bool {
	for _, flag := range command.Flags {
		if (arg == "--"+flag.Name || flag.Shorthand != "" && arg == "-"+flag.Shorthand) && flag.Type != "bool" {
			return true
		}
	}
	for _, child := range command.Commands {
		if consumesCLIValue(child, arg) {
			return true
		}
	}
	return false
}

func encodePrompt(text string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("prompt is empty")
	}
	return json.Marshal(map[string]string{"request": text})
}
