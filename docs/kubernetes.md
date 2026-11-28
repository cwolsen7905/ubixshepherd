# Running on Kubernetes

Shepherd ships a Helm chart in `deploy/charts/shepherd/`; [hosted.md](hosted.md) covers the
hosted deployment as a whole. This page covers installing the chart, its values, worker
pods, the images, the agent CLIs and the Secret the pod reads.

## Installing

Keep your values (host name, issuer, storage class, pull secret) in your own repository,
not in this one. Create the namespace and the credentials Secret first, then install:

```sh
kubectl create namespace shepherd
kubectl label namespace shepherd pod-security.kubernetes.io/enforce=restricted
kubectl -n shepherd create secret generic shepherd-credentials --from-file=totp-key=... # keys below
helm upgrade --install shepherd deploy/charts/shepherd -n shepherd -f my-values.yaml
kubectl -n shepherd exec -it shepherd-0 -- shepherd account set
```

The pod starts on loopback only; the public listener, and so readiness, comes up once
`shepherd account set` has made the owner account. Upgrading is `helm upgrade` with a new
`image.tag`; the data volume is kept across upgrades and uninstalls (delete the claim
`data-shepherd-0` yourself to drop it).

| Value | Required | What it sets |
|---|---|---|
| `image.tag` | yes | A release tag, or a development image's short-sha tag. Never a moving tag |
| `image.repository`, `imagePullSecrets` | | Another registry, and the pull Secrets it needs |
| `seed.publicUrl` | yes | The public `https://` address. Seeds `config.yaml` on an empty volume only |
| `seed.trustedProxies` | yes | The ingress controller's pod addresses. Seeds `config.yaml` on an empty volume only |
| `ingress.host`, `ingress.tls.secretName` | yes, with the Ingress | The host name and the Secret holding its certificate |
| `ingress.clusterIssuer` | | A cert-manager issuer to make that certificate |
| `persistence.storageClass`, `persistence.size` | | The data volume. Use block or local storage: SQLite needs working file locks, which NFS does not reliably give |
| `networkPolicy.ingressControllerNamespace` | | The ingress controller's namespace, the only source allowed in (default `ingress-nginx`) |
| `networkPolicy.extraEgress` | | Egress rules for destinations on private networks, such as a self-hosted forge or model server; the default allows only public addresses |
| `credentials.secretName`, `credentials.externalSecret.*` | | The Secret's name, and an optional ExternalSecret that makes it |
| `backup.*` | | The nightly store backup, and an optional VolumeSnapshot after it |
| `mode`, `seed.controllerUrl` | | `worker` for a worker pod, and the controller it links to: see [Worker pods](#worker-pods) |

The `seed` values are read only when the home has no `config.yaml`. After the first start,
change `daemon.public` in Settings or in the home's `config.yaml`.

### Moving from the old manifests

Releases before the chart shipped kustomize manifests in `deploy/kubernetes/`. Helm cannot
take those objects over, and the StatefulSet's selector (which cannot change) now also
carries the release name. Keep the data, replace the rest:

```sh
kubectl -n shepherd delete statefulset shepherd --cascade=orphan   # the pod and the claim stay
kubectl -n shepherd delete service,ingress,networkpolicy shepherd
kubectl -n shepherd delete cronjob,serviceaccount,role,rolebinding shepherd-backup --ignore-not-found
kubectl -n shepherd delete configmap shepherd-snapshot --ignore-not-found
kubectl -n shepherd delete pod shepherd-0
helm upgrade --install shepherd deploy/charts/shepherd -n shepherd -f my-values.yaml
```

Name the release `shepherd`, as above, so the StatefulSet is `shepherd` again and the
claim `data-shepherd-0` is reused with the home and workspace on it. The seeded
`config.yaml` on the volume is kept, so the `seed` values do not change it. The old
manifests took a VolumeSnapshot after each store backup; the chart does that only with
`backup.snapshot.enabled: true` (and `backup.snapshot.className`), so set it to keep them.

## Worker pods

A worker pod is a Shepherd daemon that runs in the cluster for its toolchain and its
uptime: it works on the repos homed on it while laptops sleep, and it links out to your
controller like a laptop does ([cluster.md](cluster.md)). It opens no public listener and
takes no connections in. Clustering stays opt-in: a controller with no workers, or a
laptop that never joins one, is a complete Shepherd.

Install it from the same chart with `mode: worker`, in the same namespace as its
controller (it uses the same `shepherd-credentials` Secret: the same push key and logins),
under its own release and name:

```yaml
# worker-go.values.yaml
mode: worker
fullnameOverride: shepherd-worker-go
image:
  repository: ghcr.io/ubixsys/ubixshepherd
  tag: <release>-go             # the worker image: Go, Node and make on the base
seed:
  controllerUrl: https://shepherd.example.com
resources:
  requests: {cpu: "1", memory: 2Gi}
  limits: {memory: 8Gi}
networkPolicy:
  extraEgress: []               # add the controller's address if it is on a private network
```

```sh
helm upgrade --install shepherd-worker-go deploy/charts/shepherd -n shepherd -f worker-go.values.yaml
# on the controller: name the worker and get its one-time code
kubectl -n shepherd exec shepherd-controller-0 -c shepherd -- shepherd worker add worker-go
# in the worker: trade the code for its link credential (kept on its volume)
kubectl -n shepherd exec shepherd-worker-go-0 -c shepherd -- shepherd cluster join https://shepherd.example.com <code>
```

`shepherd cluster status` on the controller then lists the worker as online. Log the
agent CLI in once inside the worker as for the controller, and clone the repos it should
work on into its workspace (`/data/workspace`), then `shepherd init`.

| Mode | Seeded from | Listens | Readiness |
|---|---|---|---|
| `controller` (default) | `seed.publicUrl`, `seed.trustedProxies` | the public listener, behind the Ingress | `GET /v1/health`, once an owner account exists |
| `worker` | `seed.controllerUrl` | loopback only | `shepherd status` |

### Images

The image comes in two targets. The base (the default) is what a controller runs: the
daemon, git, glab, gh, ssh, sqlite3 and the agent CLIs. `worker-go` (tag suffix `-go`)
adds Go, Node, npm and make, with their caches on the home volume and cgo off. For another
toolchain, build your own image `FROM` the base: switch to root, install it, and switch
back to user 10001.

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
shepherd-credentials --from-file=...`), or set `credentials.externalSecret.enabled` to have
the chart make an ExternalSecret for the External Secrets Operator, reading each key from
your store under `credentials.externalSecret.remoteKeyPrefix`. The pod's `place-credentials` init step copies each
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
