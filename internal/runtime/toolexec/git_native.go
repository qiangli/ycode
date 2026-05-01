package toolexec

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// openRepo opens the git repository at or above dir.
func openRepo(dir string) (*git.Repository, error) {
	return git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
}

func nativeRevParse(_ context.Context, dir string, args []string) (*Result, error) {
	if len(args) == 0 {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	switch args[0] {
	case "--is-inside-work-tree":
		return &Result{Stdout: "true\n", Tier: TierNative}, nil

	case "--show-toplevel":
		wt, err := repo.Worktree()
		if err != nil {
			return nil, ErrNotImplemented
		}
		return &Result{Stdout: wt.Filesystem.Root() + "\n", Tier: TierNative}, nil

	case "--abbrev-ref":
		if len(args) < 2 || args[1] != "HEAD" {
			return nil, ErrNotImplemented
		}
		head, err := repo.Head()
		if err != nil {
			return nil, ErrNotImplemented
		}
		if head.Name().IsBranch() {
			return &Result{Stdout: head.Name().Short() + "\n", Tier: TierNative}, nil
		}
		return &Result{Stdout: "HEAD\n", Tier: TierNative}, nil

	case "--short":
		if len(args) < 2 || args[1] != "HEAD" {
			return nil, ErrNotImplemented
		}
		head, err := repo.Head()
		if err != nil {
			return nil, ErrNotImplemented
		}
		return &Result{Stdout: head.Hash().String()[:7] + "\n", Tier: TierNative}, nil

	case "--verify":
		if len(args) < 2 {
			return nil, ErrNotImplemented
		}
		ref := args[1]
		if ref == "HEAD" {
			head, err := repo.Head()
			if err != nil {
				return &Result{Stderr: "fatal: not a git repository\n", ExitCode: 128, Tier: TierNative}, nil
			}
			return &Result{Stdout: head.Hash().String() + "\n", Tier: TierNative}, nil
		}
		// Try resolving as a reference
		hash, err := repo.ResolveRevision(plumbing.Revision(ref))
		if err != nil {
			return &Result{Stderr: fmt.Sprintf("fatal: Needed a single revision\n"), ExitCode: 128, Tier: TierNative}, nil
		}
		return &Result{Stdout: hash.String() + "\n", Tier: TierNative}, nil

	default:
		return nil, ErrNotImplemented
	}
}

func nativeStatus(_ context.Context, dir string, args []string) (*Result, error) {
	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, ErrNotImplemented
	}

	status, err := wt.Status()
	if err != nil {
		return nil, ErrNotImplemented
	}

	// go-git's Status.String() already produces porcelain-style output.
	// Handle --short and --porcelain which produce the same format.
	return &Result{Stdout: status.String(), Tier: TierNative}, nil
}

func nativeLog(_ context.Context, dir string, args []string) (*Result, error) {
	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	// Parse flags
	limit := 0
	oneline := false
	formatStr := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--oneline":
			oneline = true
		case strings.HasPrefix(args[i], "-") && len(args[i]) > 1 && args[i][1] >= '0' && args[i][1] <= '9':
			fmt.Sscanf(args[i], "-%d", &limit)
		case strings.HasPrefix(args[i], "-n"):
			if args[i] == "-n" && i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &limit)
			} else {
				fmt.Sscanf(args[i][2:], "%d", &limit)
			}
		case strings.HasPrefix(args[i], "--format="):
			formatStr = strings.TrimPrefix(args[i], "--format=")
		case args[i] == "--format" && i+1 < len(args):
			i++
			formatStr = args[i]
		default:
			// Unsupported flags (e.g., path specs, --since, etc.)
			return nil, ErrNotImplemented
		}
	}

	logOpts := &git.LogOptions{}
	iter, err := repo.Log(logOpts)
	if err != nil {
		return nil, ErrNotImplemented
	}

	var b strings.Builder
	count := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && count >= limit {
			return fmt.Errorf("stop")
		}
		count++

		if oneline {
			b.WriteString(c.Hash.String()[:7])
			b.WriteByte(' ')
			// First line of commit message
			msg := strings.SplitN(c.Message, "\n", 2)[0]
			b.WriteString(msg)
			b.WriteByte('\n')
		} else if formatStr != "" {
			line := formatCommit(formatStr, c)
			b.WriteString(line)
			b.WriteByte('\n')
		} else {
			b.WriteString("commit ")
			b.WriteString(c.Hash.String())
			b.WriteByte('\n')
			b.WriteString("Author: ")
			b.WriteString(c.Author.Name)
			b.WriteString(" <")
			b.WriteString(c.Author.Email)
			b.WriteString(">\n")
			b.WriteString("Date:   ")
			b.WriteString(c.Author.When.Format("Mon Jan 2 15:04:05 2006 -0700"))
			b.WriteString("\n\n    ")
			b.WriteString(strings.TrimRight(c.Message, "\n"))
			b.WriteString("\n\n")
		}
		return nil
	})
	// "stop" error is our sentinel, not a real error.
	if err != nil && err.Error() != "stop" {
		return nil, ErrNotImplemented
	}

	return &Result{Stdout: b.String(), Tier: TierNative}, nil
}

