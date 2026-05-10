# skill-CNL — design rationale

Companion to [`skill-cnl.md`](./skill-cnl.md). That doc describes
*what* the package does; this one explains *why it has the shape it
has*. It is a distilled record of the design process, kept in tree so
later contributors can re-derive choices instead of guessing.

The implementation lives at `internal/runtime/skillcnl/` and is
gated by the `experimental` build tag.

---

## 1. The problem

A skill today is `instructions + scripts`. The script half forces a
commitment to a programming language (Python, Node, Go, bash). That
commitment is load-bearing in three ways:

- **Per-skill**: a bash skill cannot trivially become a Python skill.
- **Per-author**: a non-Python reader cannot review a Python skill
  without first becoming a Python reader.
- **Per-language**: a Chinese author writing in Chinese has no path to
  a deterministic, audit-grade representation that an English reviewer
  can validate without trusting a translation step.

The question that started this work was: *can the deterministic
algorithm half of a skill be authored at a higher abstraction layer
while still keeping the validate / lint / trace / debug chain that
real programming languages give you?* — without committing to any
specific programming language at all.

## 2. State-of-the-art survey

There is no widely-accepted "natural-language-with-static-tooling"
answer in 2026. That remains the open frontier. The production-grade
move is **two-layer**: a high-level surface (declarative DSL,
structured prose, or visual graph) that compiles down to a
deterministic, typed IR. Tooling lives at the IR layer.

The validate/lint/trace/debug guarantees come from the *target*, not
the surface. The real choice is: *pick your IR*.

### IR families that already give you the guarantees

| Family | Examples | Strength | Best fit |
|---|---|---|---|
| **Datalog / logic programs** | Soufflé, Rego/OPA, Cedar | Decidable, terminating, free provenance | "derive facts from facts" — policy, code analysis |
| **Total / pure config langs** | CUE, Dhall, Nickel, Jsonnet, Starlark | Total, hermetic, schema-validated | "transform structured data" |
| **WebAssembly + WIT** | Bytecode Alliance Component Model | Sandboxed, deterministic, polyglot | When you want author-in-anything but a hard interface |
| **Term/graph rewriting** | Maude, Stratego/XT, K-framework | Algebraic semantics | Language tooling, semantics work |
| **Formal proof systems** | Lean, Coq, Idris, F* | Maximum determinism, refinement types | When correctness > velocity |
| **Process calculi & spec langs** | TLA+, Alloy, P, mCRL2 | Model-checked, exhaustive | Concurrency, distributed protocols |

### Higher-level *surfaces* worth knowing

- **Workflow DAGs** — CWL, Snakemake, Nextflow, Argo Workflows.
- **HTN / PDDL** classical AI planning.
- **Behavior trees / statecharts** — XState, BehaviorTree.CPP, SCXML.
- **Constraint / answer-set programming** — MiniZinc, Clingo, Picat.
- **DSPy** — compositional LLM programs (`Predict`, `ChainOfThought`).
- **AICI** — sandboxed Wasm modules constraining LLM output.

### "Just write prose" — three flavours that are still tractable

1. **NL → DSL transpilers** (Code-as-Policies, TaskWeaver, Voyager):
   LLM compiles prose into a typed target language; the typed target
   is what runs and what gets debugged.
2. **Structured skill specs with typed slots**: prose with named typed
   slots; algorithm lives in the typed binding layer; prose only
   describes intent. (Anthropic Skills, OpenAI GPTs are this shape.)
3. **Provenance-traced execution** (PROV-O, OpenLineage, OTEL): even
   when the surface is prose, the trace can carry full causal
   provenance.

### What's *not* solved in 2026

- "Natural-language-with-static-checking" as a general capability.
- Cross-linguistic round-trip stability at scale for general-purpose
  CNLs (vs. narrow-domain ones like medical phrasebooks).
- Reliable LLM constrained generation in non-Latin scripts beyond
  English (declining quality, real per-language work).

