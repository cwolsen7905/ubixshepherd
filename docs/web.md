# The web UI: what the person may do to a forge from the page

Status: **implemented** for approving a lane's merge request; the rest of the web UI is
described in [web-chat.md](web-chat.md), [web-projects.md](web-projects.md),
[web-settings.md](web-settings.md) and [web-daemon-status.md](web-daemon-status.md).

## Approving a lane's merge request

The person can approve a lane's merge request from the Lane page's **Changes** tab, while
looking at the lane's diff and gate badge, instead of opening the forge.

The rule does not change: **agents never approve**. Approval is the person's own click in
a browser session, and nothing else can make it.

| Piece | What it does |
|---|---|
| `GET /v1/lanes/{id}/approval` | The merge request read live from the forge: state, head commit, who approved, the head pipeline's status and its `require-approval` job. A forge that cannot approve says so (`supported: false`) with the request's link |
| `POST /v1/lanes/{id}/approve` | Body `{sha}`, the head the person was looking at. Browser sessions only |
| Who may call it | A browser session. The front desk's token, an agent's token and the operator token (the CLI's) are refused with 403. There is no CLI command and no MCP tool for it, so the desk cannot be handed it |
| Stale head | The daemon refuses (409) when the request's head is not `sha`, and passes `sha` to the forge, which refuses too, so the approval is for the diff that was shown |
| Record | A `mr` feed line, "approved by the person from the web at `<sha>`", with the lane; and an action event in the desk's thread, so the desk is told without starting a turn |
| Pipeline | The forge does not judge a pipeline again on an approval, and `require-approval` holds the merge until it passes. After a successful approval the daemon retries that job when it has failed or was canceled; if it cannot, the approval stands and the page says to retry it on the forge |

The approver is whoever the forge CLI on the daemon's machine is signed in as. What counts
as a sign-off is still decided by the repo's `require-approval` job
(`MERGE_APPROVAL_OWNERS`, and the author rule), not by this page.

### Forges

GitLab is supported. GitHub is not read as a lane's forge yet; a lane on a forge Shepherd
cannot approve through shows "Approval is not available for this forge" and the merge
request's link when there is one.

### Not yet

The feed line reuses the `mr` kind. A dedicated kind (so the thread and history could mark
a human approval distinctly) needs `internal/store` and `internal/api/events.go`, and is
tracked in Next up.
