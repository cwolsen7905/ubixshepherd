# Workspace cleanup audit

Read-only snapshot of every open lane under `/Users/cwolsen/git/*-worktrees/`, taken
2026-10-15 (evening, local time). Nothing was pushed, merged or commented on. State moves
quickly (the polish lanes were pushing while this was written): re-check before acting.

Repos: ubixshepherd, ubixkelpie, ubixos-web, brainchurts-web (internal GitLab), UbixOS (GitHub).

## 1. Lanes

| Repo | Branch | MR/PR | Pipeline | Unpushed | Uncommitted | Verdict |
|---|---|---|---|---|---|---|
| ubixshepherd | fix/web-polish-settings | [!260](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/260) open, mergeable | #7868 success | 0 | node_modules/ untracked | NEEDS-MERGE |
| ubixshepherd | fix/web-polish-projects | [!264](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/264) open, mergeable | #7877 success | 0 | none | NEEDS-MERGE |
| ubixshepherd | fix/web-polish-board | [!259](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/259) open, threads unresolved | #7866 success | 1 (local f9395a8 vs remote df70f27) | none | NEEDS-PUSH (then resolve threads) |
| ubixshepherd | fix/web-polish-next-up | [!261](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/261) open, threads unresolved | #7871 success | 1 (b474bbd vs cd796d9) | none | NEEDS-PUSH (then resolve threads) |
| ubixshepherd | fix/web-polish-artifacts | [!263](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/263) open, threads unresolved | #7874 success | 1 (f53e75c vs 70a2c29) | none | NEEDS-PUSH (then resolve threads) |
| ubixshepherd | fix/web-polish-runs | [!262](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/262) open, threads unresolved | #7878 success | 0 | none | NEEDS-MERGE (threads block it) |
| ubixshepherd | feat/web-session-extend | none | none | 2 commits, branch not on remote | none | ACTIVE (lane 391, minutes old) |
| ubixshepherd | docs/web-ui-overhaul-plan | none open ([!232](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/232) and [!246](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/246) merged) | none | 0 vs origin/dev | .DS_Store files only | CLOSE (lane 282; no work left to land) |
| ubixshepherd | docs/next-up-walkthrough | none | none | 0 (branch is at dev, no commits, not on remote) | .DS_Store | ABANDONED (lane 232, 2 days idle) |
| ubixshepherd | docs/cleanup-audit | none yet | none | this report, unpushed by instruction | none | ACTIVE (this lane) |
| ubixkelpie | docs/architecture-audit | none, branch not on remote | none | 2 commits (408-line report) | none | NEEDS-PUSH (blocked by the server, see 1.2) |
| ubixkelpie | docs/database-setup | none | none | 0 (branch is at dev, nothing on remote) | none | ABANDONED |
| ubixos-web | fix/ci-promote-failure | none, branch not on remote | none on branch; dev #7471 failed | 0 (branch is at dev 4924016) | none | BROKEN (dev, see 1.3); the branch itself has no work |
| brainchurts-web | ci/mr-pipelines | none (the MR of that name is kelpie's [!11](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/11), merged) | main green (#7875) | 0 (branch is in dev) | none | ABANDONED (empty branch) |
| UbixOS | packages/p1-libpkg | [#52](https://github.com/cwolsen7905/UbixOS/pull/52) open | 6/6 CI checks success on 7342b8d | 0 | none | NEEDS-MERGE |
| UbixOS | docs/agent-on-ubixos-2 | none | none | 0 vs master (behind it), not on remote | none | ABANDONED (empty); [#36](https://github.com/cwolsen7905/UbixOS/pull/36) for docs/agent-on-ubixos is merged |

### 1.1 ubixshepherd !257 (fix/web-inspector-frame)

Merged. Pipeline [#7879](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/pipelines/7879)
on f4f07e6 is success now. The failure was job
[go-check #28447](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/jobs/28447) in that same
pipeline, retried to green. It was a test failure, not the web change:

```
--- FAIL: TestUpgradeFailureKeepsTheBinaryAndIsNotRetried (0.21s)
    upgrade_test.go:179: {State:failed Method:binary CanUpgrade:true ... Target:v0.5.0 ... Error:shepherd-v0.5.0-linux-amd64.tar.gz does not match SHA256SUMS: not installing it ...}
FAIL	github.com/ubixsys/ubixshepherd/internal/daemon	16.321s
make: *** [Makefile:60: check] Error 1
```

Look for a flake in `internal/daemon/upgrade_test.go:179`: the daemon package is the only
failure and the three earlier pipelines for this MR were green. Separately, the push
pipeline on `main`, [#7881](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/pipelines/7881)
(sha 07efd5e, created and finished in the same second), is `failed` with no jobs and no
yaml error or failure reason. `dev` #7880 on the same sha is green.

### 1.2 ubixkelpie docs/architecture-audit push

Branch is not on the remote and the remote has no push rule (404); only `dev` and `main`
are protected, and protection does not apply to a new branch. The run log (run 996) shows
three attempts all rejected with `remote unpack failed: unable to create temporary object
directory`. That message comes from the git server itself (its object directory is not
writable or full), not from branch protection, a hook or Shepherd's pre-push scope check.
Fix is on the GitLab host (disk space or permissions on the repo storage), then push the
branch as is. The same host accepted pushes to ubixshepherd and brainchurts-web in the same
period, so check that one repo's storage first.

### 1.3 ubixos-web dev pipelines

Not green. The latest `dev` pipeline is [#7471](https://gitlab.brainchurts.com/ubixos/ubixos-web/-/pipelines/7471)
(about a day old, merge of fix/ci-autopromote, 4924016): job
[build-dev #26453](https://gitlab.brainchurts.com/ubixos/ubixos-web/-/jobs/26453) failed,
`deploy-dev` and `promote-to-staging` skipped. Cause is a registry error on push, not the code:

```
ERROR: failed to push gitlab.brainchurts.com:5050/ubixos/ubixos-web:dev: unknown: blob unknown to registry - sha256:35f2c3a3...
ERROR: Job failed: exit status 1
```

The two pipelines before it (#7470, #7468) were green, and nothing has run since, so retry
the job (or re-run the pipeline); the `fix/ci-promote-failure` branch carries no change.
`staging` and `main` last ran green 5 days ago, so the auto-promote chain has not completed
since the fix merged.

## 2. Kelpie MR thread autofix (design steps 1 to 9)

Steps are the table in `docs/design/mr-thread-autofix.md` section 7. All merged to `dev`
2026-10-15. Plan MRs [!17](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/17)
and [!18](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/18)
(docs/mr-thread-autofix-plan) are merged.

| Step | What | Branch | MR | State |
|---|---|---|---|---|
| 1 | Thread reader | feat/autofix-thread-reader | [!19](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/19) | merged |
| 2 | Event filter and enqueue | feat/autofix-event-filter | [!20](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/20) | merged |
| 3 | Database and job state | feat/autofix-job-state | [!22](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/22) | merged |
| 4 | Triage service | feat/autofix-triage | [!21](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/21) | merged |
| 5 | Reply and label | feat/autofix-reply-label | [!23](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/23) | merged |
| 6 | Fixer runner | feat/autofix-fixer-runner | [!24](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/24) | merged |
| 7 | Resolution comment and resolve | feat/autofix-resolution | [!25](https://gitlab.brainchurts.com/ubixsys/ubixkelpie/-/merge_requests/25) | merged |
| 8 | Sweep and pipeline-failure source | none | none | never opened |
| 9 | Rollout | none | none | never opened |

Notes: no branch of these is on the remote any more (removed on merge). The MR pipelines of
!24 and !25 show `failed` only at `require-approval-mr` ("no sign-off yet", expected before
approval); build, static-checks, ai-review-tests and phpunit were green, and the merge
pipelines on `dev` and `staging` are green.

## 3. ubixshepherd MRs

| MR | Branch | State |
|---|---|---|
| [!244](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/244) | fix/conversation-reply-row | merged |
| [!245](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/245) | feat/web-page-next-up | merged |
| [!247](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/247) | feat/web-artifacts-page | merged |
| [!253](https://gitlab.brainchurts.com/ubixsys/ubixshepherd/-/merge_requests/253) | docs/web-ui-fidelity | merged |

## Suggested actions

1. Close lanes 282 and 232 and the empty worktrees (UbixOS docs-agent-on-ubixos-2,
   ubixkelpie docs-database-setup, brainchurts-web ci-mr-pipelines, ubixos-web
   fix-ci-promote-failure); each has no commits.
2. Retry `build-dev` in ubixos-web #7471.
3. Fix the GitLab storage error, then push ubixkelpie docs/architecture-audit.
4. Human: merge !260, !264 and UbixOS #52; resolve threads on !259, !261, !262, !263.
5. Look at the flaky `TestUpgradeFailureKeepsTheBinaryAndIsNotRetried`.
