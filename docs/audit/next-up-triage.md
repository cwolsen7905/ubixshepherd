# Next up triage

A read-only check of every approved, started, planned and deferred Next-up item in
ubixshepherd and ubixkelpie, against `dev` in both repos as of 2026-10-26 (ubixshepherd at
`fc844ce`). Nothing was changed: only the person changes an item's state.

Verdicts:

- **done**: the evidence says the item's outcome already shipped.
- **superseded or drop**: later work covers it, or it no longer earns its place.
- **ready now**: still needed, nothing in the way.
- **blocked**: still needed, waiting on the thing named.

Scope notes: item 102 belongs to uBixCore, which is not cloned here, so it was not
checked. The UbixOS work-matrix rows that `next list` appends (W-009 to W-078) are not
Next-up items and were left out.

## ubixshepherd

| id | title | state | verdict | evidence |
|---|---|---|---|---|
| 14 | Lane diff: `shepherd lane gate` | deferred | blocked: deferred by the person | `docs/lane-diff.md` still calls it a separate decision; no `lane gate` command on dev |
| 96 | Architecture audit: SOLID review of the Go core | started | done | Report is committed on lane `docs/architecture-audit` (`8d730b2`, `160189a`); that branch has no remote ref and no MR, and items 218 to 220 already cite it |
| 113 | Approving a scope decision on the web widens the lane directly | approved | ready now | `internal/daemon/widen.go` still needs the separate `lane_widen` call; dependency 121 is done |
| 115 | OpenCode gets read-only glab access to pipelines and jobs | approved | ready now | `internal/dispatch/opencode.go:277` allows only `glab mr view*` and `glab mr merge*` |
| 116 | Agents push only after the repo's check passes | approved | ready now | `internal/dispatch/pushblock.go` has no gate check |
| 117 | Fix flaky `TestAnswerContinuesTheAsker` | approved | ready now (unreproduced) | The test lives in `internal/dispatch/runner_test.go:646`, not `internal/daemon`; 30 runs with `-race` passed on 2026-10-26. Drop if it has not failed since the item was written |
| 178 | Per-repo agent allow-list | done | done | `7517081`, merged in `9221a08` |
| 182 | Web: Mark answered moves to the next question | approved | ready now (verify first) | `4092a6d` added Mark answered and `ChatQuestions.test.tsx` covers the clear, but no test checks that the next question shows; try it in the web UI before starting |
| 184 | Typed IDs MR 2: chips in the Inbox, cards and panes | approved | done | `275f84a`, `662e11d`, `ad34df4`, merged in `fc844ce`; the item still shows approved with its lane closed |
| 185 | Main-body pages for lane, diff, file and run log | planned | ready now | Only decisions swap to the main body (`web/src/shell/swap.ts`); part of the web UI overhaul |
| 186 | One shared Confirm, ContextComposer and MR approval | planned | ready now | `web/src/ui/Confirm.tsx` and `ContextComposer.tsx` exist, but `LaneDetailApprove.tsx` and the inspector composer remain beside them |
| 187 | RunLog live and transcript variants, Stop run | planned | ready now | `web/src/ui/RunLog.tsx` exists; no live or transcript variant commits since |
| 188 | Command palette to the design | planned | ready now | No palette commits after the UI build 1 shell (`f360a1e`) |
| 189 | Shared empty, loading and error states | planned | done (check leftovers) | `8ccfd56`, `4b0cf23`, `1b5e200` added `EmptyState`, `ErrorState`, `LoadingRows` and Retry; a sweep for leftover one-off error lines may remain |
| 190 | Lane panel and page parity | planned | ready now | No commits since the audit; touches the same lane files as 185 and 186 |
| 191 | Next up detail as the side panel, rows on ListRow | planned | ready now (partly done) | `e8e3903` added a next-up side-panel pane; ListRow rows, Artifacts and Inbox DRY remain |
| 192 | Keys sheet, glyph legend and shortcuts agree | planned | ready now | No commits on the keys sheet since `f360a1e` |
| 196 | Inbox entries carry enough context to decide | approved | ready now (partly done) | Typed chips landed (`275f84a`); cause, linked item and recommendation per kind are not there |
| 197 | Main branch pipelines fail with no jobs | planned | ready now | Main pipeline 9259 at `fc844ce` still fails with no jobs, as do 9093, 9085, 9082 and 9081; the CI include is now pinned to a commit (`cfd63c9`) and has not fixed it |
| 205 | Adopt the design's round 4 shared parts | planned | ready now | Overlaps 185 to 192; do after them so existing parts are replaced rather than duplicated |
| 211 | Web: Yes / No buttons on yes-or-no questions | approved | ready now | No yes/no code in `web/src` on dev |
| 212 | `make clean` deletes tracked scripts in `bin/` | planned | ready now | `Makefile:149` still runs `rm -rf bin` |
| 218 | refactor(client): client stops importing daemon | approved | ready now | `internal/client/client.go:20` still imports `internal/daemon` |
| 219 | refactor(dispatch): drop the OpenCode globals | approved | ready now | `openCodeSettings`, `openCodeIdle`, `openCodeProbe` are still package vars (`opencode.go:51,64,91`) |
| 220 | refactor(config): one agent list | approved | ready now | `config.Agents` exists but the agent enum is repeated at `internal/cli/mcp.go:204,229,920` and `internal/chat/view.go:122` |
| 221 | Crook step 2 follow-up: `crook` feed line, docs links | approved | ready now | No `crook` feed kind in `internal/api`, `internal/daemon` or `web/src`; dependency 122 is done |
| 222 | Web: chat thread column grows to a max width | approved | ready now | The width work (`4a22b71`, `b058df2`) predates the item; `chat.css` sets `max-width: none` with no ceiling |
| 224 | Keep-going follow-up: coalescing channel | approved | ready now | Dependency 124 is done; the drive still runs inline in the feed hub poll |
| 225 | Keep-going follow-up: link desk questions to item and lane | approved | ready now | Same review findings, nothing merged for them since |
| 226 | Keep-going follow-up: summary attribution | approved | ready now | Same review findings, nothing merged for them since |
| 227 | Keep-going follow-up: autonomy docs and small points | approved | ready now (partly done) | The `keepgoing_checks` tag is already in `make check` (`Makefile:93`); the autonomy sentence for drive-started agents and the tests remain |
| 230 | Lane stall alert for unpushed or red lanes | approved | ready now | `keepgoing_stall.go` alerts on a stalled workspace, not on one lane idle for N hours; the Waiting list has a `ship` kind for unpushed work but no time threshold |
| 231 | Queued follow-ups on a running lane | approved | ready now | No queue code for `run_continue` on dev |
| 232 | Scope leases: warn at open, `lane_narrow` | approved | ready now | No `lane_narrow` tool on dev |
| 233 | `run_status` leads with the final report | approved | ready now | No such field in `internal/cli/run*.go` or `mcp.go` |
| 235 | Inbox: MRs blocked only on sign-off, approve from there | approved | done (verify) | The Inbox Approve group lists `mr` rows (`web/src/pages/Inbox.tsx:20`, from `389c3c6`), fed by the `mr` waiting kind (`35c351d`); confirm an approve button shows on those rows |
| 236 | Topic filing weighs what the message names | approved | ready now | The filing rule in `internal/daemon/topic.go` has no naming override |
| 237 | Crook step 3 follow-up: layer config, rules view, docs | approved | ready now (partly done) | `shepherd crook rules` exists (`internal/cli/crook.go:36`); layer-folder config keys, docs and the base-branch repo layer remain |
| 238 | Follow-through owner: per-note author | approved | ready now | `forge.Thread.Author` is the first note's author only, and `ownerUnfixed` takes the joined body (`internal/watch/followthrough.go:168`) |
| 239 | `changedFiles` reads names with `-z` | approved | ready now | `internal/dispatch/ship.go:217` still splits `--name-only` output on newlines; no `TestChangedFilesReadsExactNames` on dev |
| 240 | A retry failing again in the same pipeline shouldn't trigger takeover | approved | ready now | Nothing in `internal/watch` guards on the pipeline id |
| 241 | A spent takeover hands off instead of going silent | approved | ready now | Nothing in `internal/watch` hands off after the tries run out; do with 240 |
| 242 | Guard: agents can't `pkill` or `killall` | approved | ready now | No deny rule on dev; the file it was waiting on is free now (`0590ced`, `f9dc2ed`) |
| 244 | Prefixed, clickable IDs in chat | approved | superseded | The typed-ID series covers it: `IdChip` (`de53a64`), Inbox, cards and panes (`275f84a`), chat rows (`rows.tsx`). What is left (desk text, chat linkify, side-pane open on click) belongs to the series' later MRs |