## 3. Why an LLM-first language is different

The LLM-first / human-readable design profile inverts pressure on
several axes against a human-first language:

| Property | Human-first | LLM-first |
|---|---|---|
| Surface terseness | concise good | regular and verbose good — predictable next-token distributions |
| Whitespace | irrelevant | structured / significant — easier constrained decoding |
| Identifiers | short, contextual | fully-qualified, self-describing |
| Hidden semantics | bad | disastrous — LLMs hallucinate them back |
| Error messages | for humans | machine-actionable repair hints |
| Magic / overloading | clever | forbidden |

Properties an LLM-first skill language probably needs:

1. **Typed effects + capabilities** (Koka, Eff, WASI).
2. **Total by default** — every step declares budget; no unbounded
   loops.
3. **Schema-typed I/O** with refinement constraints.
4. **Trace-first semantics** — every step is a span; replay is a
   primitive.
5. **Repair-friendly errors** — `{at, expected, got, suggested_fix}`.
6. **Linear / affine resources** — for one-shot tokens (lease, lock).
7. **Self-describing primitives** — named-arg calls only, no positional.
8. **Local checkability** — type/effect errors detectable from a
   small window (≤ a few hundred tokens) so the LSP can respond
   *during streaming generation*.
9. **Compositional with NL** — natural-language strings as typed
   values.
10. **No hidden state** — no globals, no implicit context.

## 4. Why a Controlled Natural Language

The next step is committing to the *surface form*. Three forks were
considered:

- **Inform 7-style** — full restricted English. Highest readability,
  largest grammar (Inform 7 spent 20 years on one language and one
  domain).
- **Gherkin-style** — keyworded steps (Given/When/Then). Smaller
  grammar, very lintable, production-proven across ~70 locales.
- **Lean-tactic / literate** — prose interleaved with typed code
  fences. Most familiar to current LLM-skill formats.
- **Attempto Controlled English** — strict English subset that maps
  unambiguously to first-order logic. Strongest semantic guarantees;
  least flexible.

**Choice**: Inform-7 readability target combined with restricted
vocabulary discipline (Gherkin-style closed keyword set). The user-
facing shape reads as natural language; the lexicon is closed; the
grammar is small.

This commitment moves the design out of "natural CNL" territory and
into "**DSL-with-localisation**" territory — a much more tractable
problem with shipping reference systems (Cucumber/Gherkin localised,
Excel function localisation, WikiData multilingual labels).

## 5. Why multilingual from day 1

The CNL is **not English-specific**. It must match the natural
language of the surrounding prose. If the skill is in Chinese, the
strict CNL is also in Chinese.

| | Today | skill-CNL |
|---|---|---|
| Bilingual split | prose-language + programming-language (e.g. English + Python) | prose-language + strict-subset-of-the-same-prose-language (e.g. English + strict-English; Chinese + strict-Chinese) |

The canonical existing tooling for multilingual CNL is **Grammatical
Framework (GF)** by Aarne Ranta — http://www.grammaticalframework.org.
GF gives:

- One language-neutral abstract syntax.
- Concrete syntax per natural language (Resource Grammar Library
  covers ~30+).
- Free roundtrip linearisation across languages.

**Why first-class roundtrip is the shining gem:**

- **Skill identity = abstract-AST hash.** Two skills are equal iff
  their ASTs are equal. Languages are projections; identity is shared.
- **Distributed-team workflow.** One canonical skill repo; every
  member reads/writes in their own language. No "canonical English
  version" politics.
- **Audit and compliance** in any language without a separate
  translation step.
- **Versioning at the abstract layer** — translation differences don't
  create false-positive diffs.
- **Provable equivalence** — "these two skills are the same" becomes a
  mechanical check.

**Costs:**

- Language-neutral abstract syntax is genuinely hard.
- Lexicon maintenance: every new term lands in every concrete grammar.
- Untranslatable atoms (`OAuth`, `git rebase`) need explicit "opaque
  foreign atom" productions.
