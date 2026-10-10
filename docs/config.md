# Configuration

`~/.shepherd/config.yaml` is written on the daemon's first start with every setting
commented out; the comments are the reference for each field, and the template is
`internal/config/template.yaml`. A repo's profile (under `repos:`) inherits every field it
leaves out from `defaults:`. This page documents the fields that need more than a comment.

## Standards

See [standards-packs.md](standards-packs.md) for what the fields do.

| Field | Default | Meaning |
|---|---|---|
| `standards` | none | Names of Shepherd packs (folders under `~/.shepherd/standards/`) whose rules the repo's agents are told. A repo's list replaces the defaults', and `[]` clears it. |
| `standards_docs` | `docs/standards`, `docs/architecture`, `AGENTS.md`, `CLAUDE.md` | Paths in the repo holding its own standards. Those that exist are named in the brief and win over a pack rule. Relative, inside the repo. |
| `standards_pack_dir` | `.shepherd/standards` | The repo's own pack directory. A rule with a Shepherd rule's id replaces it; `disabled: true` switches it off. |
| `standards_when` | none | Extra `when` tags active in every lane of the repo. |
| `autonomy.plan_first` | `true` | The brief tells the agent to post its plan as a decision and wait before changing code. |

## Unknown and retired keys

The file is read strictly: a key Shepherd does not know is an error, so a typo cannot
silently fall back to a default. The one exception is a key Shepherd itself retired or
renamed. Such a key is listed in `config.RetiredKeys` (dotted path to a note, `*` matching
any key at that level, e.g. `repos.*.old_key`); a file that still has it loads with the key
ignored and one warning per occurrence, naming the key and what replaces it. The daemon
logs the warnings when it starts, and `shepherd daemon restart`, `daemon reload` and
`daemon check` print them. Edit the key out of the file to silence the warning. The list
is empty until a key is first retired.
