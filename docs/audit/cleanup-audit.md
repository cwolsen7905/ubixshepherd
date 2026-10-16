# Workspace cleanup audit

A read-only pass over the open lanes of a multi-repo Shepherd workspace, first taken
2026-10-15 and rechecked 2026-10-16. Nothing was pushed, merged or commented on while
gathering it. Specific repos, merge requests, pipelines and hosts are left out on purpose:
this repo is public, and the point is the pattern and what to check, not the instance.

## Method

For each open lane (`shepherd lane list`, or the `*-worktrees/` directories) record the
branch, its merge request or pull request and that request's state, the latest pipeline,
commits not on the remote, and uncommitted changes. Then give the lane one verdict:

| Verdict | Meaning |
|---|---|
| CLOSE | Work is merged, or the branch holds nothing to land |
| NEEDS-PUSH | Local commits the remote does not have |
| NEEDS-MERGE | Green and open, waiting on a person (approvals or threads) |
| BROKEN | A failing job; name the job and the cause |
| ABANDONED | No request, no commits, no recent activity |
| ACTIVE | Recent commits or a live run |

## Findings

Sixteen lanes were open on the first pass. By the second pass every request that was
waiting on a person had merged, so only the blocked push remained.

| Verdict | Count (first pass) | Notes |
|---|---|---|
| NEEDS-MERGE | 4 | All merged by the second pass |
| NEEDS-PUSH | 4 | Three were pushed and merged; one is blocked, see below |
| ACTIVE | 2 | New lanes opened during the audit |
| CLOSE | 1 | A plan lane whose merge requests had all landed |
| ABANDONED | 4 | Branches at the integration tip with no commits, no request, nothing on the remote |
| BROKEN | 1 | An integration branch, not the lane itself |

### Lessons

1. **Squash and merge-commit landings look unmerged to git.** Decide CLOSE from the
   forge's request state, never from `git branch --merged`.
2. **A lane's branch can be empty.** Four lanes were cut from the integration branch and
   never committed to. `rev-list --count <integration>..HEAD` of 0 plus no request is the
   signal; closing the lane and removing the worktree is safe.
3. **A retried job hides a flake.** A merged request showed a failed `go-check` in its
   history even though its pipeline ended green. Retried jobs (`include_retried=true`)
   showed a daemon test failing once: an upgrade test reported a checksum mismatch for an
   archive it had just built. Treat it as a flaky test worth a look, not as a web change
   problem.
4. **Pushes can fail on the server, not in policy.** A push rejected with
   `remote unpack failed: unable to create temporary object directory` is a git server
   storage problem (space or permissions on the repository's object directory). Branch
   protection, push rules and Shepherd's pre-push scope check all report differently.
   Check `ls-remote`, protected branches and push rules first; if they are clean, the fix
   is on the server, and the branch can be pushed unchanged afterwards.
5. **A registry blob error is not a code failure.** A build job that fails on
   `blob unknown to registry` while pushing an image is a registry fault: retry the job
   before touching the branch. The integration branch stays red until someone does.
6. **A pipeline with no jobs is a failure with no log.** The push pipeline on the release
   branch fails the moment it is created, with no jobs and no failure reason, on every
   promotion, while the same commit is green on the integration branch. Nothing in the job
   logs explains it; compare the pipeline's `source`, the workflow rules and the project's
   pipeline settings for the release branch.
7. **Required-approval jobs are expected to fail until sign-off.** A merge request
   pipeline can read `failed` only at the approval job ("no sign-off yet") while every
   build, lint, test and review job is green. Count the rest, not the whole pipeline.
8. **Plan steps with no branch are "never opened", not "failed".** For a multi-step plan
   with one request per step, map each step to a branch and a request by title, since
   request numbers need not follow step order (two adjacent steps merged in swapped order
   here). The first seven of nine steps had merged; the last two (a sweep and the
   rollout) had no branch.

## Suggested actions

1. Close lanes whose requests are merged and remove empty worktrees.
2. Retry registry-blocked build jobs on the integration branch.
3. Fix the git server's storage fault, then push the blocked branch unchanged.
4. Find why release-branch pipelines are created failed with no jobs.
5. Fix or quarantine the flaky upgrade test.
