# Cluster: workers linked to a controller

Clustering is opt-in. A daemon with no `cluster:` section in its config, that has never run
`shepherd cluster join`, never dials anywhere: it is a complete, independent Shepherd. That
is the normal way to use Shepherd as a tool, and nothing on this page is needed for it.

When you do want it, one Shepherd daemon can be the **controller**, and daemons on other
machines (a work laptop, a home laptop) can be its **workers**. A worker dials out to the
controller over HTTPS and holds one connection open. The controller never dials a worker, so
a laptop behind NAT, a VPN or a work network that allows only outbound HTTPS works.

This is the first step: the controller lists its workers, whether each is connected, which
tenant each is in, and the repos and agent CLIs each one reports. No events or actions
cross the link yet, and nothing is routed to a worker.

## What you need

- A controller: a daemon in hosted mode (`daemon.public`, see [hosted.md](hosted.md)) with
  `cluster.role: controller`. Its public address must pass a long-lived WebSocket: an
  ingress or load balancer timeout of an hour or more, or WebSocket-aware defaults.
- A worker: any daemon. It needs outbound HTTPS to the controller's public address and
  nothing else.

## Set it up

On the controller, set the role in `config.yaml` and reload (`shepherd daemon reload`):

```yaml
daemon:
  public: { ... }     # hosted mode, as in hosted.md
cluster:
  role: controller
```

Name the machine, on the controller (or with `--host`, or with `kubectl exec` in a pod):

```
$ shepherd worker add work
sunny-acorn-...-eight-words

This code enrols one machine as "work", once, until 14:05.
```

The code is eight words from the EFF short wordlist, lowercase, joined by `-`: about 81 bits.
It works once and ends after five minutes. Settings in the web UI does the same under
Workers.

On the worker:

```
$ shepherd cluster join https://shepherd.example.com sunny-acorn-...-eight-words
joined https://shepherd.example.com as work; the daemon is linking now
```

The daemon trades the code for a **link credential** and keeps it in its Shepherd home
(`cluster-link.json`, readable by its owner only), never in the config. It starts linking
at once. `cluster.controller` in the config is optional: set it to the controller's address
to say which controller this daemon belongs to. If it names a different controller than the
credential is for, the daemon does not link and says why. Deleting `cluster-link.json` makes
the daemon independent again.

## See it

```
$ shepherd cluster status
controller 6f1c2e0a-...
  work         online since 09:12 (2h ago)  v0.9.0  api 1  1 running, 2 waiting  tenant default  id 3a9d...
               agents  claude, copilot
               repos   github.com/example/lib as lib
                       gitlab.example.com/group/app as app
  home         offline, last seen 2d ago  tenant default  id 71be...
```

