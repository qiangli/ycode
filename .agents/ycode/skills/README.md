# Contributor skills (internal lane)

Skills for people (and agents) working ON ycode itself — they assume
the ycode source tree as cwd and are discovered via the workspace
overlay, so they are available exactly when you're inside this repo:

- `/setup`, `/build`, `/deploy` — dev-environment + build + deploy flows
- `/validate`, `/eval`, `/bench-instructions` — test/benchmark harnesses
- `/audit` — security/compliance audit; **run before submitting a PR**
- `/analyze` — gap analysis against the priorart/ corpus

They are tracked in git (force-added past the `.agents/` ignore) but
NOT embedded in the binary and NOT installed user-globally — regular
ycode users never see them.

Skills for regular users of ycode live in the top-level `skills/`
package instead: embedded in the binary, installed to
`~/.config/ycode/skills/` by `ycode init`, usable in any repo.
