# Merging after a person approves

A repo can let Shepherd merge a lane's merge request once a person has approved it. Set it
per repo, or under `defaults:`:

```yaml
repos:
  app:
    autonomy:
      merge: after_approval
      merge_bots: [review-helper]   # optional: more accounts whose approval never counts
```

| `autonomy.merge` | Who merges |
|---|---|
| `human` (default) | The person, on the forge. |
| `after_approval` | Shepherd's watcher, once the conditions below hold. Agents never arm a merge. |
| `agent` | An agent may arm merge-when-pipeline-succeeds at any time; the forge's rules still gate it. |

## What has to be true

The watcher checks these on every poll of an open lane's request, and merges only when all
hold on the request's current head:

- At least one approver is a person who approved after the current head was pushed: not the request's author, not an account the forge
  marks as a bot (including project and group access-token users), and not listed in
  `merge_bots`. Shepherd and agents never approve.
- The request is not a draft and the forge reports it mergeable (conflicts and a stale
  branch wait).
- The head pipeline passed, and it ran on the head.
- No review thread is open.

Anything unmet is waited on, with the reason in the daemon's debug log. Shepherd never
overrides the forge: its required approvals, protected branch and pipeline rules apply as
usual.

## New commits after approval

GitLab's approvals are not per commit, and a project may keep them across pushes. So
Shepherd reads when each person approved (the request's approval notes) and when the
current head was pushed, and counts only an approval given at or after the push. An
unapproval after an approval cancels it. After a new push the request therefore waits for a
person to approve again, whatever the project does with old approvals. The merge call also
names the head that was checked, so the forge refuses it if the head has moved. Shepherd
keeps no approval state of its own.

## What you see

The feed says when a request was merged this way and by whom it was approved. If the forge
refuses the merge, the feed says why and Shepherd does not retry that head: push a new
commit or merge it by hand. The next poll sees the merge and closes the lane as for any
merge.

## Limits

GitLab only: the merge uses `glab api`. How the commit lands (merge commit or squash) is the
project's setting; Shepherd has no squash option of its own.