## ubixkelpie

| id | title | state | verdict | evidence |
|---|---|---|---|---|
| 177 | K-33 push mode | approved | ready now (after `feat/project-mode-enforce`) | `ProjectMode::Push` exists and `ProposalReactionService` writes `fix(review)` commits with a Thread line; no `Fixed: <sha>` reply, one-push-per-round limit or MR-files-only rule yet. The unmerged `feat/project-mode-enforce` touches the same area |
| 198 | Issue autofix step 1: classify Issue events | approved | done | `1c424da`, merged in `851207f`; design doc table row 1 describes it |
| 199 | Issue autofix step 2: triage comment and labels | approved | done | `fd0ad1c`, `6f8a747`, `99e7dab`, merged in `496d4f3` |
| 200 | Issue autofix step 3: fix and MR on `kelpie::fix` | approved | ready now | Dependency 199 is done; no issue fixer or `Closes #N` MR code on dev. Autofix is off in every manifest until propose mode is enforced (`a8cf12f`) |
| 201 | Issue autofix step 4: failure paths | approved | blocked on 200 | Nothing to fail until the fixer exists |
| 202 | Issue autofix step 5: per-project enrolment list | approved | blocked on 201 | Chain order |
| 203 | Kelpie takes over MR review threads on ubixshepherd | approved | done (ops left) | The webhook now writes jobs and `kelpie:work` runs them (`af28cf0`), with the sweep only queueing; the Vault, flag and un-suspend steps are the person's |
| 214 | K-14 spend caps per project and per day | approved | ready now (partly done) | `Project::maxCostUsdDay` and the daily cap in the job coalescer exist; no refusal note, per-project spend view or review and triage call gating confirmed. Dependency 176 is done |
| 215 | K-12 prod gets its own bot token and webhook secret | approved | blocked on 214 | Also needs the person to create the token and write Vault |
| 216 | K-45 scan forge and repo text before a model | approved | blocked on 215 | No scanning code on dev |
| 217 | K-44 lessons from outcomes | approved | blocked on 177 | `thread_outcomes` exists (`8e922ac`), but the item waits on push mode |
| 245 | Why Kelpie left threads unresolved on several ubixshepherd MRs | planned | ready now | No thread-resolve call outside the already-fixed path (`67f49c1`); investigate propose mode (`8e922ac`) and thumbs handling (`6b83b2a`) first |
| 246 | Cut the next uBixKelpie beta | planned | blocked on `feat/project-mode-enforce` | That branch is not on `dev`; `CHANGELOG.md` has an empty Unreleased section and the only tag is `v0.1.0-beta.1` |

