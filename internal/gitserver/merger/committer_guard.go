package merger

import (
	"context"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/gitserver"
)

// committerAllowed implements Defense Layer 4 from loom v2: gates
// auto-merge to PRs whose head ref or committer matches one of the
// configured AllowedCommitterEmails patterns.
//
// The substrate-deep version of this check inspects the head commit's
// committer email via Gitea's PR-commits API. Until gitserver.Client
// gains a ListPRCommits method, we approximate with a head-ref
// substring check (loom branches all carry the agent-identity prefix
// "agent/agent-loom-<label>-..." per pkg/loom.Service.Lease).
//
// This is enough to block merges from PR branches that don't carry
// an agent identity (e.g. a misconfigured tool pushing directly to a
// human's branch name). When the deeper committer check arrives,
// this body switches to inspecting commits[0].Committer.Email
// without touching the call sites.
func (m *Merger) committerAllowed(ctx context.Context, pr gitserver.PullRequest) (bool, string, error) {
	_ = ctx
	ref := pr.Head.Ref
	for _, pattern := range m.cfg.AllowedCommitterEmails {
		// Treat each pattern as a substring match against the head
		// ref. Patterns like "agent/agent-loom-" allow every loom-
		// allocated lease; "@ycode.local" is treated identically
		// since it never matches a ref (caller intent: explicit
		// allowlist).
		if strings.Contains(ref, pattern) {
			return true, "", nil
		}
	}
	return false, fmt.Sprintf("PR head ref %q does not match allowlist %v (Defense Layer 4)", ref, m.cfg.AllowedCommitterEmails), nil
}