// formatCommit handles basic --format placeholders.
func formatCommit(format string, c *object.Commit) string {
	r := strings.NewReplacer(
		"%H", c.Hash.String(),
		"%h", c.Hash.String()[:7],
		"%s", strings.SplitN(c.Message, "\n", 2)[0],
		"%an", c.Author.Name,
		"%ae", c.Author.Email,
		"%cn", c.Committer.Name,
		"%ce", c.Committer.Email,
		"%ad", c.Author.When.Format(time.RFC3339),
		"%cd", c.Committer.When.Format(time.RFC3339),
	)
	return r.Replace(format)
}

func nativeDiff(_ context.Context, dir string, args []string) (*Result, error) {
	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, ErrNotImplemented
	}

	// Parse flags
	cached := false
	stat := false
	for _, arg := range args {
		switch arg {
		case "--cached", "--staged":
			cached = true
		case "--stat":
			stat = true
		default:
			// Path specs, commit ranges, etc. — fall through
			return nil, ErrNotImplemented
		}
	}

	_ = stat // stat formatting is complex; fall through for now
	if stat {
		return nil, ErrNotImplemented
	}

	if cached {
		// Staged changes: diff between HEAD and index
		return nil, ErrNotImplemented // go-git staged diff is complex
	}

	// Unstaged changes: diff between index and working tree
	status, err := wt.Status()
	if err != nil {
		return nil, ErrNotImplemented
	}

	if status.IsClean() {
		return &Result{Stdout: "", Tier: TierNative}, nil
	}

	// For full diff output, fall through to host git — go-git's patch generation
	// for worktree diffs requires manual file comparison.
	return nil, ErrNotImplemented
}

func nativeMergeBase(_ context.Context, dir string, args []string) (*Result, error) {
	if len(args) < 2 {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	hash1, err := repo.ResolveRevision(plumbing.Revision(args[0]))
	if err != nil {
		return nil, ErrNotImplemented
	}

	hash2, err := repo.ResolveRevision(plumbing.Revision(args[1]))
	if err != nil {
		return nil, ErrNotImplemented
	}

	commit1, err := repo.CommitObject(*hash1)
	if err != nil {
		return nil, ErrNotImplemented
	}

	commit2, err := repo.CommitObject(*hash2)
	if err != nil {
		return nil, ErrNotImplemented
	}

	bases, err := commit1.MergeBase(commit2)
	if err != nil || len(bases) == 0 {
		return nil, ErrNotImplemented
	}

	return &Result{Stdout: bases[0].Hash.String() + "\n", Tier: TierNative}, nil
}

func nativeRevList(_ context.Context, dir string, args []string) (*Result, error) {
	// Handle --count ref1..ref2 or --count ref1...ref2
	countMode := false
	var rangeSpec string
	for _, arg := range args {
		if arg == "--count" {
			countMode = true
		} else if !strings.HasPrefix(arg, "-") {
			rangeSpec = arg
		}
	}

	if !countMode || rangeSpec == "" {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	// Parse ref1..ref2 or ref1...ref2
	var from, to string
	if parts := strings.SplitN(rangeSpec, "...", 2); len(parts) == 2 {
		from, to = parts[0], parts[1]
	} else if parts := strings.SplitN(rangeSpec, "..", 2); len(parts) == 2 {
		from, to = parts[0], parts[1]
	} else {
		return nil, ErrNotImplemented
	}

	fromHash, err := repo.ResolveRevision(plumbing.Revision(from))
	if err != nil {
		return nil, ErrNotImplemented
	}

	toHash, err := repo.ResolveRevision(plumbing.Revision(to))
	if err != nil {
		return nil, ErrNotImplemented
	}

	// Count commits reachable from 'to' but not from 'from'
	iter, err := repo.Log(&git.LogOptions{From: *toHash})
	if err != nil {
		return nil, ErrNotImplemented
	}

	count := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Hash == *fromHash {
			return fmt.Errorf("stop")
		}
		count++
		return nil
	})
	if err != nil && err.Error() != "stop" {
		return nil, ErrNotImplemented
	}

	return &Result{Stdout: fmt.Sprintf("%d\n", count), Tier: TierNative}, nil
}

