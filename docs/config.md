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

## Keep-going

See [keep-going.md](keep-going.md) for the mode. The `keep_going:` section holds its
limits; a field left out or `0` takes its default, and a reload (`shepherd daemon reload`
or a SIGHUP) applies a change to the next check.

| Field | Default | Meaning |
|---|---|---|
| `cap` | `5` | Dollars the mode may spend after it is turned on. At the cap it turns itself off. |
| `max_runs` | `2` | Runs that may be running in the workspace while it is on. A start past it is refused. |
| `idle_hours` | `4` | Hours with nothing started or answered before it turns itself off. |
| `day_end` | `00:00` | Local time of day it turns itself off, `HH:MM`. `00:00` and `24:00` are midnight. |
| `lane_starts` | `3` | Autonomous starts a lane may have in a day before it is handed back to the person. |

`shepherd keep-going on --cap USD --runs N --idle HOURS` overrides the first three for
one session. The daily budget is `daemon.budget`: crossing it turns the mode off when the
budget's cap is hard.

## Follow-through owner

By default Shepherd follows a merge request through: a failed pipeline and unresolved review
threads go back to the lane's agent, up to `watch.fix_tries`. A repo can instead name an
external owner, another service that fixes failures and answers threads. Shepherd then stands
back, and takes an item over if the owner does not act. Set it under `defaults:` or a repo's
`watch:`; a repo's fields override the defaults' one by one.

```yaml
repos:
  app:
    watch:
      follow_through:
        owner: external
        name: reviewbot
        patience: 30m
        author: [review-bot]
```

| Field | Default | Meaning |
|---|---|---|
| `owner` | `shepherd` | `shepherd` or `external`. |
| `name` | `external` | One word used for the owner in the thread. |
| `patience` | `30m` | How long the owner may be silent on an item before Shepherd takes it over. At least a minute. |
| `author` | none | Forge usernames whose review threads are the owner's. Required with `owner: external`, and allowed only then. |

With an external owner:

- A failed pipeline is reported but not handed to the agent. If the head pipeline is still
  failed after `patience`, Shepherd hands it to the agent as it would have. A new pipeline
  restarts the wait.
- A review thread opened by an `author` is left to the owner. Threads by anyone else go
  to the agent as usual. A reply by anyone restarts that thread's wait, and a resolved
  thread drops out. After `patience` with no reply, or at once when the owner's own note
  on it begins `Unfixed:`, Shepherd takes the thread over.
- Taking over is per item and final for it. Takeovers spend the same `fix_tries` budget.
- The AI review job retry, merge detection and the rule that Shepherd never approves or
  merges are unchanged.

## Update

The release check (see [update-check.md](update-check.md)): the daemon asks GitHub about
once a day whether a newer release exists and shows a notice you can dismiss. The notice
carries an Upgrade action; `update.auto` lets the daemon take it by itself. The settings are in the `update:` section, and a reload
(`shepherd daemon reload` or a SIGHUP) applies a change.

| Field | Default | Meaning |
|---|---|---|
| `update.check` | `true` | Turns the check on or off. `false` stops the daemon from calling GitHub. |
| `update.channel` | `beta` | `beta` counts every release, pre-releases included; `stable` only full releases. |
| `update.auto` | `false` | Opt in: install a newer release by itself and restart onto it once no agent is running. Needs `update.check`; it only replaces a release binary it may write, and never retries a release that failed. |

`SHEPHERD_NO_UPDATE_CHECK=1` in the daemon's environment turns the check off regardless of
the config.

## Per-repo run limit

`max_runs` under `defaults:` or a repo's entry in `repos:` caps how many agents may be
running in that repo at once. `daemon.max_runs` stays the cap for the whole machine, so
a repo's limit only keeps one busy repo from taking every slot; it cannot raise the
machine's. Unset or `0` means only the machine's limit applies; a negative number is an
error.

```yaml
repos:
  ubixos:
    max_runs: 2
```

A start past the limit (`lane run`, `run continue`, a routed request or a keep-going
start) is held, not queued: it is refused with a message naming the repo, and a run
Shepherd starts on its own is tried again when a slot frees, exactly as for
`daemon.max_runs`. A reload applies a change.

## Projects and the `repos:` section

