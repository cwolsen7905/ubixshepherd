# Running on Kubernetes

The manifests are in `deploy/kubernetes/`; [hosted.md](hosted.md) covers the hosted
deployment as a whole. This page covers the image's agent CLIs and the Secret the pod reads.

## Agent CLIs in the image

The image is built for amd64 and arm64 and carries `git`, `glab`, `gh`, `ssh`, `sqlite3`
and two agent CLIs, each pinned to a version and checked against a SHA digest at build time:

| CLI | Source | Pinned by |
|---|---|---|
| `claude` (Claude Code) | the per-platform npm package for linux | `CLAUDE_CODE_VERSION` and SHA-512 digests in the `Dockerfile` |
| `opencode` | the project's GitHub release archive | `OPENCODE_VERSION` and SHA-256 digests in the `Dockerfile` |

To bump one, change its version and both architectures' digests together. Self-update is
turned off, so the version in a running pod is the version in the image. `deploy/docker/check-image.sh`
runs `claude --version` and `opencode --version` on both architectures before a release
digest is signed. Their state (sessions, tokens) lives under `/data/home/cli`, on the
home volume, because the root filesystem is read-only.

## The `shepherd-credentials` Secret

Create it from files that never touch this repo (`kubectl create secret generic
shepherd-credentials --from-file=...`), or with the optional template in
`deploy/kubernetes/external-secrets/`. The pod's `place-credentials` init step copies each
key to an in-memory volume at mode 0600, owned by the daemon's user. The Secret itself is
not mounted into the daemon container.

| Key | Required | Holds |
|---|---|---|
| `totp-key` | yes | at least 16 random bytes; encrypts the TOTP secret at rest |
| `ssh-id` | for git over ssh | the private half of the GitLab push key, for the daemon's user |
| `ssh-known-hosts` | with `ssh-id` | host keys of the GitLab host (and any other forge), in `known_hosts` format |
| `glab-config` | for GitLab reads | glab's `config.yml` (holds the GitLab token) |
| `gh-hosts` | for GitHub | gh's `hosts.yml` |
| `claude-credentials` | for Claude Code runs | the CLI's login state, `.credentials.json` |

The image's ssh configuration reads the key from `/run/secrets/shepherd/ssh/id` and the host
keys from `/run/secrets/shepherd/ssh/known_hosts`, with `IdentitiesOnly` and strict host
key checking, so a git operation over ssh fails rather than trusting an unknown host. Get
the host keys with `ssh-keyscan <host>` and check them against a source you trust before
storing them.

The `glab`, `gh` and `claude` entries are seeds: the entrypoint copies each into
`/data/home/cli` once and never replaces it, so a rotated token has no effect until its file
there is deleted and the pod restarted. `totp-key` and the ssh files are read from the
Secret on every start.