func nativeConfig(_ context.Context, dir string, args []string) (*Result, error) {
	if len(args) == 0 {
		return nil, ErrNotImplemented
	}

	// Only handle reads (single key argument or --get key)
	key := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--get" && i+1 < len(args):
			i++
			key = args[i]
		case !strings.HasPrefix(args[i], "-"):
			if key == "" {
				key = args[i]
			} else {
				// Two non-flag args = a write operation
				return nil, ErrNotImplemented
			}
		default:
			return nil, ErrNotImplemented
		}
	}

	if key == "" {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	cfg, err := repo.ConfigScoped(config.GlobalScope)
	if err != nil {
		return nil, ErrNotImplemented
	}

	switch key {
	case "user.name":
		if cfg.User.Name == "" {
			return &Result{ExitCode: 1, Tier: TierNative}, nil
		}
		return &Result{Stdout: cfg.User.Name + "\n", Tier: TierNative}, nil
	case "user.email":
		if cfg.User.Email == "" {
			return &Result{ExitCode: 1, Tier: TierNative}, nil
		}
		return &Result{Stdout: cfg.User.Email + "\n", Tier: TierNative}, nil
	default:
		return nil, ErrNotImplemented
	}
}

func nativeAdd(_ context.Context, dir string, args []string) (*Result, error) {
	if len(args) == 0 {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, ErrNotImplemented
	}

	for _, arg := range args {
		if arg == "-A" || arg == "--all" || arg == "." {
			// Add all: get status and add each file
			status, err := wt.Status()
			if err != nil {
				return nil, ErrNotImplemented
			}
			for path := range status {
				if _, err := wt.Add(path); err != nil {
					return nil, ErrNotImplemented
				}
			}
			return &Result{Stdout: "", Tier: TierNative}, nil
		}
	}

	// Add specific paths
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return nil, ErrNotImplemented
		}
		// Make path relative to repo root
		absDir, _ := filepath.Abs(dir)
		root := wt.Filesystem.Root()
		relPath, err := filepath.Rel(root, filepath.Join(absDir, arg))
		if err != nil {
			relPath = arg
		}
		if _, err := wt.Add(relPath); err != nil {
			return nil, ErrNotImplemented
		}
	}

	return &Result{Stdout: "", Tier: TierNative}, nil
}

func nativeCommit(_ context.Context, dir string, args []string) (*Result, error) {
	// Extract -m message
	msg := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-m" && i+1 < len(args) {
			i++
			msg = args[i]
		} else if strings.HasPrefix(args[i], "-m") {
			msg = args[i][2:]
		} else if args[i] == "--allow-empty" || args[i] == "--no-verify" {
			// Accepted flags, no-op
		} else {
			return nil, ErrNotImplemented
		}
	}

	if msg == "" {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, ErrNotImplemented
	}

	hash, err := wt.Commit(msg, &git.CommitOptions{})
	if err != nil {
		return nil, ErrNotImplemented
	}

	// Format output similar to git commit
	head, _ := repo.Head()
	branchName := "HEAD"
	if head != nil && head.Name().IsBranch() {
		branchName = head.Name().Short()
	}

	firstLine := strings.SplitN(msg, "\n", 2)[0]
	out := fmt.Sprintf("[%s %s] %s\n", branchName, hash.String()[:7], firstLine)
	return &Result{Stdout: out, Tier: TierNative}, nil
}