- Roundtrip stability becomes a CI-grade property test matrix.

## 6. The closed glossary keystone

A single explicit **glossary** that defines all permissible word
choices, with every natural-language vocabulary item mapped into it,
collapses the entire problem.

Without a closed glossary, "Layer 0 free prose → Layer 1 strict CNL"
is an open-vocabulary natural-language interpretation problem. With
a closed glossary, it is a **closed-world keyword-substitution +
structural-rewrite problem**, which is dramatically simpler:

- The LLM normaliser maps free-text mentions into one of N glossary
  entries — a classification task, not a generation task.
- Constrained decoding becomes nearly trivial; the grammar's
  terminals are the glossary keys.
- Roundtrip across languages becomes a deterministic table lookup.
- "Untranslatable atoms" stop being a problem: anything not in the
  glossary is preserved verbatim by definition.
- The CIDOC-CRM ontology discipline now has a concrete artifact: the
  glossary *is* the ontology, with per-language label sets attached.

The seed glossary covers the skill domain only (skill, step, flow,
capability, budget, needs, in, out, do, on_fail, retry, escalate,
plus type primitives). New domains add new entries; new languages add
a label key.

This is the keystone. Every later component fits once it is in place.

## 7. Why dhnt as the canonical machine form

The user's prior research at https://github.com/dhnt/dhnt is a
constructed interlingua designed to unify all languages —
programming, constructed, natural — into a single normalised form:

- **Alphabet**: 26 chars (a-z), 5 vowel-group rows, 110 syllables.
- **Phonotactics**: CV-only. Every string decomposes into `(V|CV)+`.
- **TAM-less + inflection-free**.
- **Universal vocabulary import** via defined transformation rules
  (English as-is, Chinese via Pinyin, Latin/Cyrillic via ISO-9,
  Esperanto with diacritic mapping). Programming languages are dhnt
  dialects by construction (same alphabet).
- **Built-in numerals**: `ju`-prefix decimal, `bu`-prefix binary,
  `pu`-prefix hex.

dhnt becomes Layer 1.5: the internal canonical form to which all
glossary identifiers and structural keywords are encoded. It is
*purely machine-facing* — humans never have to read it. Anyone *can*
validate by running the transpiler.

What dhnt buys the architecture:

1. **Universal identifier scheme.** Every glossary concept has a dhnt
   form. CIDOC-CRM-style URIs collapse into clean dhnt strings.
2. **Programming languages are first-class.** Embedded code fragments
   in Python/Go/JS are dhnt dialects already.
3. **Translation rules replace translation tables.** Multilingual
   import is algorithmic.
4. **Roundtrip becomes deterministic at the identifier level.**
5. **Phonotactic regularity** makes constrained decoding trivial.
6. **Loan-word handling is in the spec** — `OAuth`, `git rebase`,
   project names preserved by the as-is rule.
7. **Numerals are universal** — dates and IDs lose language-specific
   formatting.

## 8. Validity is defined by transpilability

The unifying insight: **a skill expression is valid iff it transpiles
cleanly into Layer 1.5.** The dhnt encoder *is* the validator. There
is no separate validation pass.

This collapses the constraint-compliance question into a single rule:

| Layer | a-z only? | Notes |
|---|---|---|
| Layer 0 | No — free prose, source language, anything goes | Author-facing |
| Layer 1 | No — CNL in source language, normal script | Human-readable display |
| Layer 1.5 | **Yes** (excluding text-slot payloads) | Canonical machine form; the validator |
| Layer 2 | **Yes** for identifiers + numerals; text slots tagged | Internal AST |
| Layer 3 | Wasm bytecode (binary) | Outside the language |
| Tooling artifacts (glossary YAML, paths, configs) | No constraint | Outside the language being designed |