## Not checked

| id | title | state | note |
|---|---|---|---|
| 102 | CI pipeline docs in every uBixCore repo | approved | uBixCore is not cloned here, so there was no evidence to read |

## Items the person could mark done or drop

Mark done:

- 96: the audit report exists on its lane branch. Note that it is unpushed and has no MR, so the report is not on `dev` yet.
- 184: shipped in `fc844ce`; the item still shows approved.
- 198 and 199: shipped in Kelpie `851207f` and `496d4f3`.
- 203: shipped in code; keep it only if the ops steps need tracking.
- 189: shipped in `8ccfd56`; look once for leftover one-off error lines.
- 235: the Inbox Approve group already holds the MR rows; confirm the approve button, then close.

Drop or fold in:

- 244: covered by the typed-ID series. Drop, or keep as a small remainder for desk text and chat linkify.
- 117: drop if it has not failed since it was added. It did not fail in 30 `-race` runs, and the item names the wrong package.
- 205: fold into 185 to 192 if the person prefers a single web UI track.

## Ready to start now, in the order I would do them

Small and unblocking first, then correctness and safety, then the refactors, then features.

1. 197: main pipelines are red with no jobs, which gets in the way of promotion and releases.
2. 212: one-line `make clean` fix that currently stages deletions after a release build.
3. 242: agent guard against killing other lanes' runs.
4. 239: `-z` fix in `changedFiles`.
5. 238: carry the note author so an Unfixed marker cannot be forged.
6. 240 and 241: follow-through retry and hand-off, in one lane with 238's files.
7. 116: agents push only after the check passes.
8. 113: scope approval widens the lane directly.
9. 220, 218, 219: the audit refactors (agent list first, since it is the smallest).
10. 245: find out why Kelpie left threads unresolved (Kelpie).
11. 182 and 211: the two small web question fixes.
12. 196: Inbox context, then 222 for the chat width.
13. 231, 233, 232, 230, 236: the desk and lane workflow items.
14. 224, 225, 226, 227, 221, 237: the keep-going and Crook follow-ups.
15. 115: OpenCode glab reads.
16. 185 to 192, then 205: the web UI track, one lane at a time since they share files.
17. Kelpie, once `feat/project-mode-enforce` lands: 177, 214, then 200; and 246 to cut the beta.
