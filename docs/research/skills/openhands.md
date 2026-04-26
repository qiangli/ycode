# OpenHands - Skills Analysis

**Project:** OpenHands (formerly OpenDevin)
**Language:** Python (backend) + React (frontend)
**Repository:** All-Hands-AI/OpenHands

---

## Skills (26 shared microagents)

| Skill | Description | Triggers |
|-------|-------------|----------|
| `github.md` | GitHub PR/issue management | GitHub keywords |
| `gitlab.md` | GitLab integration | GitLab keywords |
| `azure_devops.md` | Azure DevOps integration | Azure keywords |
| `bitbucket.md` | Bitbucket integration | Bitbucket keywords |
| `docker.md` | Docker usage guidelines | Docker keywords |
| `kubernetes.md` | K8s deployment | K8s keywords |
| `ssh.md` | SSH operations | SSH keywords |
| `security.md` | Security best practices | Security keywords |
| `code-review.md` | Code review workflow | Review keywords |
| `fix_test.md` | Test fixing workflow | Test keywords |
| `add_agent.md` | Create new microagents | "new agent", "create agent" |
| `add_repo_inst.md` | Generate repo instructions | - |
| `agent_memory.md` | Memory/context management | - |
| `agent-builder.md` | Build custom agents | - |
| `npm.md` | NPM package management | NPM keywords |
| `onboarding.md` | User onboarding | - |
| `pdflatex.md` | PDF/LaTeX handling | LaTeX keywords |
| `address_pr_comments.md` | Address PR comments | - |
| `update_pr_description.md` | Update PR descriptions | - |
| `update_test.md` | Test updating workflow | - |
| `swift-linux.md` | Swift on Linux | Swift keywords |
| `default-tools.md` | Default tools docs | - |
| Plus 4 more specialized skills | | |

### Skill Structure (YAML frontmatter)
```yaml
name: skill_name
type: knowledge | repo
version: 1.0.0
agent: CodeActAgent
triggers: [keyword1, keyword2]
```

---

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **Keyword triggers** | Skills auto-activate on conversation content match |
| **Agent binding** | Skills can be bound to specific agent types |

---

## Notable Patterns

- **Keyword-triggered:** Automatic skill activation based on conversation content
- **26 shared microagents:** Broad coverage of common development workflows
- **Agent binding:** Skills specify which agent type they work with

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Keyword-triggered skills | Not implemented | **Medium** - auto-activation |
| 26 microagent skills | 6 skills | **High** - expand library |
| GitHub/GitLab/Azure/Bitbucket skills | Not implemented | **Medium** - VCS integration |
| Docker/K8s/SSH skills | Not implemented | Low - platform guidance |
| Agent-bound skills | Not implemented | Low |
