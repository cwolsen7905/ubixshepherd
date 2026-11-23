# Cluster: a laptop linked to a hub

One Shepherd daemon can be a **hub**, and daemons on other machines (a work laptop, a home
laptop) can be its **members**. A member dials out to the hub over HTTPS and holds one
connection open. The hub never dials a member, so a laptop behind NAT, a VPN or a work
network that allows only outbound HTTPS works.

This is the first step, and it is status only: the hub lists its members and whether each is
connected. No repos, events or actions cross the link yet.

## What you need

- A hub: a daemon in hosted mode (`daemon.public`, see [hosted.md](hosted.md)) with
  `cluster.role: hub`. Its public address must pass a long-lived WebSocket: an ingress or
  load balancer timeout of an hour or more, or WebSocket-aware defaults.
- A member: any daemon. It needs outbound HTTPS to the hub's public address and nothing else.

## Set it up

On the hub, set the role in `config.yaml` and reload (`shepherd daemon reload`):

```yaml
daemon:
  public: { ... }     # hosted mode, as in hosted.md
cluster:
  role: hub
```

Name the machine, on the hub (or with `--host`, or with `kubectl exec` in a pod):

```
$ shepherd member add work
sunny-acorn-...-eight-words

This code enrols one machine as "work", once, until 14:05.
```

The code is eight words from the EFF short wordlist, lowercase, joined by `-`: about 81 bits.
It works once and ends after five minutes. Settings in the web UI does the same under
Members.

On the member:

```
$ shepherd cluster join https://shepherd.example.com sunny-acorn-...-eight-words
joined https://shepherd.example.com as work; the daemon is linking now
```

The daemon trades the code for a **link credential** and keeps it in its Shepherd home
(`cluster-link.json`, readable by its owner only), never in the config. It starts linking
at once. `cluster.hub` in the config is optional: set it to the hub's address to say which
hub this daemon belongs to. If it names a different hub than the credential is for, the
daemon does not link and says why.

## See it

```
$ shepherd cluster status
hub 6f1c2e0a-...
  work         online since 09:12 (2h ago)  v0.9.0  api 1  1 running, 2 waiting  id 3a9d...
  home         offline, last seen 2d ago  id 71be...
```

`--json` gives the same as `GET /v1/cluster`. On a member the command shows the link: its
state (`connected`, `connecting`, `waiting to retry`, `refused`), since when, why the last
try failed and when the next one is. The desk answers "is my work laptop connected" with the
`cluster_status` MCP tool. In the web UI, Settings lists the members on a hub.

## Commands

| Command | Where | Does |
|---|---|---|
| `shepherd member add <name>` | hub | Names a member and shows its enrolment code once |
| `shepherd member list` | hub | Lists members, as `cluster status` does |
| `shepherd member revoke <name>` | hub | Deletes its credential and closes its link; `member add` enrols it again |
| `shepherd member remove <name>` | hub | Revoke, and forget the name |
| `shepherd member confirm <name>` | hub | Accepts a changed daemon id (below) |
| `shepherd cluster join <url> <code>` | member | Enrols this daemon and starts linking |
| `shepherd cluster status [--json]` | either | Members, or this member's link |

Adding, revoking, removing and confirming are the person's: the operator token, or a browser
session signed in with `--allow-settings`. The front desk can read the cluster and nothing
more.

## How the link works

- A member opens `GET /v1/cluster/link` with its credential and the header
  `X-Shepherd-Daemon-Id`, and upgrades it to a WebSocket (`github.com/coder/websocket`).
- Each side sends a hello (daemon id, version, `api_level`, capabilities), then the member
  sends a status (runs going, items waiting) at once and at every ping. The hub records
  what the member reports.
- Each side pings every 20 seconds; two pings in a row with no pong close the link.
- A dropped link is redialled after a random wait between zero and a step that doubles from
  one second to a cap of 60. A `Retry-After` from the hub is a floor. A link that reached the
  hub's hello starts the step again.
- A member has one link. A second from the same member replaces the first.

## Trust

- A link credential has the `link` role. It opens the link and nothing else: every other
  route answers it 403, the acting routes included. Its prefix (`shl_`) means a device token
  is never taken for one and a link credential is never taken for a device token.
- The hub keeps credentials as hashes. A revoked credential drops the live link and is
  refused at the next dial.
- Each daemon has an id (a random UUID in `daemon.id` in its Shepherd home, shown in
  `GET /v1/status`). The hub records the id a member enrolled with. A link from the same
  credential with a different id (a reinstall, or another machine) is refused with 403 and
  shown as `refused` with the new id, until `shepherd member confirm <name>`.
- Enrolment is the one call with no token: the code is the proof. It works only on a hub.

## Choices this version made

| Choice | Why |
|---|---|
| The names are hub and member, in config, commands and tables | What this build was asked for; a later change may rename them |
| A frame is one WebSocket text message holding one JSON object | The WebSocket already frames messages, so a length prefix adds nothing |
| The wordlist is the EFF short list without `yo-yo`: 1295 words | A word with a `-` would not split back out of a code |
| Enrolment codes and credentials are in the store; the daemon id and the member's credential are files in the Shepherd home | A restarted hub keeps its members; a member keeps its credential out of the config |
| `cluster.hub` is optional once a credential exists | The credential file already names the hub it is for |
| A member whose id changed gets `Retry-After: 60` | It is waiting on a person, so it need not retry fast |
| Status goes up at each ping rather than on change | Simplest; the hub's view is at most 20 seconds old |

## Not here yet

Repo homes, reads through the link, the journal and the thread, offline replay, acting on a
member's repos and spend across machines are later steps. A member's own desk and repos are
untouched and keep working while it is unlinked.
