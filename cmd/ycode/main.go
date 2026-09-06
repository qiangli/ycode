package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/qiangli/coreutils/pkg/telemetry"
	"github.com/qiangli/ycode/internal/buildinfo"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
)

var harnessFile = "agent.yaml"

func main() {
	buildinfo.Set(version, commit)
	if maybeHandleShellCmd() {
		return
	}
	if err := realMain(); err != nil {
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func realMain() error {
	shutdown := telemetry.Init(context.Background())
	defer func() { _ = shutdown(context.Background()) }()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return rootCmd.ExecuteContext(ctx)
}

var rootCmd = &cobra.Command{
	Use:           "ycode [prompt]",
	Short:         "Run the YAML-native ycode agent harness",
	Args:          cobra.ArbitraryArgs,
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := openHarnessApplication(harnessFile)
		if err != nil {
			return err
		}
		defer app.Close()
		if len(args) != 0 {
			return app.RunText(cmd.Context(), "one-shot", strings.Join(args, " "), cmd.OutOrStdout())
		}
		if !stdinIsTerminal() {
			return app.RunReader(cmd.Context(), "one-shot", cmd.InOrStdin(), cmd.OutOrStdout())
		}
		return app.RunREPL(cmd.Context(), "tui", cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

var promptCmd = &cobra.Command{
	Use:   "prompt [message]",
	Short: "Submit one prompt through the configured one-shot frontend",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app, err := openHarnessApplication(harnessFile)
		if err != nil {
			return err
		}
		defer app.Close()
		return app.RunText(cmd.Context(), "one-shot", strings.Join(args, " "), cmd.OutOrStdout())
	},
}

var replCmd = &cobra.Command{
	Use:   "repl",
	Short: "Run the configured line-oriented REPL frontend",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		app, err := openHarnessApplication(harnessFile)
		if err != nil {
			return err
		}
		defer app.Close()
		return app.RunREPL(cmd.Context(), "repl", cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "ycode %s (%s)\n", version, commit)
		return err
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Strictly compile agent.yaml and report provider readiness",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		doc, err := loadHarness(harnessFile)
		if err != nil {
			return fmt.Errorf("doctor: compile harness: %w", err)
		}
		message, ready := harnessCredentialStatus(doc)
		status := "BLOCKED"
		if ready {
			status = "READY"
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "provider\t%s\t%s\nconfig\tREADY\t%s (%s)\n", status, message, doc.Source, doc.ConfigDigest)
		return err
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&harnessFile, "file", "f", "agent.yaml", "strict harness configuration")
	rootCmd.AddCommand(promptCmd, replCmd, versionCmd, doctorCmd, newACPCmd())
	rootCmd.AddCommand(newModelCmd(), newSkillCmd(), newConfigCmd(), newMemoryCmd(), newToolsCmd())
	rootCmd.AddCommand(newFeaturesCmd(), newDocsCmd(), newHarnessValidateCmd(), newHarnessSchemaCmd())
	rootCmd.AddCommand(newShellCmd(), serveCmd)
}

func encodePrompt(text string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("prompt is empty")
	}
	return json.Marshal(map[string]string{"request": text})
}