func nativeBranch(_ context.Context, dir string, args []string) (*Result, error) {
	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	// No args or -v: list branches
	if len(args) == 0 || (len(args) == 1 && args[0] == "-v") {
		head, _ := repo.Head()
		refs, err := repo.Branches()
		if err != nil {
			return nil, ErrNotImplemented
		}

		var b strings.Builder
		err = refs.ForEach(func(ref *plumbing.Reference) error {
			name := ref.Name().Short()
			if head != nil && ref.Name() == head.Name() {
				b.WriteString("* ")
			} else {
				b.WriteString("  ")
			}
			b.WriteString(name)
			b.WriteByte('\n')
			return nil
		})
		if err != nil {
			return nil, ErrNotImplemented
		}
		return &Result{Stdout: b.String(), Tier: TierNative}, nil
	}

	// Delete branch
	if args[0] == "-D" || args[0] == "-d" {
		if len(args) < 2 {
			return nil, ErrNotImplemented
		}
		branchName := args[1]
		err := repo.DeleteBranch(branchName)
		if err != nil {
			return &Result{
				Stderr:   fmt.Sprintf("error: branch '%s' not found.\n", branchName),
				ExitCode: 1,
				Tier:     TierNative,
			}, nil
		}
		// Also delete the reference
		refName := plumbing.NewBranchReferenceName(branchName)
		_ = repo.Storer.RemoveReference(refName)
		return &Result{Stdout: fmt.Sprintf("Deleted branch %s\n", branchName), Tier: TierNative}, nil
	}

	// Create branch (single name argument)
	if len(args) == 1 && !strings.HasPrefix(args[0], "-") {
		branchName := args[0]
		head, err := repo.Head()
		if err != nil {
			return nil, ErrNotImplemented
		}
		refName := plumbing.NewBranchReferenceName(branchName)
		ref := plumbing.NewHashReference(refName, head.Hash())
		if err := repo.Storer.SetReference(ref); err != nil {
			return nil, ErrNotImplemented
		}
		return &Result{Stdout: "", Tier: TierNative}, nil
	}

	return nil, ErrNotImplemented
}

func nativeCheckout(_ context.Context, dir string, args []string) (*Result, error) {
	if len(args) == 0 {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, ErrNotImplemented
	}

	// Handle -b (create and checkout)
	if args[0] == "-b" {
		if len(args) < 2 {
			return nil, ErrNotImplemented
		}
		branchName := args[1]
		err := wt.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(branchName),
			Create: true,
		})
		if err != nil {
			return nil, ErrNotImplemented
		}
		return &Result{
			Stdout: fmt.Sprintf("Switched to a new branch '%s'\n", branchName),
			Tier:   TierNative,
		}, nil
	}

	// Checkout existing branch
	if len(args) == 1 && !strings.HasPrefix(args[0], "-") {
		branchName := args[0]
		err := wt.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(branchName),
		})
		if err != nil {
			return nil, ErrNotImplemented
		}
		return &Result{
			Stdout: fmt.Sprintf("Switched to branch '%s'\n", branchName),
			Tier:   TierNative,
		}, nil
	}

	return nil, ErrNotImplemented
}

func nativeRemote(_ context.Context, dir string, args []string) (*Result, error) {
	if len(args) < 2 {
		return nil, ErrNotImplemented
	}

	// Handle "get-url <remote>"
	if args[0] != "get-url" {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	remoteName := args[1]
	remote, err := repo.Remote(remoteName)
	if err != nil {
		return &Result{
			Stderr:   fmt.Sprintf("fatal: No such remote '%s'\n", remoteName),
			ExitCode: 2,
			Tier:     TierNative,
		}, nil
	}

	urls := remote.Config().URLs
	if len(urls) == 0 {
		return &Result{ExitCode: 1, Tier: TierNative}, nil
	}

	return &Result{Stdout: urls[0] + "\n", Tier: TierNative}, nil
}

func nativeForEachRef(_ context.Context, _ string, _ []string) (*Result, error) {
	// Complex format parsing — fall through for now
	return nil, ErrNotImplemented
}

func nativeStash(_ context.Context, _ string, _ []string) (*Result, error) {
	return nil, ErrNotImplemented
}

func nativeWorktree(_ context.Context, _ string, _ []string) (*Result, error) {
	return nil, ErrNotImplemented
}

func nativeMerge(_ context.Context, _ string, _ []string) (*Result, error) {
	return nil, ErrNotImplemented
}
