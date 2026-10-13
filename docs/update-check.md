# The update check

The daemon asks GitHub whether a newer Shepherd release exists, about once a day, and
shows a notice the person can dismiss. From the notice the person can upgrade in one
click, and can opt in to the daemon doing it by itself ([Upgrading](#upgrading)).

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
  auto: false      # true: install a newer release by itself (see Upgrading)
```

`SHEPHERD_NO_UPDATE_CHECK=1` in the daemon's environment turns the check off whatever the
config says, for machines with no route out and for tests.

## Where the notice shows

| Where | What |
|---|---|
| Web UI | A slim notice under the bar: "Shepherd vX is available", a link to the release, an Upgrade button, a Dismiss button |
| `shepherd chat` | One line above the dock; `/update upgrade [now]` installs, `/update dismiss` hides it |
| CLI | `shepherd update` prints the cached answer; `shepherd update upgrade [--now]` installs; `shepherd update dismiss` hides the notice |

Dismissing hides the notice for that version until a newer one appears. It is stored in
`update.json` next to `config.yaml`, so it survives a daemon restart.

## A build with no release version

A dev build or a source install reports a version that is not a release number (`dev`, or
a Go pseudo-version it cannot rank). Nothing is compared, so Shepherd never claims the
release is newer. It still shows the notice, worded as the latest release being X and this
build being its own, because a person on such a build most wants to know that a release
exists. The notice can be dismissed like any other. A Go pseudo-version that is valid
semver (`v0.0.0-2026...`) is compared as the pre-release it looks like.

## Upgrading

`POST /v1/update/upgrade` (the notice's Upgrade button, `shepherd update upgrade`,
`/update upgrade`) makes the daemon install the offered release itself. Only the version
now offered is accepted, and only when it is newer than this build.

### What it does

1. **Fetch.** It picks `shepherd-<tag>-<os>-<arch>.tar.gz` (`.zip` on Windows) and the
   release's `SHA256SUMS` from the assets the check cached. Both URLs must be the
   project's own GitHub release downloads; anything else is refused. It does not verify
   the cosign signature on `SHA256SUMS` (neither does `install.sh`); do that by hand
   ([RELEASING.md](RELEASING.md#verifying-a-download)) if you need the proof.
2. **Verify.** The archive's SHA-256 must match its line in `SHA256SUMS`. The binary inside
   is written next to the running one and run as `shepherd version`, which must report
   the target tag.
3. **Swap.** The running binary is copied to `shepherd.previous` beside it, then the new
   one is renamed over it (atomic on macOS and Linux; on Windows the running `.exe` is
   renamed aside first). An `upgrade.json` marker next to `config.yaml` records the
   upgrade as unproven.
4. **Restart.** The daemon restarts when no agent is running. A restart stops every agent
   and records its run as interrupted, so by default it waits: the status says
   `installed` with the number of agents running, and the restart happens at the first
   idle moment. `shepherd update upgrade --now`, the web UI's "Restart now" and
   `/update upgrade now` restart at once and interrupt them, which is the person's call
   (keep-going's rerun-after-restart applies as for any restart).

   The process does not exit and relaunch through the service manager: on macOS and Linux
   it replaces itself with the new binary (`exec`), so launchd or systemd see one daemon
   that never stopped. On Windows it starts the new binary detached and exits.

### Which installs it replaces

| Install | What happens |
|---|---|
| Release binary in a directory the daemon user can write (`install.sh`, an unpacked archive) | Replaced in place |
| Homebrew (`Cellar`, `linuxbrew`) | Not replaced; the notice says `brew upgrade shepherd`, then `shepherd daemon restart` |
| `go install` (`GOBIN` or `GOPATH/bin`) | Not replaced; the notice says the `go install ...@<tag>` command |
| A development build, or a version Shepherd cannot rank | Not replaced; the notice says to update the checkout and `make install` |
| A release binary in a directory the daemon cannot write | Refused with a message |

There is no Homebrew formula yet; the detection is so a future one is not overwritten.

### Failure and rollback

- Anything that fails before the swap (a download, a checksum, the version check, a
  directory it cannot write) leaves the running binary untouched. The status says
  `failed` with the reason, and the failed release is remembered in `update.json` so
  `update.auto` does not retry it. A newer release, or the person pressing Upgrade, tries
  afresh.
- Once swapped, the new daemon proves itself: it counts its launch in `upgrade.json` and
  removes the marker after serving 30 seconds. A launch that finds the marker already
  counted once (a crash, or a start that failed before the 30 seconds) restores
  `shepherd.previous`, clears the marker, records the release as failed with the reason
  `rolled_back`, and starts the old binary. A clean stop within the 30 seconds is not a
  failure. If the new binary cannot even be executed, the same restore happens at once.
- `shepherd.previous` stays after a good upgrade; it is the manual rollback
  (`mv shepherd.previous shepherd`, then `shepherd daemon restart`). There is no
  downgrade of the store, as for any release ([INSTALL.md](INSTALL.md#upgrade)).

### Automatic upgrades

`update.auto: true` makes the daemon do this by itself, checked every five minutes, when
the check is on and a newer release exists on the channel. It skips a release the person
dismissed, one an earlier attempt failed for, and any install it would not replace. The
restart waits for an idle moment as above, never interrupting agents. It is off unless
set, and is a **risk** setting in the web settings page.

## API

The routes are for the operator token and a signed-in browser session. The front desk and
an agent's worker token get a 403.

| Route | |
|---|---|
| `GET /v1/update` | `current`, `current_known`, `latest`, `url`, `notes` (the release's changelog section as published), `available`, `dismissed`, `show`, `enabled`, `channel`, `checked_at`, `error` |
| `POST /v1/update/dismiss` | body `{"version": "v0.5.0-beta.2"}`. Only the version now offered is accepted (409 otherwise) |
| `POST /v1/update/upgrade` | body `{"version": "v0.5.0-beta.2", "now": false}`. 202 once the install has started; 409 for a stale or not-newer version, an install it will not replace, or an upgrade already running. `now` restarts at once, interrupting agents; alone it restarts onto an already installed upgrade |

`GET` also carries `upgrade`: `state` (`idle`, `installing`, `installed`, `restarting`,
`failed`, `rolled_back`), `method`, `can_upgrade`, `hint` (what to run when the daemon will
not replace the install), `target`, `active_runs`, `error`, `failed_version` and `auto`.

`show` is what a client acts on: the check is on, there is a release, it is not
dismissed, and it is newer than this build or this build is of unknown version.

## What it does not do

It does not verify cosign signatures, does not downgrade, and does not replace an
install another tool owns. It never restarts over running agents unless told to.
