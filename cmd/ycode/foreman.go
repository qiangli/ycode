package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/gitserver/backlog"
)

// Boss → Foreman control channel.
//
// All Foreman lifecycle commands (start/pause/resume/stop/skip/prio/tell)
// append a single JSONL line to .agents/ycode/foreman/commands.jsonl.
// The /foreman skill body (chat agent) reads the queue between Worker
// iterations and applies the verbs. The CLI here is the Boss-side
// surface — a separate-shell or scripted invocation path that produces
// the same commands the chat agent would write when the Boss types
// instructions in-band.
//
// See docs/backlog.md for the full chain of command and `.agents/ycode/
// skills/ycode-foreman/skill.md` for the loop that consumes these
// commands.

// ForemanState is the current Foreman lifecycle state.
type ForemanState string

const (
	StateIdle    ForemanState = "idle"
	StateRunning ForemanState = "running"
	StatePaused  ForemanState = "paused"
	StateStopped ForemanState = "stopped"
)

// ForemanCommand is one line in commands.jsonl.
type ForemanCommand struct {
	ID   string         `json:"id"`
	TS   time.Time      `json:"ts"`
	Verb string         `json:"verb"`
	Args map[string]any `json:"args,omitempty"`
	From string         `json:"from"` // "cli" | "chat"
}

// ForemanStateFile is the structure of state.json.
type ForemanStateFile struct {
	State          ForemanState `json:"state"`
	CurrentIssue   *int64       `json:"current_issue,omitempty"`
	CurrentLoomID  string       `json:"current_loom_id,omitempty"`
	CurrentSlug    string       `json:"current_slug,omitempty"`
	StartedAt      *time.Time   `json:"started_at,omitempty"`
	LastCommandID  string       `json:"last_command_id,omitempty"`
	LastTransition time.Time    `json:"last_transition"`
}

// foremanDir resolves .agents/ycode/foreman/ relative to cwd, creating
// it if missing.
func foremanDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, ".agents", "ycode", "foreman")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func commandsPath(dir string) string { return filepath.Join(dir, "commands.jsonl") }
func statePath(dir string) string    { return filepath.Join(dir, "state.json") }

func newCommandID() string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// appendCommand appends one JSONL line atomically (single Write under
// O_APPEND). Concurrent Boss CLIs are safe.
func appendCommand(dir string, cmd ForemanCommand) error {
	if cmd.ID == "" {
		cmd.ID = newCommandID()
	}
	if cmd.TS.IsZero() {
		cmd.TS = time.Now().UTC()
	}
	if cmd.From == "" {
		cmd.From = "cli"
	}
	line, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(commandsPath(dir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ReadState loads state.json (or returns a default idle state if missing).
func ReadState(dir string) (ForemanStateFile, error) {
	data, err := os.ReadFile(statePath(dir))
	if os.IsNotExist(err) {
		return ForemanStateFile{State: StateIdle}, nil
	}
	if err != nil {
		return ForemanStateFile{}, err
	}
	var s ForemanStateFile
	if err := json.Unmarshal(data, &s); err != nil {
		return ForemanStateFile{}, err
	}
	return s, nil
}

// WriteState atomically writes state.json.
func WriteState(dir string, s ForemanStateFile) error {
	s.LastTransition = time.Now().UTC()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, statePath(dir))
}

// newForemanCmd builds `ycode foreman ...`.
func newForemanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "foreman",
		Short: "Boss → Foreman control channel (start/pause/resume/stop/tell/skip/prio/status)",
		Long: `The Boss controls the Foreman either in-band (chat) or via these CLI
commands. Verbs append to .agents/ycode/foreman/commands.jsonl; the
running Foreman (chat agent or daemon) applies them between iterations
or, for pause/stop/skip, mid-Worker via SIGTERM.

See docs/backlog.md and .agents/ycode/skills/ycode-foreman/skill.md.`,
	}
	cmd.AddCommand(newForemanStartCmd())
	cmd.AddCommand(newForemanPauseCmd())
	cmd.AddCommand(newForemanResumeCmd())
	cmd.AddCommand(newForemanStopCmd())
	cmd.AddCommand(newForemanStatusCmd())
	cmd.AddCommand(newForemanTellCmd())
	cmd.AddCommand(newForemanSkipCmd())
	cmd.AddCommand(newForemanPrioCmd())
	cmd.AddCommand(newForemanDaemonCmd())
	return cmd
}

func newForemanStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Signal the Foreman to start (or resume) its loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := foremanDir()
			if err != nil {
				return err
			}
			return appendCommand(dir, ForemanCommand{Verb: "start"})
		},
	}
}

func newForemanPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause after the current Worker exits (or interrupt mid-Worker via SIGTERM)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := foremanDir()
			if err != nil {
				return err
			}
			return appendCommand(dir, ForemanCommand{Verb: "pause"})
		},
	}
}

func newForemanResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume from paused state",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := foremanDir()
			if err != nil {
				return err
			}
			return appendCommand(dir, ForemanCommand{Verb: "resume"})
		},
	}
}

func newForemanStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Foreman; release any in-flight Worker claim",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := foremanDir()
			if err != nil {
				return err
			}
			return appendCommand(dir, ForemanCommand{Verb: "stop"})
		},
	}
}

func newForemanStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the current Foreman state",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := foremanDir()
			if err != nil {
				return err
			}
			s, err := ReadState(dir)
			if err != nil {
				return err
			}
			fmt.Printf("state:           %s\n", s.State)
			if s.CurrentIssue != nil {
				fmt.Printf("current_issue:   #%d\n", *s.CurrentIssue)
			}
			if s.CurrentSlug != "" {
				fmt.Printf("current_slug:    %s\n", s.CurrentSlug)
			}
			if s.CurrentLoomID != "" {
				fmt.Printf("current_loom:    %s\n", s.CurrentLoomID)
			}
			if s.StartedAt != nil {
				fmt.Printf("started_at:      %s\n", s.StartedAt.Format(time.RFC3339))
			}
			fmt.Printf("last_transition: %s\n", s.LastTransition.Format(time.RFC3339))
			fmt.Printf("commands.jsonl:  %s\n", commandsPath(dir))
			fmt.Printf("state.json:      %s\n", statePath(dir))
			// Pause sentinel hint.
			cwd, _ := os.Getwd()
			if backlog.PauseSentinelExists(filepath.Join(cwd, "docs", "backlog")) {
				fmt.Println("PAUSE sentinel:  present (docs/backlog/PAUSE)")
			}
			return nil
		},
	}
}

func newForemanTellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tell \"<message>\"",
		Short: "Send a freeform instruction the Foreman LLM will interpret",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := foremanDir()
			if err != nil {
				return err
			}
			msg := strings.Join(args, " ")
			return appendCommand(dir, ForemanCommand{
				Verb: "tell",
				Args: map[string]any{"message": msg},
			})
		},
	}
}

func newForemanSkipCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skip",
		Short: "Skip the current issue (or a named one with --slug)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := foremanDir()
			if err != nil {
				return err
			}
			slug, _ := cmd.Flags().GetString("slug")
			cArgs := map[string]any{}
			if slug != "" {
				cArgs["slug"] = slug
			}
			return appendCommand(dir, ForemanCommand{Verb: "skip", Args: cArgs})
		},
	}
	cmd.Flags().String("slug", "", "Skip a specific backlog slug (default: current)")
	return cmd
}

func newForemanPrioCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prio <slug> <p1|p2|p3>",
		Short: "Re-rank a backlog entry; reconciler propagates label change",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, prio := args[0], args[1]
			if !backlog.IsValidPriority(prio) {
				return fmt.Errorf("invalid priority %q (want p1|p2|p3)", prio)
			}
			// Apply directly to the markdown source of truth — the
			// reconciler picks up the change on its next 60s poll.
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			dir := filepath.Join(cwd, "docs", "backlog")
			if err := backlog.SetPriority(dir, slug, prio); err != nil {
				return err
			}
			fmDir, err := foremanDir()
			if err != nil {
				return err
			}
			return appendCommand(fmDir, ForemanCommand{
				Verb: "prio",
				Args: map[string]any{"slug": slug, "priority": prio},
			})
		},
	}
}

func newForemanDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Headless Foreman: tail commands.jsonl and maintain state.json",
		Long: `Daemon mode is a minimal v1 — it tails commands.jsonl, applies state
transitions to state.json, and exits on 'stop'. The actual issue-pull /
Worker-dispatch loop is performed by a chat-agent Foreman running
` + "`ycode prompt /foreman`" + ` (typically in another shell or session).
A future revision can fold the loop body into Go for fully-headless
operation. For v1, run the chat agent with this daemon as a state
mirror that any Boss CLI can read or write.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := foremanDir()
			if err != nil {
				return err
			}
			return runForemanDaemon(dir)
		},
	}
}

// runForemanDaemon is the headless state-machine loop. v1 only tracks
// lifecycle state — Worker dispatch is the chat agent's job (see the
// note in newForemanDaemonCmd).
func runForemanDaemon(dir string) error {
	state, err := ReadState(dir)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if state.StartedAt == nil {
		state.StartedAt = &now
	}
	state.State = StateRunning
	if err := WriteState(dir, state); err != nil {
		return err
	}
	fmt.Printf("foreman daemon: started; state=%s\n", state.State)

	for {
		processed, err := drainCommands(dir, &state)
		if err != nil {
			return err
		}
		if processed > 0 {
			if err := WriteState(dir, state); err != nil {
				return err
			}
			fmt.Printf("foreman daemon: applied %d command(s); state=%s\n", processed, state.State)
		}
		if state.State == StateStopped {
			fmt.Println("foreman daemon: stop received; exiting")
			return nil
		}
		time.Sleep(2 * time.Second)
	}
}

// drainCommands reads new lines from commands.jsonl (past LastCommandID)
// and applies them to state in order. Returns the number applied.
func drainCommands(dir string, state *ForemanStateFile) (int, error) {
	data, err := os.ReadFile(commandsPath(dir))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	skip := state.LastCommandID != ""
	applied := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		var c ForemanCommand
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		if skip {
			if c.ID == state.LastCommandID {
				skip = false
			}
			continue
		}
		applyCommand(state, c)
		state.LastCommandID = c.ID
		applied++
	}
	return applied, nil
}

// applyCommand is the state-machine transition function.
func applyCommand(state *ForemanStateFile, c ForemanCommand) {
	switch c.Verb {
	case "start":
		if state.State == StatePaused || state.State == StateIdle || state.State == StateStopped {
			state.State = StateRunning
			now := time.Now().UTC()
			state.StartedAt = &now
		}
	case "resume":
		if state.State == StatePaused {
			state.State = StateRunning
		}
	case "pause":
		if state.State == StateRunning {
			state.State = StatePaused
		}
	case "stop":
		state.State = StateStopped
	case "skip":
		// skip is observed by the chat agent; daemon just records it.
	case "prio", "tell":
		// observed by the chat agent.
	}
}
