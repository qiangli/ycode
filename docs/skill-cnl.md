# skill-CNL — multilingual Controlled Natural Language for skills

A skill today is `instructions + scripts`, where the script half forces a
commitment to a programming language (Python, Node, Go, bash). skill-CNL
replaces the script half with a **typed AST** that has two parallel
projections: a human-readable Controlled Natural Language in the author's
language (Layer 1), and a machine-internal canonical form (Layer 1.5,
written in `dhnt` — see [the dhnt spec](https://github.com/dhnt/dhnt)).

Validation is defined by transpilability: a Layer 1 expression is valid
iff it transpiles cleanly into Layer 1.5. The dhnt encoder *is* the
validator.

This package lives at `internal/runtime/skillcnl/` and is gated behind
the `experimental` build tag. Phase 0 of skill-CNL ships the deterministic
core (encoder, glossary, AST, roundtrip); the LLM normaliser and Wasm
leaf adapter follow later.

## Why this shape

- **Programming-language commitment is per-skill today.** A bash skill
  cannot trivially become a Python skill; a Python skill cannot be
  reviewed by a non-Python reader. Both problems disappear when the
  intermediate is a typed AST keyed by language-neutral identifiers.
- **Authoring is per-author.** A Chinese author wants Chinese; an
  English reviewer wants English. Both views must be deterministic
  projections of the same skill identity.
- **The toolchain wants determinism.** Lint, diff, hash, replay, test —
  all of these need a canonical form. Free prose does not give you
  that. Layer 1.5 (dhnt) does.

## The four layers

```
[Layer 0]   Free prose in any natural language. Author-facing.
              No constraints. May contain code fragments, foreign atoms.

   ↓ LLM normaliser (Phase 1+, not in this drop)

[Layer 1]   Glossary-locked CNL in the author's language.
              Reads as restricted natural language.
              Vocabulary ⊂ glossary[lang]. Free-text intent slots
              preserved verbatim with a language tag.

   ↓ deterministic glossary lookup + dhnt encoder

[Layer 1.5] dhnt — canonical machine form. Strictly a-z (and spaces
              between syllables/words). Every glossary identifier is
              its dhnt key. Numerals via the ju/bu/pu prefix system.
              Free-text intent slots preserved as opaque text atoms.
              **Validity = "this transpiles" — humans are not required
              to read it; if they choose to, they can validate by
              running the transpiler.**

   ↓ trivial regular parse

[Layer 2]   Typed AST. Identifiers in dhnt; effects + capabilities
              + budgets typed. This layer is what gets versioned,
              hashed, diffed, and dispatched.

   ↓ interpret + dispatch (Phase 2+)

[Layer 3]   Wasm Component Model leaves via WIT + AST orchestrator.
```

## The glossary keystone

The `Glossary` (see `glossary.go`) is a closed multilingual lexicon. Each
entry has:

- `Dhnt` — canonical identifier in `[a-z]+`, derived from the primary
  English label by the dhnt loan-word rule.
- `Kind` — one of `keyword`, `capability`, `type`, `primitive`.
- `Labels` — map from language tag to a list of synonyms in that
  language. The lookup is bidirectional: label → entry, dhnt → entry.

Sample entry (YAML, glossary tooling format — note that YAML is *outside*
the language; the constraint applies only to skill content):

```yaml
- dhnt: sotepe
  kind: keyword
  labels:
    en: ["step", "action"]
    zh: ["步骤", "动作"]
- dhnt: nidi
  kind: keyword
  labels:
    en: ["needs", "requires"]
    zh: ["需要", "依赖"]
- dhnt: giti
  kind: capability
  labels:
    all: ["git"]            # foreign atom: same surface in all languages
```

The seed glossary covers the skill domain only (skill, step, flow,
capability, budget, needs, in, out, do, on_fail, retry, escalate, plus
the type primitives). New domains add new entries; new languages add a
label key.

## dhnt encoder rules (alpha subset)

Implemented in `dhnt.go`:

- **Alphabet**: 26 chars, 5 vowel-group rows. Each consonant has a
  "row vowel" (b/c/d→a, f/g/h→e, j/k/l/m/n→i, p/q/r/s/t→o, v/w/x/y/z→u).
- **Vowel insertion**: between any two adjacent consonants, insert the
  *first* consonant's row vowel. After a final consonant, insert that
  consonant's row vowel.
- **Contraction**: a CV-syllable's vowel may be dropped when followed by
  a consonant or end-of-word (this is the inverse — humans can read
  contracted forms; the encoder always emits the full form for
  determinism).
- **English import**: lowercase, then apply vowel insertion.
- **Pinyin import**: toneless Hanyu Pinyin is treated as English (it is
  already a-z lowercase). Tonal disambiguation is deferred.
- **Numerals**: `ju`-prefix decimal (a=1..i=9, j=0). Always emitted in
  full form in Layer 1.5; contraction is display-only.
- **Reserved-word lookahead**: `bu`, `ju`, `pu` are recognised as
  numeral prefixes only when followed by their defined suffix sets;
  otherwise they are part of a regular word.

The encoder treats free-text intent slots as opaque atoms — they are
preserved with a language tag and never re-encoded. The validator skips
them.

## Roundtrip property

For any AST `a` produced by the loader or the parser:

```
parse(linearise(a, dhnt))  ==  a
```

This is the central correctness guarantee. The unit-test suite includes
a property test exercising it across a generated set of valid ASTs.

For the human-readable projection:

```
ast = parse(linearise(a, dhnt))
linearise(ast, "en")  == deterministic English rendering
linearise(ast, "zh")  == deterministic Chinese rendering
```

EN and ZH outputs are deterministic by glossary lookup; equivalence
between them is established at the AST layer, not by translation.

## What's in this drop (Phase 0)

| Component | Status |
|---|---|
| dhnt encoder/decoder (alpha subset) | shipped |
| Glossary types + YAML loader + skill-domain seed | shipped |
| Layer 2 typed AST | shipped |
| AST → Layer 1.5 lineariser | shipped |
| Layer 1.5 → AST parser | shipped |
| AST → Layer 1 lineariser (EN, ZH) | shipped |
| Roundtrip + property tests | shipped |
| e2e roundtrip in `internal/integration/` | shipped |

## What's deferred

| Component | Why deferred |
|---|---|
| LLM constrained-decoded slot-filler (Layer 0 → Layer 2) | Requires xgrammar / llguidance plumbing; separable; depends on stable AST + glossary. |
| Wasm Component Model leaf adapter | Same — runtime concern, not authoring. |
| Full dhnt spec compliance (Cyrillic ISO-9, Esperanto diacritics, Unicode hex form) | Out of scope for the skill domain alpha; can be added per-language. |
| Pinyin tonal disambiguation | Toneless Pinyin is sufficient for the skill domain seed. |
| Multi-language skill registry / discovery integration | Lives in the existing `internal/runtime/skillengine/`; bridge in a follow-up. |

## File map

```
internal/runtime/skillcnl/
  doc.go              package overview + experimental build tag
  dhnt.go             encoder/decoder per the dhnt spec subset
  dhnt_test.go        unit tests + roundtrip property
  glossary.go         types + YAML loader
  glossary_test.go    loader + bidirectional lookup tests
  ast.go              Layer 2 types
  linearise.go        AST → Layer 1.5  +  AST → Layer 1 (lang)
  parse.go            Layer 1.5 → AST
  roundtrip_test.go   parse(linearise(a, dhnt)) == a property test
  testdata/
    glossary.yaml     skill-domain seed (EN + ZH)
    fixtures/         hand-authored ASTs + their expected projections

internal/integration/
  skillcnl_e2e_test.go   AST → 1.5 → AST → EN/ZH roundtrip
```

## References

- Plan / research note: `~/.claude/plans/skills-optimization-when-composing-silly-pnueli.md`
- dhnt spec: https://github.com/dhnt/dhnt
- Grammatical Framework: http://www.grammaticalframework.org
- CIDOC-CRM ontology discipline: https://www.cidoc-crm.org
- Cucumber/Gherkin localisation: https://cucumber.io/docs/gherkin/reference/#spoken-languages
