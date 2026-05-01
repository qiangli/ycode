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
	var sinceTime, untilTime *time.Time
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
		case strings.HasPrefix(args[i], "--since="):
			val := strings.TrimPrefix(args[i], "--since=")
			if t, err := parseGitDate(val); err == nil {
				sinceTime = &t
			} else {
				return nil, ErrNotImplemented
			}
		case strings.HasPrefix(args[i], "--until="):
			val := strings.TrimPrefix(args[i], "--until=")
			if t, err := parseGitDate(val); err == nil {
				untilTime = &t
			} else {
				return nil, ErrNotImplemented
			}
		case strings.HasPrefix(args[i], "--date="):
			// Accept --date= flag (formatting hint) — ignore for native
		case strings.HasPrefix(args[i], "--author="):
			// Author filter — fall through to host git for now
			return nil, ErrNotImplemented
		default:
			// Unsupported flags (e.g., path specs)
			return nil, ErrNotImplemented
		}
	}

	logOpts := &git.LogOptions{}
	if sinceTime != nil {
		logOpts.Since = sinceTime
	}
	if untilTime != nil {
		logOpts.Until = untilTime
	}
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

	// Parse list flags
	listRemote := false
	listAll := false
	verbose := false
	containsRef := ""
	nonFlagArgs := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-v":
			verbose = true
		case "-r":
			listRemote = true
		case "-a":
			listAll = true
		case "--contains":
			if i+1 < len(args) {
				i++
				containsRef = args[i]
			} else {
				return nil, ErrNotImplemented
			}
		default:
			if strings.HasPrefix(args[i], "--contains=") {
				containsRef = strings.TrimPrefix(args[i], "--contains=")
			} else if strings.HasPrefix(args[i], "-") && args[i] != "-D" && args[i] != "-d" {
				nonFlagArgs = append(nonFlagArgs, args[i])
			} else {
				nonFlagArgs = append(nonFlagArgs, args[i])
			}
		}
	}
	_ = verbose

	// Listing mode: no non-flag args, or only -v/-r/-a/--contains
	isListMode := len(nonFlagArgs) == 0 || (len(args) > 0 && (args[0] == "-v" || args[0] == "-r" || args[0] == "-a"))

	if isListMode && (len(nonFlagArgs) == 0) {
		head, _ := repo.Head()

		// Resolve --contains commit hash if specified
		var containsHash *plumbing.Hash
		if containsRef != "" {
			h, err := repo.ResolveRevision(plumbing.Revision(containsRef))
			if err != nil {
				return &Result{
					Stderr:   fmt.Sprintf("error: no such commit '%s'\n", containsRef),
					ExitCode: 129,
					Tier:     TierNative,
				}, nil
			}
			containsHash = h
		}

		var b strings.Builder

		// List local branches
		if !listRemote {
			refs, err := repo.Branches()
			if err != nil {
				return nil, ErrNotImplemented
			}
			err = refs.ForEach(func(ref *plumbing.Reference) error {
				if containsHash != nil {
					if !branchContainsCommit(repo, ref.Hash(), *containsHash) {
						return nil
					}
				}
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
		}

		// List remote branches
		if listRemote || listAll {
			allRefs, err := repo.References()
			if err != nil {
				return nil, ErrNotImplemented
			}
			err = allRefs.ForEach(func(ref *plumbing.Reference) error {
				if !ref.Name().IsRemote() {
					return nil
				}
				if containsHash != nil {
					if !branchContainsCommit(repo, ref.Hash(), *containsHash) {
						return nil
					}
				}
				b.WriteString("  ")
				b.WriteString(ref.Name().Short())
				b.WriteByte('\n')
				return nil
			})
			if err != nil {
				return nil, ErrNotImplemented
			}
		}

		return &Result{Stdout: b.String(), Tier: TierNative}, nil
	}

	// No args or -v: list branches (original simple path)
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

func nativeReset(_ context.Context, dir string, args []string) (*Result, error) {
	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, ErrNotImplemented
	}

	// Check for specific file paths after "--"
	var files []string
	dashDash := false
	for _, arg := range args {
		if arg == "--" {
			dashDash = true
			continue
		}
		if arg == "HEAD" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return nil, ErrNotImplemented
		}
		if dashDash {
			files = append(files, arg)
		}
	}

	if len(files) > 0 {
		// Reset specific files: re-read HEAD tree and update index entries
		head, err := repo.Head()
		if err != nil {
			return nil, ErrNotImplemented
		}
		commit, err := repo.CommitObject(head.Hash())
		if err != nil {
			return nil, ErrNotImplemented
		}
		tree, err := commit.Tree()
		if err != nil {
			return nil, ErrNotImplemented
		}

		// Get status first to determine which files are staged
		status, err := wt.Status()
		if err != nil {
			return nil, ErrNotImplemented
		}

		for _, file := range files {
			fs := status.File(file)
			if fs.Staging == git.Unmodified {
				continue
			}
			// Check if file exists in HEAD tree
			_, err := tree.File(file)
			if err != nil {
				// File doesn't exist in HEAD — remove from index
				// This is equivalent to git reset HEAD -- <newfile>
				// go-git doesn't have a direct "unstage" API, fall through
				return nil, ErrNotImplemented
			}
			// File exists in HEAD: re-add from worktree to reset staging
			// Actually, go-git's worktree doesn't have a reset-file API
			// Fall through to host git for this case
			return nil, ErrNotImplemented
		}
		return &Result{Stdout: "", Tier: TierNative}, nil
	}

	// No specific files: mixed reset to HEAD (unstage everything)
	head, err := repo.Head()
	if err != nil {
		return nil, ErrNotImplemented
	}
	err = wt.Reset(&git.ResetOptions{
		Mode:   git.MixedReset,
		Commit: head.Hash(),
	})
	if err != nil {
		return nil, ErrNotImplemented
	}

	return &Result{Stdout: "", Tier: TierNative}, nil
}

