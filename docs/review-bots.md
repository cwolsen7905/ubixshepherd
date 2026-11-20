# Review bots

The **Review bots** page (`shepherd web`, then Review bots in the sidebar) shows what a
review bot did on your merge requests: per repo, the jobs it ran, what it proposed, what a
person dismissed, what waits on you, the fixes it pushed and what it cost. It is read only.

Shepherd reads the forge through its own CLI (`glab`) and your login in it, keeps nothing
and stores nothing: the answer is cached in the daemon for five minutes, and the page's
Refresh button reads the forge again. Only GitLab repos can be read today.

## Configure a bot

A bot is whatever account posts review threads and sets status labels on your merge
requests. Name it under `review_bots:`:

```yaml
review_bots:
  - name: Reviewer
    user: review-bot        # the bot's forge username
    label_prefix: "bot::"   # its status labels start with this
    projects: [app, lib]    # repo names, as the workspace lists them
    window_days: 30         # optional, 1 to 365
```

| Field | Default | Meaning |
|---|---|---|
| `name` | required | The tab's title. Unique. |
| `user` | required | The bot's forge username. Its threads and notes are the ones counted. |
| `label_prefix` | required | The start of the bot's status labels. |
| `projects` | required | Repos to read. Each needs a remote Shepherd can read. |
| `window_days` | `30` | How far back, by last update, merge requests count. |

The status labels are the prefix followed by `working`, `pushed`, `proposed` and
`needs-you`: with `label_prefix: "bot::"` they are `bot::working`, `bot::pushed`,
`bot::proposed` and `bot::needs-you`. A bot keeps one of the last three on a merge
request at a time, replacing it each round.

## What the columns mean

| Column | Counts |
|---|---|
| Jobs | Merge requests the bot worked on in the window: one with a label of its prefix, or a note by its user. |
| Proposals | Merge requests whose latest status is `proposed`. |
| Dismissals | Threads the bot opened that someone other than the bot resolved. |
| Needs you | Merge requests whose latest status is `needs-you`. Open ones are listed under the table, with links. |
| Fixes pushed | Merge requests whose latest status is `pushed`. |
| Spend | The cost on the bot's notes: an italic line that begins ``_Model `name` `` and has a dollar amount, as a review job's usage stamp does. "not stamped" when no note has one. |

A merge request counts once, under its latest label, so a request that was `needs-you` and
is now `pushed` counts as a fix. A bot that edits one summary note per request leaves one
stamp, so spend is the latest round's cost, not the sum of every round.

Shepherd reads the newest 60 merge requests in the window for each repo and says so when a
repo had more. A merge request with more than 1000 discussions is read in part, and its repo's row says its counts are partial. A repo it cannot read shows the reason in its row and zero counts, and the
other repos still show. A report with a failed read is not cached, so the next load tries again.

## The API

`GET /v1/review-bots` answers `{as_of, bots: [{name, user, label_prefix, window_days,
projects: [counts...], total}]}`; `?refresh=1` skips the cache. Counts are `jobs`,
`working`, `proposals`, `dismissals`, `needs_you`, `fixes`, `spend_usd` and `stamped`.
