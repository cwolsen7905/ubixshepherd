# Standards packs

A standards pack is a folder of plain markdown rules. Shepherd puts the rules that apply
to a lane into the one brief every agent receives, so Claude Code, Copilot, Cursor and
OpenCode (or any agent added later) are told the same thing in the same words. Nothing in
the brief is specific to an agent, and Shepherd writes no `AGENTS.md` or `CLAUDE.md` into
worktrees.

"We're leading the flock, but the flock knows its pasture": **a repo's own standards win**
over Shepherd's wherever they disagree.

## Format

A pack is a directory; each rule is one file, `<id>.md`, with YAML front matter.

```markdown
---
level: must                  # must | should (required)
check: '! git diff "$SHEPHERD_BASE_REF"...HEAD -U0 | grep -E "^\+[^+].*(TODO|FIXME)"'
applies_to: ["src/**"]       # optional globs; empty means everywhere
when: [commit]               # optional tags; empty means always
---
Mark deferred work `NOT_IMPLEMENTED:` or `BY_DESIGN:`, never TODO or FIXME.

## Why

The two markers tell the next reader whether a gap is a bug or a decision.
```

| Part | Meaning |
|---|---|
| file name | The rule's stable id: lower-case words joined by dashes. A front matter `id:` is allowed only if it matches. |
| `level` | `must` or `should`. |
| first paragraph | The statement, shown to the agent on one line. |
| `## Why` section | Rationale, for people. It is never put in a brief. |
| `check` | Optional shell command run in the lane's worktree; exit 0 passes. Makes the rule mechanical. |
| `applies_to` | Optional globs (the scope syntax). The rule is shown only to lanes whose scope reaches them, and its check runs only when the lane's diff touches a matching file. |
| `when` | Optional tags. Shown only when one is active in the lane (below). |
| `disabled: true` | In a repo pack: switch off the Shepherd rule with this id. |

`pack.md` in a pack folder describes the pack and is not a rule. One rule per file keeps
ids visible in git, and lets a settings page read and write a rule as one small object
(`id`, `level`, `statement`, `rationale`, `check`, `applies_to`, `when`); `Rule.Render`
and `standards.Parse` are the two directions.

### Tags

`commit` and `ship` are active in every lane. `test`, `docs` and `migration` are active when
the lane's scope reaches tests, documentation or migration files. A profile's
`standards_when` adds more, active in every lane of the repo. A rule with no `when` is
never filtered by tag.

## Where packs live, and who wins

| Source | Location | Used when |
|---|---|---|
| Shepherd packs | `~/.shepherd/standards/<pack>/` | the repo's profile lists the pack in `standards` |
| The repo's pack | `.shepherd/standards/` in the repo (`standards_pack_dir`) | always, if it exists |
| The repo's own docs | `docs/standards`, `docs/architecture`, `AGENTS.md`, `CLAUDE.md` (`standards_docs`) | named in the brief, those that exist |

Effective rules are resolved in this order:

1. Shepherd packs, in the order the profile lists them. A later pack's rule replaces an
   earlier one's of the same id.
2. The repo's pack. A rule with the same id replaces the Shepherd rule (level and all);
   a rule file with `disabled: true` removes it; other ids are added.

The brief states plainly that the repo's own docs take precedence over the Shepherd digest:
where they disagree the agent follows the repo and mentions the conflict in its report. It
does not stop to ask.

## What the agent is told

For a new session, the brief carries a short digest after the powers and before the task:

- the precedence statement and the paths of the repo's own standards that exist;
- each matching `must` rule in full (`MUST id: statement`);
- each matching `should` rule as one line (`should id: statement`, clipped);
- a one-line pointer to the pack folders for rules that do not apply to the lane;
- the ids the repo switched off.

Rationale is never inlined. A budget (about 2.4 KB of rules) is the backstop: rules past
it are listed by id and file path. A resumed session is not given the digest again.

## Checks

A rule with a `check` is mechanical. Shepherd runs the checks of the effective rules where
it already runs the repo's gate: after the gate passes, before an automatic push and
before `shepherd lane ship`. A check runs only when the lane's diff touches a file matching
the rule's `applies_to` (no `applies_to`: always). Its working directory is the worktree
and `$SHEPHERD_BASE_REF` is the ref the lane's changes are counted from (so
`git diff "$SHEPHERD_BASE_REF"...HEAD` is the lane's work).

- A failing **must** check blocks the push. It goes back to the agent like a gate failure,
  by rule id with the command's output, within the same number of tries; `lane ship`
  refuses with the same text.
- A failing **should** check is reported in the feed and does not block.
- Rules with no `check` are advice: not enforced yet.

## Setup

```
shepherd standards init          # write the example house pack, only if absent
shepherd standards list [--json] # the packs on this machine
shepherd standards show [dir]    # the rules in force for a repo, and a lane's digest
```

The first start of the daemon writes the example `house` pack along with the config. A
copy lives in `examples/standards/house/`. Then in `config.yaml`:

```yaml
defaults:
  standards: [house]
repos:
  my-app:
    standards: [house, php]        # replaces the defaults' list
    standards_docs: [docs/rules, CLAUDE.md]
```

See [config.md](config.md) for the fields.

## plan_first

`autonomy.plan_first` (default `true`) is part of the brief: the agent is told to post its
plan as a decision (`ask_human`) and wait for the answer before changing code; an agent
without Shepherd's tools is told to state the plan and stop. A run Shepherd starts on its
own (a follow lane, a gate fix) is not asked to, since no one is there to answer.
