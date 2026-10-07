# Prior-art references

Reference repositories are cloned on demand, not retained in this checkout.
They are research material, not build or test dependencies. Keep any temporary
checkout under the ignored `priorart/` directory; its small `go.mod` prevents
foreign Go packages from entering this module's package traversal.

## References removed from local disk on 2026-10-07

| Directory | GitHub repository | Last local commit |
| --- | --- | --- |
| ZCode | [zai-org/ZCode](https://github.com/zai-org/ZCode) | `29628c9acdb81b703bbd4080c207a0e7ce5e276e` |
| codex | [openai/codex](https://github.com/openai/codex) | `2635431edbd43279a4da9c1199456758fbbc8ad3` |
| hermes-agent | [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) | `3ba5602b6273f9bb0d2a0d52c77bbe0f50f6227b` |
| open-claude-code | [ruvnet/open-claude-code](https://github.com/ruvnet/open-claude-code) | `76017317119d8ef1ec54a2be561b85683a8b337b` |
| openclaw | [openclaw/openclaw](https://github.com/openclaw/openclaw) | `6718352a63df815940c7a7bc5666f6484c7b8d9a` |
| opencode | [sst/opencode](https://github.com/sst/opencode) | `7945de208964a49300d7f770d1a71d078db9a4c4` |

From the ycode root, fetch only the reference needed:

```sh
git clone --depth 1 https://github.com/openai/codex.git priorart/codex
```

To inspect the recorded revision instead of current upstream, fetch and detach
at the commit in the table (a shallow fetch may need additional history):

```sh
git -C priorart/codex fetch --depth 1 origin 2635431edbd43279a4da9c1199456758fbbc8ad3
git -C priorart/codex checkout --detach FETCH_HEAD
```

Before deleting a temporary checkout, check working-tree changes, unpublished
commits and stashes. Preserve unique work separately. Never include reference
checkouts in product builds or tests.

## Earlier reference removals (2026-09-03)

These origins and abbreviated revisions were retained in the previous local
reference list; they have not been re-fetched as part of this cleanup.

| Directory | GitHub repository | Recorded revision |
| --- | --- | --- |
| agent-orchestrator | [AgentWrapper/agent-orchestrator](https://github.com/AgentWrapper/agent-orchestrator) | `5897b4e8` |
| aider | [Aider-AI/aider](https://github.com/Aider-AI/aider) | `5dc9490` |
| cline | [cline/cline](https://github.com/cline/cline) | `4bb93ee` |
| continue | [continuedev/continue](https://github.com/continuedev/continue) | `a3076b4` |
| crewAI | [crewAIInc/crewAI](https://github.com/crewAIInc/crewAI) | `e9d568dc6` |
| gemini-cli | [google-gemini/gemini-cli](https://github.com/google-gemini/gemini-cli) | `acae712` |
| goose | [block/goose](https://github.com/block/goose) | `4904e3c` |
| kimi-code | [MoonshotAI/kimi-code](https://github.com/MoonshotAI/kimi-code) | `71bcfba54` |
| openhands | [All-Hands-AI/OpenHands](https://github.com/All-Hands-AI/OpenHands) | `0d77a0d` |
| plandex | [plandex-ai/plandex](https://github.com/plandex-ai/plandex) | `e2d7720` |
| qwen-code | [QwenLM/qwen-code](https://github.com/QwenLM/qwen-code) | `aea34fa` |