The constraint applies *only* where it matters: the canonical
machine form (Layer 1.5) and the structural identifiers in the AST
(Layer 2). Tooling formats and human-facing layers are unconstrained.

## 9. The architecture that emerged

```
[Layer 0]    free prose in any natural language; anything goes.

   ↓ LLM, constrained-decoded against the glossary;
     output is slot-filled AST nodes (dhnt-keyed)

[Layer 2]    typed AST. Identifiers in dhnt; effects, capabilities,
              budgets typed. Versioned, hashed, diffed at this layer.

   ↓ linearise(AST, lang)         ↓ linearise(AST, dhnt)

[Layer 1]    CNL display in       [Layer 1.5]   dhnt canonical form
             source language.                  (a-z + spaces only).
             For human review.                  For hashing, diffing,
                                                 version control,
                                                 machine-to-machine.

   ↓ interpret + dispatch (from Layer 2)

[Layer 3]    Wasm Component Model leaves via WIT + AST orchestrator.
```

Layers 1 and 1.5 are both **linearisations of Layer 2** — different
projections of the same identity. The LLM's job is *slot-filling
into Layer 2*, not free-text generation; constrained decoding handles
the grammar.

## 10. Specific design decisions and their rationale

### Reserved-word collisions

dhnt reserves `u` (kind), `azu` (all/any), `zu` (collection/plural),
`bu` (binary), `ju` (decimal), `pu` (hex/logic). Glossary entries
whose dhnt form starts with these can collide.

**Solution**: tokenizer lookahead (reserved prefixes recognised only
when followed by their defined suffix sets) + glossary lint at
admission time.

### Toneless Pinyin homophones

Standard Hanyu Pinyin without tone marks collapses `mā / má / mǎ /
mà` to `ma`. This loses Chinese disambiguation.

**Solution**: glossary keys are dhnt-derived from the *primary
import language* (typically English). Chinese is a secondary
projection, not the identity source. Per-language label
disambiguation lives in the labels list.

### Numeral parse ambiguity

dhnt numerals interact subtly with the contraction rule.

**Solution**: always emit numerals in **full form** with the
`ju`/`bu`/`pu` prefix in Layer 1.5; contraction is a display-time
optimisation only. Eliminates roundtrip ambiguity at storage.

### Mixed-language input

Real Chinese tech writing freely mixes English. Code-switching is the
default, not the exception.

**Solution**: glossary keywords are matched by dhnt form, not by
per-language label. The LLM normaliser dhnt-encodes everything
(Chinese via Pinyin → dhnt; English as-is → dhnt) and matches against
glossary dhnt keys. Code-switching collapses at Layer 2.

### Free-text intent slots

Free-text (intent / rationale) is per-author and per-language; not in
the glossary.

**Solution**: two-atom IR. Concept-level identifiers stored as dhnt
forms. Free-text intent / rationale slots stored as `text[lang,
content]` — preserved verbatim with a language tag, passed through to
LLM tool calls. The validator skips them.

### LLM normaliser job specification

Earlier framings had the LLM produce Layer 1 then a deterministic
post-processor produce Layer 1.5. That is over-staged.

**Solution**: the LLM goes Layer 0 → Layer 2 directly via slot-filling
against the glossary, constrained-decoded. Layers 1 and 1.5 are both
linearisations of Layer 2.

## 11. Effort calibration

The realism arc through the design:

| Constraint level | Estimated alpha effort |
|---|---|
| Full multilingual research-grade vision | 3+ years, dedicated team |
| Multilingual CNL with general grammar | 6–12 person-months |
| Multilingual + Cucumber/Gherkin shape (restricted vocab + CIDOC-CRM discipline) | 3–6 person-months |
| + Closed glossary keystone | 6–10 person-weeks |
| + dhnt as machine form (this drop) | ~3 person-weeks for the deterministic core |

Each refinement collapsed the problem. The keystone insights are: (a)
the closed glossary turns LLM normalisation into classification, not
generation; (b) dhnt turns identifier translation into deterministic
algorithm, not table maintenance.

