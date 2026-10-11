# Models and effort

Which model and which effort a run uses is a policy the person sets in `config.yaml`.
The front desk reads it and chooses per task; Shepherd enforces it at the one place every
run passes, `lane run`, so a desk, a person at the CLI, a follow and a fix all meet the
same rules.

Nothing here is required. With no `models:` block and no rules, a run gets the repo's
`agent.model` for its agent, else the CLI's own default, exactly as before.

## The catalogue

`models.<agent>` describes one agent's models (`claude`, `copilot`, `cursor`, `opencode`).

```yaml
models:
  claude:
    default: sonnet          # a run that names no model, in a repo that names none
    effort: medium           # the workspace's default effort
    catalogue:
      haiku:  {use: "mechanical edits, renames, docs and typo fixes"}
      sonnet: {use: "everyday feature work and tests", efforts: [low, medium, high]}
      opus:   {use: "design-heavy or cross-cutting change, hard debugging",
               efforts: [low, medium, high, xhigh, max], effort: high}
    deny: []                 # removed for every repo
```

- `use` is what the front desk reads to choose. One line, in the person's words.
- The catalogue is the **allow-list**. Once an agent has one, a model that is not in it is
  refused. An agent with no catalogue accepts any name, subject to the rules below.
- `efforts` lists the effort values the model takes. Empty means it takes none.
- `effort` on an entry is that model's own default effort.
- `default` must be in the catalogue.

## Narrowing: deny and allow

Projects and repos narrow what the catalogue left. Both take `deny` (the usual tool) and
`allow` (for the cases a deny-list handles badly), by agent:

```yaml
projects:
  shop:
    repos: [api, web]
    models:
      deny: {claude: ["opus*"]}
repos:
  api:
    agent:
      models:
        allow: {opencode: ["local/*"]}   # this repo's code may only go to the local model
```

`defaults.agent.models` applies to every repo; a repo's own `agent.models` adds to it.

**Why both.** A deny-list is easiest to keep up: a model added to the catalogue later is
usable everywhere without touching every project, and "not opus here" is one line. But a
deny-list fails open: a new cloud model appears in every repo you meant to keep local. An
allow-list fails closed. So the catalogue is the workspace allow-list, `deny` is the
everyday tool below it, and `allow` is there for the repos where the answer is "only
these".

**Precedence**, widest first: the workspace catalogue and `models.<agent>.deny`; the
project's rules; `defaults.agent.models`; the repo's own `agent.models`. At each level
`allow`, if set, keeps only the models it names, then `deny` removes models; deny wins over
allow at the same level. A level can only narrow: a repo cannot allow back what its
project denied.

Names are matched exactly or as globs where `*` stands for any run of characters.
Shepherd cannot know that the alias `opus` and a full model id are one model, so list both
or use a glob (`*opus*`). For OpenCode, `qwen3` and `local/qwen3` are one name (a model
with no provider runs under `local`).

## Choosing the model

For a run the model is, in order: the one asked for (`--model`, `model` on `lane_run`),
else the continued run's model, else the repo's `agent.model` for the agent, else
`models.<agent>.default`, else the CLI's own default.

- A model that was **asked for** and is not permitted is refused, naming the rule:
  `model "opus" is not permitted for claude in repo api: denied by
  projects.shop.models.deny.claude ("opus*"); permitted: haiku, sonnet`.
- A model that was **not asked for** (a continued run's, a repo default, a follow's, a
  fix's) and is no longer permitted falls to the next candidate, and the run's log header
  says so (`# policy: model sonnet used: ...`). If nothing is left the run is refused;
  Shepherd never picks "the first permitted model".
- Loading the config **warns** for each default a rule removes. It does not fail: a deny
  must not stop the daemon.
- With a catalogue, a run that resolves to no model at all is refused: set
  `models.<agent>.default`. The CLI's own default cannot be checked against a rule.

## Effort

Effort is not a standard: each CLI has its own words and its own way to take one, so the
value is the agent's own (Claude Code: `low`, `medium`, `high`, `xhigh`, `max`) and
Shepherd passes it on rather than translating it.

| Agent | How it is passed | When it is not applied |
|---|---|---|
| Claude Code | `--effort <value>` | the model's catalogue entry lists no effort, or not that one |
| Cursor | a parameter of the model id: `model[effort=high]` | the model is not in the catalogue with `efforts`; it takes it only by that route |
| Copilot | not passed: no effort setting is known for the CLI | always |
| OpenCode | not passed: a model on a local OpenAI-compatible host takes none | always |

For a run the effort is: the one asked for (`--effort`, `effort` on `lane_run` and
`run_continue`), else the repo's `agent.effort` for the agent, else the model's catalogue
`effort`, else `models.<agent>.effort`. A continued run does not inherit its parent's
effort: the run record does not keep one yet.

- Effort is a hint, not a boundary. When an agent or model cannot take the effort that was
  asked for, the run starts without it and the log header says so
  (`# policy: effort high not applied: copilot has no effort setting`). A default that
  cannot apply is skipped silently.
- A value the model's catalogue entry does not list is refused. A model with no catalogue
  entry passes the value to the CLI, which judges it.

The log header of each run records the model and effort it ran with.

## For the front desk

`shepherd models [--repo R] [--agent A] [--json]` and the MCP tool `models_list` print, per
agent, the permitted models with their `use`, their efforts, the defaults, and which rule
removed any other model. The desk's brief tells it to call this before `lane_run`, choose
model and effort for the task from the permitted ones, and say which and why in a few
words when it starts the run. It reads the policy live, so a config change applies to the
next run.

## Related

- [config.md](config.md): the whole config file.
- [opencode.md](opencode.md): the OpenCode adapter and its model host.
