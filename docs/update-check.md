# The update check

The daemon asks GitHub whether a newer Shepherd release exists, about once a day, and
shows a notice the person can dismiss. It only tells: it downloads and installs nothing.
To upgrade, install the new release and run `shepherd daemon restart`
([INSTALL.md](INSTALL.md)).

## What it does

- About a minute after the daemon starts, then every 24 hours, the daemon lists the
  releases of `ubixsys/ubixshepherd` on GitHub's public API. Pre-releases count, since
  every release so far is a beta.
- It compares the newest one on the channel with its own build version as semantic
  versions, `-beta.N` suffixes included: `v0.4.0-beta.2` is older than `v0.4.0-beta.10`,
  and both are older than `v0.4.0`.
- The answer is cached in the daemon. Clients (the web UI, `shepherd chat`, the CLI) ask
  the daemon and never call GitHub themselves.
- A check that fails (offline, rate limited, a bad answer) is logged at debug level and
  tried again at the next interval. The last good answer stays.

## Settings

In `~/.shepherd/config.yaml` (a reload applies a change, no restart):

```yaml
update:
  check: true      # false turns the check off; the daemon never calls GitHub for it
  channel: beta    # beta: every release; stable: full releases only
```

`SHEPHERD_NO_UPDATE_CHECK=1` in the daemon's environment turns the check off whatever the
config says, for machines with no route out and for tests.

## Where the notice shows

| Where | What |
|---|---|
| Web UI | A slim notice under the bar: "Shepherd vX is available", a link to the release, a Dismiss button |
| `shepherd chat` | One line above the dock; `/update dismiss` hides it |
| CLI | `shepherd update` prints the cached answer; `shepherd update dismiss` hides the notice |

Dismissing hides the notice for that version until a newer one appears. It is stored in
`update.json` next to `config.yaml`, so it survives a daemon restart.

## A build with no release version

A dev build or a source install reports a version that is not a release number (`dev`, or
a Go pseudo-version it cannot rank). Nothing is compared, so Shepherd never claims the
release is newer. It still shows the notice, worded as the latest release being X and this
build being its own, because a person on such a build most wants to know that a release
exists. The notice can be dismissed like any other. A Go pseudo-version that is valid
semver (`v0.0.0-2026...`) is compared as the pre-release it looks like.

## API

Both routes are for the operator token and a signed-in browser session. The front desk and
an agent's worker token get a 403.

| Route | |
|---|---|
| `GET /v1/update` | `current`, `current_known`, `latest`, `url`, `notes` (the release's changelog section as published), `available`, `dismissed`, `show`, `enabled`, `channel`, `checked_at`, `error` |
| `POST /v1/update/dismiss` | body `{"version": "v0.5.0-beta.2"}`. Only the version now offered is accepted (409 otherwise) |

`show` is what a client acts on: the check is on, there is a release, it is not
dismissed, and it is newer than this build or this build is of unknown version.

## What it does not do

It does not upgrade. The check keeps each release's assets (archive names and URLs) in
its cache so a later one-click or automatic upgrade can pick its archive without asking
GitHub again; nothing uses them yet.
