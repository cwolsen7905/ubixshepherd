# Installing uBixShepherd

uBixShepherd is one binary, `shepherd`, with no installer and nothing to compile. This
page covers installing it on macOS, Linux and Windows, a first run, upgrading and
removing it. It is a **beta**: read [Known limits](#known-limits) first.

## What you need

| Need | Why |
|---|---|
| **git 2.31 or newer** | Shepherd drives the git CLI and blocks an agent's pushes with git settings older git ignores silently. The daemon refuses to start on an older git. Ubuntu 20.04 and Debian 11 ship older versions: install a newer git there. |
| **An agent CLI, logged in** | Shepherd starts the agents you already use: `claude` (Claude Code), `copilot`, `cursor-agent`, or `opencode`. You need at least one to run agents; lanes and the pre-push hook work without. |
| **`glab`, logged in** | Only for GitLab repos: merge request polling and pipeline hand-offs read the forge through your own `glab` login. Shepherd stores no forge token. GitHub is not yet a forge for lanes, so `gh` is not used. |
| **A workspace of git repos** | A directory such as `~/git` holding the repos Shepherd manages. |

Shepherd keeps its files in `~/.shepherd` (`%USERPROFILE%\.shepherd` on Windows), or in
`$SHEPHERD_HOME` if set. The daemon listens on loopback only.

## Install

Releases are on [GitHub Releases](https://github.com/ubixsys/ubixshepherd/releases). The
binaries are **not code-signed or notarised**: the checksums are signed instead (see
[Verify a download](#verify-a-download)).

### macOS and Linux

The script detects your OS and architecture, downloads the newest release, checks the
archive against `SHA256SUMS` and installs `shepherd` into `~/.local/bin`. Read it first
if you like ([bin/install.sh](../bin/install.sh)), then run it:

```sh
curl -fsSL -o install.sh https://raw.githubusercontent.com/ubixsys/ubixshepherd/main/bin/install.sh
less install.sh
sh install.sh                          # VERSION=v0.1.0-beta.1 or PREFIX=/usr/local/bin to choose
shepherd version
```

If `shepherd` is not found, `~/.local/bin` is not on your `PATH`: add
`export PATH="$HOME/.local/bin:$PATH"` to your shell profile and open a new terminal.

**By hand:** download `shepherd-<tag>-<os>-<arch>.tar.gz` for your platform, then:

```sh
tar -xzf shepherd-v0.1.0-beta.1-darwin-arm64.tar.gz
install -m 755 shepherd-v0.1.0-beta.1-darwin-arm64/shepherd ~/.local/bin/shepherd
```

**macOS Gatekeeper.** A file downloaded in a browser carries a quarantine flag, and
macOS refuses to open an unsigned binary with it ("cannot be opened because the developer
cannot be verified"). Clear the flag once, after you have verified the download:

```sh
xattr -d com.apple.quarantine ~/.local/bin/shepherd
```

(Or allow it under System Settings, Privacy & Security.) Files fetched with `curl`, as the
script does, are not quarantined.

### Windows

Windows is built but **untested**: expect rough edges (see [Known limits](#known-limits)).
In PowerShell:

```powershell
# Download shepherd-<tag>-windows-amd64.zip (or -arm64) from the release page, then:
Expand-Archive shepherd-v0.1.0-beta.1-windows-amd64.zip -DestinationPath $env:TEMP\shepherd
New-Item -ItemType Directory -Force $env:USERPROFILE\bin | Out-Null
Copy-Item $env:TEMP\shepherd\shepherd-v0.1.0-beta.1-windows-amd64\shepherd.exe $env:USERPROFILE\bin\
# Put that folder on your user PATH (once), then open a new terminal:
[Environment]::SetEnvironmentVariable('Path', "$([Environment]::GetEnvironmentVariable('Path','User'));$env:USERPROFILE\bin", 'User')
shepherd version
```

SmartScreen may warn about an unrecognised app: choose "More info", then "Run anyway",
after verifying the download. Git for Windows provides a new enough git. Shepherd's files
live in `%USERPROFILE%\.shepherd`.

### With Go, or from source

```sh
go install github.com/ubixsys/ubixshepherd/cmd/shepherd@v0.1.0-beta.1
```

A `go install` build has **no web UI** (`shepherd web` shows a "not built" page), because
the UI is built from `web/` by `make`. To build everything, clone the repo and run
`make install` (needs Go, see `go.mod`, git, and Node 22 or later for the UI; without
Node the binary builds and says the UI is missing). `make install` puts the binary in
`~/.local/bin` and restarts a running daemon onto it.

### Verify a download

Each release publishes `SHA256SUMS` and `SHA256SUMS.sigstore.json`. The commands, for
cosign and for each OS, are in
[RELEASING.md](RELEASING.md#verifying-a-download). The install script checks the archive
against `SHA256SUMS`, but does not run cosign; run it yourself if you want proof the sums
came from this repository's release workflow.

## First run

```sh
shepherd init ~/git             # finds the repos below ~/git; you choose which Shepherd manages
shepherd status                 # the daemon, its workspaces, and where you are
shepherd daemon install         # optional: start at login (macOS launchd, Linux systemd)
shepherd chat                   # from ~/git: the one conversation with the front desk
shepherd web                    # or: the browser UI, signed in with a one-use link
```

- Any command starts the daemon in the background if it is not running. On Windows there is
  no login service yet: `shepherd daemon start` starts it, and `shepherd daemon stop` stops it.
- The first daemon start writes `~/.shepherd/config.yaml` with every setting commented
  out, so defaults apply until you edit it. [config.md](config.md) lists the settings. After
  an edit, `shepherd daemon check` validates it and `shepherd daemon reload` applies it
  (on Windows use `shepherd daemon restart`).
- Defaults are cautious: agents commit and **you** push, a human merges, and no agent can
  approve its own work. `autonomy.push` and `autonomy.merge` in a repo's profile opt in.
- The daemon's log is `~/.shepherd/daemon.log`. If a repo is not picked up, `shepherd where`
  says what Shepherd sees from the current directory.

## The front desk

The front desk is a Claude Code session Shepherd runs headless at the workspace root. It
backs both `shepherd chat` and the chat pane of the web UI. It needs `claude` installed and
logged in on the machine the daemon runs on, as the same user. `desk.model` in `config.yaml`
picks which Claude model it uses; there is no setting for another agent as the desk.

Without Claude Code, everything else works: the CLI (`shepherd lane open`,
`shepherd lane run --agent copilot|cursor|opencode ...`, `shepherd decision answer`,
`shepherd request route`), the web UI's board and lane pages, and the MCP tools.

`shepherd mcp` serves the operator tools over MCP on stdio, and any MCP client that can
start a stdio server can use them. A Cursor, Copilot or OpenCode user can register
`shepherd mcp` in their own agent and ask it in plain words, as an informal front desk.
See each tool's MCP documentation for how to register a stdio server.

The README has the full tour: lanes, scope leases, tags, agent runs and decisions.

## Upgrade

Install the newer release over the old one (re-run the script, or copy the new binary
into place), then restart the daemon so it runs the new build:

```sh
shepherd daemon check           # does the new build accept your config.yaml?
shepherd daemon restart
shepherd version
```

While in beta, interfaces may change between releases: read that release's **Interface
changes** in [CHANGELOG.md](../CHANGELOG.md) before upgrading. Shepherd's schema changes
are migrations appended to its store, so your data carries across; there is no downgrade.

## Uninstall

```sh
shepherd daemon uninstall       # remove the login service, if you installed it
shepherd daemon stop
shepherd hook uninstall         # in each repo where lane open installed the pre-push hook
shepherd lane close <name>      # in each open lane, or keep the worktrees and delete them yourself
rm ~/.local/bin/shepherd
rm -r ~/.shepherd               # config, store, run logs: this is everything Shepherd kept
```

Other things you may have registered: `claude mcp remove shepherd` for the MCP server,
and the `shepherd-worker` entry in `~/.cursor/mcp.json` if you ran
`shepherd agents setup cursor`. Lane worktrees sit beside your repos in
`<repo>-worktrees/`; Shepherd never removes work without being asked.

## Known limits

- **GitLab is the only forge for lanes.** Merge request polling and merge closure use
  `glab`. GitHub-as-a-forge, cross-repo typed work orders and triage, and workspace-wide
  leases are not built. See [roadmap.md](roadmap.md).
- **macOS and Linux are tested; Windows is not.** Windows has no login-service
  integration and no config reload.
- **Not a security boundary against a hostile agent.** Scoped tokens and the pre-push hook
  stop accidents; an agent runs as your OS user. Forge branch protection remains the
  enforcement boundary.
- **Interfaces may change** between betas (CLI, config, HTTP API, MCP tools, hook protocol);
  each change is listed in the changelog. See [VERSIONING.md](VERSIONING.md).
- **Agents cost money.** Runs record their cost and `daemon.budget` (default $20 a day)
  holds runs Shepherd would start on its own; it does not cap runs you start.

## Reporting a problem

Open an issue at <https://github.com/ubixsys/ubixshepherd/issues>. Include `shepherd
version`, your OS and architecture, `git --version`, what you ran, and the relevant lines
of `~/.shepherd/daemon.log` or `shepherd run logs <id>`. Output is redacted, but read it
before you paste it: logs can hold your repo and branch names. Never paste
`~/.shepherd/daemon.json`: it holds your operator token. For a security problem, use
GitHub's "Report a vulnerability" on the repository's Security tab rather than a public
issue.