func nativeShow(_ context.Context, dir string, args []string) (*Result, error) {
	if len(args) == 0 {
		return nil, ErrNotImplemented
	}

	repo, err := openRepo(dir)
	if err != nil {
		return nil, ErrNotImplemented
	}

	// Resolve revision
	revision := args[0]
	hash, err := repo.ResolveRevision(plumbing.Revision(revision))
	if err != nil {
		return nil, ErrNotImplemented
	}

	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return nil, ErrNotImplemented
	}

	var b strings.Builder
	b.WriteString("commit ")
	b.WriteString(commit.Hash.String())
	b.WriteByte('\n')
	b.WriteString("Author: ")
	b.WriteString(commit.Author.Name)
	b.WriteString(" <")
	b.WriteString(commit.Author.Email)
	b.WriteString(">\n")
	b.WriteString("Date:   ")
	b.WriteString(commit.Author.When.Format("Mon Jan 2 15:04:05 2006 -0700"))
	b.WriteString("\n\n    ")
	b.WriteString(strings.TrimRight(commit.Message, "\n"))
	b.WriteString("\n\n")

	// Generate patch
	var parentTree *object.Tree
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err == nil {
			parentTree, _ = parent.Tree()
		}
	}

	commitTree, err := commit.Tree()
	if err != nil {
		// Return header without patch
		return &Result{Stdout: b.String(), Tier: TierNative}, nil
	}

	if parentTree == nil {
		// Root commit: diff against empty tree
		parentTree = &object.Tree{}
	}

	changes, err := parentTree.Diff(commitTree)
	if err != nil {
		// Return header without patch
		return &Result{Stdout: b.String(), Tier: TierNative}, nil
	}

	patch, err := changes.Patch()
	if err != nil {
		return &Result{Stdout: b.String(), Tier: TierNative}, nil
	}

	b.WriteString(patch.String())

	return &Result{Stdout: b.String(), Tier: TierNative}, nil
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

// parseGitDate attempts to parse a date string in common git formats.
// Dates without explicit timezone are interpreted in the local timezone,
// matching git's default behavior.
func parseGitDate(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02",
		"2006-01-02T15:04:05",
		"Jan 2 2006",
		"2 Jan 2006",
	}
	// Try local timezone first (matches git behavior)
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return t, nil
		}
	}
	// Try RFC3339 which includes timezone
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported date format: %s", s)
}

// branchContainsCommit checks if the branch tip is an ancestor of (or equal to)
// the given commit, meaning the branch contains that commit in its history.
func branchContainsCommit(repo *git.Repository, branchTip, target plumbing.Hash) bool {
	if branchTip == target {
		return true
	}
	// Walk backwards from branchTip looking for target
	iter, err := repo.Log(&git.LogOptions{From: branchTip})
	if err != nil {
		return false
	}
	found := false
	_ = iter.ForEach(func(c *object.Commit) error {
		if c.Hash == target {
			found = true
			return fmt.Errorf("stop")
		}
		return nil
	})
	return found
}