`--json` gives the same as `GET /v1/cluster`: `role` is `controller`, `worker` or `alone`,
and a controller lists `workers`. Each worker has its `tenant` (the slug), and, while it is
online, `repos` (each an `origin` and the worker's `name` for it) and `agents`. On a worker
the command shows the link: its state (`connected`, `connecting`, `waiting to retry`,
`refused`), its tenant, since when, why the last try failed and when the next one is. The
desk answers "is my work laptop connected" and "which machine has this repo" with the
`cluster_status` MCP tool. In the web UI, Settings lists the workers on a controller, with
each online worker's agents and repos.

## What a worker reports

A worker reports what it has, so the controller knows which machine can work on what:

- **Repos**: each managed repo with an origin remote, by origin, with the worker's own name
  for it. The origin is normalised to host and path, lowercase, without `.git`, a user or a
  port, so `git@gitlab.example.com:group/app.git` and `https://gitlab.example.com/group/app`
  are one repo. A repo with no remote is left out, since it cannot be matched to another
  machine's.
- **Agents**: the agent CLIs the worker can start (`claude`, `copilot`, `cursor`,
  `opencode`), found the way a run finds them, on the daemon's PATH.

The worker sends both in a `repos` frame once it has accepted the controller's hello, then
checks them again at each status (every 20 seconds) and sends another when either changed,
so a repo added on a worker shows at the controller within one status interval. If the
worker cannot read its own repos, it sends nothing and the last report stands. A report
carries at most 500 repos and 48 KiB of them, so a frame always fits the link's 64 KiB read
limit; any more are counted, and the status says how many were left out. The controller keeps the report in memory with
the link: it is live, gone when the link drops, and never stored. A worker from before the
report sends none; the controller still links it and lists it as not reporting.

## Tenants

Each worker's credential belongs to a tenant, and the controller's hello names it. Until
the store records a worker's tenant, every worker is reported in the default tenant. The
worker records the first tenant it is linked in, in `cluster-link.json`, and shows it in its
link. The controller lists a worker only once it has accepted the hello, at its first frame
after it. If a later hello names another tenant, the worker refuses the link (state `refused`,
with both tenants in the reason) and retries slowly: a credential does not move between
tenants. Enrol it again (`shepherd worker add` on the controller, `shepherd cluster join`
on the worker) to link it in the new one. A controller that names no tenant is not a
change.

## Commands

| Command | Where | Does |
|---|---|---|
| `shepherd worker add <name>` | controller | Names a worker and shows its enrolment code once |
| `shepherd worker list [--json]` | controller | Lists workers, as `cluster status` does |
| `shepherd worker revoke <name>` | controller | Deletes its credential and closes its link; `worker add` enrols it again |
| `shepherd worker remove <name>` | controller | Revoke, and forget the name |
| `shepherd worker confirm <name>` | controller | Accepts a changed daemon id (below) |
| `shepherd cluster join <url> <code>` | worker | Enrols this daemon and starts linking |
| `shepherd cluster status [--json]` | either | Workers, or this worker's link |

Adding, revoking, removing and confirming are the person's: the operator token, or a browser
session signed in with `--allow-settings`. The front desk can read the cluster and nothing
more. Over HTTP they are `POST /v1/cluster/workers` with `{"name": "..."}`, and
`POST /v1/cluster/workers/{name}/revoke`, `/remove` and `/confirm`.

## Old names

This feature first shipped calling the controller a hub and the workers members. The old
names still work for one release, and each says what replaces it:

| Old | New | What happens |
|---|---|---|
| `cluster.role: hub` | `cluster.role: controller` | Read as `controller`, with a config warning |
| `cluster.hub: <url>` | `cluster.controller: <url>` | Read as `cluster.controller`, with a config warning. Both set to different addresses is an error |
| `shepherd member ...` | `shepherd worker ...` | Runs the same thing, after a note on stderr |
| `/v1/cluster/members...` | `/v1/cluster/workers...` | Both prefixes are served |

Config warnings are logged when the daemon starts and printed by `shepherd daemon reload`,
`daemon restart` and `daemon check` (see [config.md](config.md)). The link itself is
unchanged, so a controller and a worker on either side of the rename keep linking: the
enrolment answer carries the controller's id under both `controller_daemon_id` and the old
`hub_daemon_id`, and a worker reads either.

## How the link works

- A worker opens `GET /v1/cluster/link` with its credential and the header
  `X-Shepherd-Daemon-Id`, and upgrades it to a WebSocket (`github.com/coder/websocket`).
- Each side sends a hello (daemon id, version, `api_level`, capabilities). The controller's
  names the worker's `tenant`; the worker's names the capability `repos`. Once the worker
  accepts the controller's hello it sends a `repos` frame (its whole report, replacing the
  last) and a status (runs going, items waiting), then a status at every ping and a `repos`
  frame when its repos or agents change. The controller lists the worker from its first
  frame after the hellos. The controller records what the worker reports. No frame names a
  role.
- Each side ignores a frame type or field it does not know, so the report and the tenant
  were added without moving `api_level`: an older controller drops them, and an older
  worker sends neither.
- Each side pings every 20 seconds; two pings in a row with no pong close the link.
- A dropped link is redialled after a random wait between zero and a step that doubles from
  one second to a cap of 60. A `Retry-After` from the controller is a floor. A link that
  reached the controller's hello starts the step again.
- A worker has one link. A second from the same worker replaces the first.

## Trust

- A link credential has the `link` role. It opens the link and nothing else: every other
  route answers it 403, the acting routes included. Its prefix (`shl_`) means a device token
  is never taken for one and a link credential is never taken for a device token.
- The controller keeps credentials as hashes. A revoked credential drops the live link and
  is refused at the next dial.
- Each daemon has an id (a random UUID in `daemon.id` in its Shepherd home, shown in
  `GET /v1/status`). The controller records the id a worker enrolled with. A link from the
  same credential with a different id (a reinstall, or another machine) is refused with 403
  and shown as `refused` with the new id, until `shepherd worker confirm <name>`.
- Enrolment is the one call with no token: the code is the proof. It works only on a
  controller.

## Choices this version made

| Choice | Why |
|---|---|
| The names are controller and worker; hub and member are read for one release | The project's vocabulary; a daemon already upgraded must still understand one that is not |
| A frame is one WebSocket text message holding one JSON object | The WebSocket already frames messages, so a length prefix adds nothing |
| The wordlist is the EFF short list without `yo-yo`: 1295 words | A word with a `-` would not split back out of a code |
| Enrolment codes and credentials are in the store; the daemon id and the worker's credential are files in the Shepherd home | A restarted controller keeps its workers; a worker keeps its credential out of the config |
| `cluster.controller` is optional once a credential exists | The credential file already names the controller it is for |
| A worker whose id changed gets `Retry-After: 60` | It is waiting on a person, so it need not retry fast |
| Status goes up at each ping rather than on change | Simplest; the controller's view is at most 20 seconds old |
| Repos and agents are checked at each status and sent on change | A change shows within one interval, and an unchanged report costs nothing on the wire |
| A worker's report is kept in memory with its link, not in the store | It describes the machine now; an offline worker's old report would mislead |
| A worker refuses a changed tenant rather than following it | Which tenant a machine works for is the person's choice, made by enrolling it |

## Not here yet

Routing work to a worker by its repos, repo homes, reads through the link, the journal and the thread, offline replay, acting on a
worker's repos and spend across machines are later steps. A worker's own desk and repos are
untouched and keep working while it is unlinked.
