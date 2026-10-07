# Declared session controls

The canonical example and genie declare these session operations in their CLI
trees. Command names and availability come from YAML. Frontends map them to
the public Harness API on their existing application; they do not implement
separate session policy.

| Command | Engine behavior |
| --- | --- |
| `pause` | Suspend the owning Harness's live turn at its next graph-stage boundary, after any in-flight tool completes. A separate process without that live Harness reports an error. |
| `continue` | Release the live pause without approving or consuming a typed HITL decision. |
| `btw TEXT...` | Answer a tool-free side query using the declared aside prompt and a snapshot of history, without committing the query or answer to the transcript. |
| `retry [TEXT...]` | Rewind the last completed turn and submit its recorded input or replacement text with fresh identities and the current caller's admission authority. Prior tool effects remain. |
| `revert` | Restore the conversation to the last completed turn's loaded history boundary, preserving the append-only log. Report `files_restored: false` and unsupported restoration because the Bashy execution boundary has no undo API. |
| `compact` | Apply the agent memory's existing compaction policy and persist the resulting history. Report `nothing-to-compact` when the preservation budget retains everything. |
| `plan [TEXT...]` | Toggle durable plan/act mode, or enter planning mode with a request. Use the declared route and planning prompt without running the execution graph. No tools are advertised; unexpected tool calls fail. Persist the conversation and deliver through declared sinks. |
| `model use MODEL_REF` | Persist a session selection among model resources in the default agent's declared route. Provider IDs and undeclared resources are rejected. Each subsequent turn revalidates against its own agent route. |
| `model current` | Show the effective session model; without a session show the default route's first model. |

These commands accept `--session`; otherwise they use the terminal pointer or
latest session. Mutating commands also declare `--dry-run` and `--json`.
Dry-run prints the requested operation without provider, queue or session
mutation. JSON plan/retry output is a stream of canonical events. The API
returns model resource keys; the human `model current` view displays the
corresponding provider model ID.

The optional agent policy is:

```yaml
sessionControls:
  planPrompt: Produce a plan. Do not execute tools or claim file changes.
  planSinkRefs: [primary-output]
  btwPrompt: Answer the side question without executing tools.
```

Planning is non-mutating with respect to workspace/tool execution, while
session events, transcript checkpoints and delivery remain durable. Omitting
the planning declaration rejects Plan. Typed HITL Resume is unchanged;
Continue releases only a cooperative pause. Live pause is an API operation;
cross-process cancellation transport is not implemented by this slice.

Model changes, compaction and revert require an idle session. Kernel locks
exclude concurrent turns or maintenance across harness instances. Model
selection is configuration-digest bound and copied from the completed boundary
when forking. A fork remains independently selectable. Queue replay uses new
versioned admission and consumption events; old queue records without durable
consumption identities are not resurrected.