A project's `repos:` lists repo names; each name need not also appear under the top-level
`repos:`. A repo with no entry there simply uses the `defaults:` profile, so grouping
repos needs no `name: {}` line per repo. Add a `repos:` entry only to override something
for that repo. The same holds for the repo a cache's `made_by` names. A repo is still in
at most one project, and a repeated name is an error.

Parsing cannot know which names are real repos of your workspace, so a name that matches
no repo is not rejected: it simply never matches anything.

## Web

The `web:` section holds settings for the browser UI.

| Field | Default | Meaning |
|---|---|---|
| `editor_url` | `vscode://file/{path}` | A URL template for "open in editor" links on the lane page. `{path}` is the file's absolute path (URL-escaped), `{line}` is optional. `{path}` must be present; only `{line}` may be omitted. Empty value hides the links. The scheme must open an application (e.g. `vscode`, `cursor`), not `javascript`, `data`, `vbscript`, `file`, `blob`, or `about`. Validated when the config loads. |

Examples: `vscode://file/{path}:{line}`, `cursor://file/{path}`, `{path}:{line}`.

## Unknown and retired keys

The file is read strictly: a key Shepherd does not know is an error, so a typo cannot
silently fall back to a default. The one exception is a key Shepherd itself retired or
renamed. Such a key is listed in `config.RetiredKeys` (dotted path to a note, `*` matching
any key at that level, e.g. `repos.*.old_key`); a file that still has it loads with the key
ignored and one warning per occurrence, naming the key and what replaces it. The daemon
logs the warnings when it starts, and `shepherd daemon restart`, `daemon reload` and
`daemon check` print them. Edit the key out of the file to silence the warning. The list
is empty until a key is first retired.

## Auto-widen

By default every widening of a lane's scope is the person's call. A repo can let Shepherd
widen by rule instead, for a small request that stays near the lane's work. No model
decides. Set these under `defaults:` or under a repo in `repos:`.

`widen` is the switch, and it is off until a repo sets it to `auto`. The daemon reads it
when it starts, so turning it on needs a restart.

| Field | Default | Meaning |
|---|---|---|
| `widen` | `human` | `auto` turns the rule on; `human` leaves every widening to the person. |
| `widen_max_files` | 3 | The most files one auto-widen may add. |
| `widen_cap` | 3 | The most auto-widens one lane may have; after that the person is asked. |
| `widen_protected` | none | Globs added to the built-in protected list. A repo's list extends the defaults' list. |

An auto-widen happens only when the request names exact files (no globs) in the same
repo, no other open lane's scope overlaps them, each is in the directory of a file the
lane changed or is a test of one, and none is protected. The built-in protected list
covers CI definitions (`.gitlab-ci.yml`, `ci/`, `.github/`), migrations, `go.mod`,
`go.sum`, `package.json`, other manifests and lockfiles, deploy and secret files
(`deploy/`, `secrets/`, `.env*`, keys), the files that define a lane's own checks
(`Makefile`, `bin/`, `scripts/`, hooks, lint and test configuration) and `docs/WORK.md`. If any condition fails, the
request goes to the person as before.

It acts in two places: when a run ends with files outside its scope, and when an agent asks
a decision that explicitly asks to widen the scope and names the files in backticks, with no
options or alternative such as a revert. In the first the run is continued and told its scope
now includes the files. In the second the decision is answered with a note that says
Shepherd's rule answered it, not the person; the question stays on record. If continuing the
run fails, the widening is undone and the request goes to the person.

Each auto-widen is a feed item naming the files and the checks that passed, and the
merge request description lists the lane's auto-widens.

```yaml
repos:
  myrepo:
    widen: auto
    widen_max_files: 2
    widen_protected: ["internal/billing/**"]
```

## Escalating a follow-up to Claude

When a follow-up round on a merge request (review threads or a failed pipeline) ran on a
cheaper agent or model and left the same items open, the next round runs on Claude Code. See
[follow-through.md](follow-through.md). It is on by default; turn it off under `defaults:` or a
repo's `watch:`.

```yaml
repos:
  app:
    watch:
      escalate: false
```

| Field | Default | Meaning |
|---|---|---|
| `escalate` | `true` | Hand the next round to Claude when the round before it, on another agent or a Claude model other than the repo's default, did not close. |
