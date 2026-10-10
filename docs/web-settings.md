# Web settings

The **Settings** tab of the web UI (`shepherd web`) reads and edits Shepherd's
`config.yaml`: the daemon, the front desk, chat, OpenCode, the defaults, each repo's profile
and each project's budget. It is the same file the CLI and a text editor use; nothing is
kept elsewhere.

## What it does

- Shows each repo's profile with its own values and the ones it inherits from the
  defaults. An inherited value is greyed with its source; the **Override** box makes it
  the repo's own, and unticking it removes the key so the default applies again.
- Previews every change as a diff of `config.yaml` before saving, and shows validation
  problems beside the field they name.
- Saves by editing the file in place: comments, key order and sections it was not asked
  about are kept. The previous file is kept as `config.yaml.bak.<time>` beside it (the
  last 10), and the file is replaced atomically.
- Reloads the daemon's config in process after a save, as `shepherd daemon reload`
  does, so it works on every platform. Running agents keep what they started with;
  `daemon.listen` and `daemon.poll` need a restart (the form says so).
- Refuses to save over a file that changed on disk since the page read it, so a hand
  edit is never clobbered. Reload the page's copy and review the change again.

The writer round-trips YAML through a node tree. Indentation is normalised to two
spaces and blank lines between entries may move; values are never reinterpreted. A
file that is only comments (the first-start template) keeps them all and gets new keys
appended.

## Projects

Projects live under `projects:` in `config.yaml`. Settings edits them in the same
preview, save and etag flow as everything else; nothing is written until the diff is
reviewed and saved.

- **Add a project.** The *add a project* box in the sidebar, beside *add a repo*, takes a
  name (letters, digits, dots, dashes and underscores) and starts the project with an
  empty `repos` list, so it is written even before it has a repo. Its other fields
  (budget, brief age, caches, intake rules) are the project's page of the form.
  The **New project** button on the Projects page opens Settings with that box ready.
- **Repos.** A project's repos are a checklist over every configured repo. Each repo says
  which project it is in now. A repo is in at most one project, so ticking one that
  another project holds moves it: the draft changes both projects' `repos` lists, and the
  review shows both as one diff. Unticking leaves the repo in no project, where the
  Board lists it as Unassigned.
- **Delete a project.** *Delete project…* on its page asks first, naming the repos that
  become Unassigned and warning that its budget, caches and intake rules go with it.
  Confirming only adds the removal to the draft; the review and save make it real. A rule
  in another project that names the deleted one as `from` fails validation, so the save
  is refused with that message until the rule is changed.
- **Edit** on a project's page (`#/projects/<name>`) opens `?scope=project:<name>`.

A session that cannot save sees none of the add or delete controls, as for repos, but the
repo checklist still previews.

## Who may save

| Caller | Read and preview | Save |
|---|---|---|
| Operator token (the CLI, `curl`) | yes | yes |
| Browser session | yes | only if its sign-in link asked for it |
| Front desk, agents | no | no |

A browser session is read-only by default. `POST /v1/web/signin` takes an optional body
`{"allow_settings": true}`; the session that link starts may save. The CLI exposes this
as `shepherd web --allow-settings`. The flag lives with the session in daemon memory
and ends with it: after a daemon restart, a session reloaded from the store reads but
cannot save, so sign in again. The UI says so when the session cannot save, and still
lets you review the diff a save would make.

The front desk is never given these routes: it could otherwise widen its own autonomy.

### Changes that need confirming

A change that gives agents more power is refused by the server (HTTP 428) until its
path is in the request's `confirm` list, and the UI asks for a typed `confirm`. These
are, by effective value of a repo or the defaults:

- `autonomy.push`, `merge`, `tag` or `deploy` moving toward `shepherd` or `agent`
- `autonomy.plan_first` turned off
- `agent.permission_mode` set to `bypassPermissions`
- `gate` emptied
- an entry removed from `forbid`

A repo that merely inherits a raised default is named once, under `defaults`.

## API

All under `/v1/config`; types in `internal/api/config.go`.

| Request | Answer |
|---|---|
| `GET /v1/config` | `ConfigDoc`: `etag`, the file as written (`raw`), the config it gives (`effective`), `defaults` and `repos` each as `{raw, effective, overridden}`, `projects`, `can_write`, and `sections` (the form's field description) |
| `POST /v1/config/preview` | `ConfigPreview`: the diff and any `risky` changes; nothing is written |
| `PUT /v1/config` | `ConfigSaved`, or a refusal below |

A change is `{etag, ops, confirm}`. Each op is `{path, value}` to set a key, or
`{path, unset: true}` to remove it; `path` is the keys from the top of the file
(`["repos", "api", "autonomy", "push"]`). The result must pass `config.Parse` (the same
check as at daemon start, unknown keys included) before anything is written.

| Status | Meaning |
|---|---|
| 403 | The caller may not save (a browser session not signed in with `allow_settings`, or the desk or a worker) |
| 409 | The file changed on disk since `etag` was read |
| 422 | The result is not a valid config; `problems` says why |
| 428 | The change needs confirming; `risky` lists the paths to send back in `confirm` |

## Adding a field

The form is built from `sections` in `GET /v1/config`. Each section's fields come from
the `yaml` tags of the structs in `internal/config` by reflection (`configSections` in
`internal/daemon/configfields.go`), so a field added to `Profile`, `Daemon`, `Desk`,
`Chat`, `OpenCode` or `Project` appears as a plain input of the right kind (string, number,
bool, list, map, or JSON for a structured value) with no change to the web code. To give
it a label, help text, a closed set of choices, a group or a `power` mark, add an entry
to the matching table in `configfields.go`. The standards fields (`standards`,
`standards_docs`, `standards_pack_dir`, `standards_when`) are described this way.

`chat.hyperlinks` (the *Chat* section) is read when `shepherd chat` starts, so a
saved change or a daemon reload does not reach a chat already running: restart the chat
to apply it.

An editor for the standards packs' rule files is not part of this: those are files under
`~/.shepherd/standards/<pack>/`, not keys in `config.yaml`. A later tab can reuse the
same etag, preview and save shape from `internal/configedit` for them.

## What is not here yet

- Restoring a backup from the UI (the files are beside `config.yaml`).
- Creating a project or editing its intake rules and caches field by field: they show
  as JSON.
- A hint from `internal/config` for choices and help (struct tags or an exported table)
  would replace the tables in `configfields.go`.