## 12. What this drop validates and what's deferred

**Phase 0 alpha (shipped):**

- dhnt encoder/decoder per spec subset.
- Closed multilingual Glossary with bidirectional lookup.
- Layer 2 typed AST.
- Layer 2 → Layer 1.5 lineariser (a-z + spaces only).
- Layer 1.5 → Layer 2 parser.
- Layer 2 → Layer 1 lineariser per language (EN, ZH).
- Roundtrip + property tests.
- e2e roundtrip in `internal/integration/`.

**Deferred:**

| Component | Why |
|---|---|
| LLM constrained-decoded slot-filler (Layer 0 → Layer 2) | Requires xgrammar/llguidance plumbing; separable; depends on stable AST + glossary. |
| Wasm Component Model leaves (Layer 3) | Runtime concern, not authoring. Standard host/plugin shape. |
| Pinyin tonal disambiguation | Not needed for the skill domain seed. |
| Cyrillic ISO-9 / Esperanto / Unicode hex form | Per-language additions; not blocking. |
| Bridge to existing `internal/runtime/skillengine/` | Follow-up. |

The alpha proves the keystone (glossary + dhnt + roundtrip) without
committing to runtime concerns.

## 12a. Where the code lives (post-extraction)

The deterministic core has been extracted upstream to
[`github.com/dhnt/dhnt`](https://github.com/dhnt/dhnt) under the
Apache-2.0 license — the intent is for this to become a community-
shared reference implementation that any agent harness can adopt,
not just ycode. The dhnt repository now hosts both the language
specification (`dhnt.md`) and the Go reference implementation:

- `github.com/dhnt/dhnt` — encoder, IsCanonical, numeral codec.
- `github.com/dhnt/dhnt/skills` — Glossary, AST, lineariser, parser.

ycode consumes this as a regular Go module dependency
(`require github.com/dhnt/dhnt v0.1.0-alpha.1`). During development
the `peers/dhnt` convention (gitignored local clone wired into
`go.work`) lets ycode iterate against an in-progress dhnt branch.
ycode-specific assets (the domain glossary at
`assets/skillcnl/ycode-glossary.yaml`, the e2e test) live in the
ycode tree. The local `internal/runtime/skillcnl/` package has been
removed in favour of the upstream module.

This split aligns with the original intent: dhnt and skill-CNL are
infrastructure for many possible LLM agent harnesses; ycode is one
consumer.

## 13. References

- dhnt language specification — https://github.com/dhnt/dhnt
- Grammatical Framework — http://www.grammaticalframework.org
- Inform 7 — https://ganelson.github.io/inform-website/
- Attempto Controlled English — http://attempto.ifi.uzh.ch
- Cucumber / Gherkin localisation —
  https://cucumber.io/docs/gherkin/reference/#spoken-languages
- WikiData multilingual labels — https://www.wikidata.org
- CIDOC-CRM ontology — https://www.cidoc-crm.org
- ISO TC37 terminology standards — https://www.iso.org/committee/48104.html
- DSPy — https://dspy.ai
- AICI (AI Controller Interface) — https://github.com/microsoft/aici
- Outlines / xgrammar / llguidance — constrained-decoding libraries
- WebAssembly Component Model + WIT —
  https://component-model.bytecodealliance.org
- CUE — https://cuelang.org
- Soufflé (Datalog with provenance) — https://souffle-lang.github.io
- OPA / Rego — https://openpolicyagent.org
- TLA+ — https://lamport.azurewebsites.net/tla
- CWL — https://commonwl.org
- Code-as-Policies, TaskWeaver, Voyager, ProgPrompt — recent
  ICAPS/NeurIPS LLM-planning literature (2024–2026)

The full conversational design history that produced this document is
archived at
`~/.claude/plans/skills-optimization-when-composing-silly-pnueli.md`.
This doc is the distilled rationale; that file is the working notes.
