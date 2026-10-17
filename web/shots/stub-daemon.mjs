// A stand-in for the Shepherd daemon, for screenshots: it serves the built web UI and
// canned /v1 answers with made-up data (acme-* repos, git.example). It never touches a
// real daemon or ~/.shepherd. Usage: node stub-daemon.mjs DIST_DIR [PORT]
// NODEC=1 leaves no open decisions or requests.
// BUSY=1 serves a heavy day for the timeline: about 25 lanes in five repos, each with 3 to 12
// runs through the working hours so far, all four agents, two failures, a decision waiting
// and two answered, a request that needs routing and six that were routed, three merges, a
// running bar in five lanes and a spend curve that crosses 80% of the budget.
import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'
import zlib from 'node:zlib'

const dist = path.resolve(process.argv[2] ?? 'dist')
const port = Number(process.argv[3] || process.env.SHOTS_PORT || 5199)
const NODEC = !!process.env.NODEC
const BUSY = !!process.env.BUSY
// LONG=1 makes the lane page, the run log and the changes long enough to scroll.
const LONG = !!process.env.LONG


// A busy day (BUSY=1), built from the current time and a fixed seed, so a shot repeats.
const BUSY_REPOS = [['acme-api', 7], ['acme-web', 6], ['acme-docs', 3], ['acme-infra', 5], ['acme-mobile', 3]]
const BUSY_AGENTS = ['claude', 'opencode', 'cursor', 'copilot']
const BUSY_MODELS = { claude: 'claude-sonnet-5-5', opencode: 'acme-large', cursor: 'cursor-auto', copilot: 'copilot-default' }
const BUSY_TASKS = [
  'Add pagination to the orders endpoint', 'Fix the stale cache after a price change', 'Split the queue worker into a poller and a handler',
  'Write an install guide for Linux and macOS', 'Bump the minor versions of the dependencies', 'Export the orders table as CSV',
  'Retry failed webhooks with a backoff', 'Move the session store behind an interface', 'Tidy the release notes script',
  'Add a dark theme to the settings page', 'Cover the invoice rounding with tests', 'Replace the polling loop with a watch',
]
const seeded = (seed) => { let s = seed; return () => (s = (s * 1664525 + 1013904223) % 4294967296) / 4294967296 }

function busyDay() {
  const rnd = seeded(7)
  const now = Date.now()
  const d = new Date(now)
  d.setHours(0, 0, 0, 0)
  const midnight = d.getTime()
  // The working hours so far: from midnight, or nine hours back when the day is young.
  const start = Math.max(midnight, now - 9 * 3600_000)
  const span = now - start
  const scale = Math.min(1, span / (6 * 3600_000))
  const MIN = 60000
  const iso = (t) => new Date(t).toISOString()
  const lanes = [], closedLanes = [], runs = []
  let laneId = 0, runId = 100
  const failedAt = new Set(['5:2', '12:1'])
  const runningLanes = new Set([3, 9, 14, 17, 21])
  const merged = new Set([2, 8])
  const openMerged = 15
  let straddleRepo = null

  BUSY_REPOS.forEach(([repo, n], repoIndex) => {
    for (let i = 0; i < n; i++) {
      laneId++
      const kind = ['feat', 'fix', 'chore', 'refactor', 'docs'][(laneId + repoIndex) % 5]
      const name = `${kind}/${BUSY_TASKS[(laneId * 5) % BUSY_TASKS.length].toLowerCase().split(' ').slice(1, 4).join('-').replace(/[^a-z-]/g, '')}`
      const created = iso(start - 3 * 3600_000 - rnd() * 2 * 3600_000)
      const base = {
        id: laneId, repo_id: repoIndex + 1, name, branch: name, base: 'dev', worktree: `/work/${repo}-${laneId}`,
        scope: [`${name.split('/')[0]}/**`, 'docs/**'], state: 'open', created, origin: { via: 'cli' }, repo,
        last_activity: iso(now - rnd() * 40 * MIN), last_activity_by: 'run',
      }
      const count = 3 + Math.floor(rnd() * 10)
      const isRunning = runningLanes.has(laneId)
      let t = start + rnd() * span * 0.25
      const laneRuns = []
      const add = (k, end) => {
        runId++
        const agent = BUSY_AGENTS[(laneId + k) % 4]
        const running = end === undefined
        const fails = failedAt.has(`${laneId}:${k}`)
        laneRuns.push({
          id: runId, lane_id: laneId, agent, model: BUSY_MODELS[agent], prompt: BUSY_TASKS[(runId * 7) % BUSY_TASKS.length],
          state: running ? 'running' : fails ? 'failed' : 'succeeded', log: '', start_sha: 'a1b2c3d', end_sha: running ? undefined : 'e4f5a6b',
          commits: fails ? 0 : 1 + Math.floor(rnd() * 3), started: iso(t), ended: end === undefined ? undefined : iso(end),
          cost_usd: agent === 'copilot' ? undefined : 0.3 + rnd() * 1.1, credits: agent === 'copilot' ? 2 : undefined,
          lane: name, repo, worktree: base.worktree,
          ...(running ? { progress: { line: 'running make check (1 commit, 9m)', step: 'running make check', commits: 1, elapsed_seconds: Math.round((now - t) / 1000) } } : {}),
        })
      }
      const finishBy = now - (isRunning ? 50 : 2) * MIN
      for (let k = 0; k < (isRunning ? count - 1 : count); k++) {
        const end = t + (6 + rnd() * 34) * MIN * scale
        if (end > finishBy) break
        add(k, end)
        t = end + (3 + rnd() * 22) * MIN * scale
      }
      if (isRunning) {
        t = Math.max(t, now - (8 + rnd() * 40) * MIN)
        add(count - 1, undefined)
      }
      runs.push(...laneRuns)
      if (merged.has(laneId)) {
        const closedAt = start + span * (laneId === 2 ? 0.45 : 0.7)
        closedLanes.push({
          ...base, state: 'closed', closed: iso(closedAt), outcome: 'merged', mr: 40 + laneId, mr_state: 'merged',
          mr_url: `https://git.example/acme/${repo}/-/merge_requests/${40 + laneId}`, mr_updated: iso(closedAt - 2 * MIN),
          runs: laneRuns.length, cost_usd: 2.4,
        })
        continue
      }
      if (laneId === openMerged) {
        lanes.push({ ...base, mr: 55, mr_state: 'merged', mr_url: `https://git.example/acme/${repo}/-/merge_requests/55`, ps: 'passed', pipeline: 9100 + laneId, pipeline_status: 'passed', mr_updated: iso(now - 40 * MIN) })
        continue
      }
      if (laneId % 4 === 1) {
        lanes.push({ ...base, mr: 60 + laneId, mr_state: 'open', mr_url: `https://git.example/acme/${repo}/-/merge_requests/${60 + laneId}`, pipeline: 9100 + laneId, pipeline_status: 'passed', mr_updated: iso(now - 30 * MIN) })
        continue
      }
      if (repo === 'acme-web' && straddleRepo === null) straddleRepo = { id: laneId, name, repo }
      lanes.push(base)
    }
  })

  // Three lanes with nothing today: idle, left out of the timeline unless asked for.
  for (const [i, name] of ['chore/old-bump', 'docs/stale-readme', 'fix/flaky-test'].entries()) {
    laneId++
    lanes.push({
      id: laneId, repo_id: 1 + i, name, branch: name, base: 'dev', worktree: `/work/idle-${laneId}`, scope: ['docs/**'], state: 'open',
      created: iso(midnight - (2 + i) * 24 * 3600_000), origin: { via: 'cli' }, repo: BUSY_REPOS[i][0],
      last_activity: iso(midnight - (2 + i) * 24 * 3600_000), last_activity_by: 'created',
    })
  }

  // One run that began before midnight and ended after it.
  if (straddleRepo) {
    runs.push({
      id: ++runId, lane_id: straddleRepo.id, agent: 'claude', model: BUSY_MODELS.claude, prompt: 'Overnight migration dry run', state: 'succeeded',
      log: '', start_sha: 'a1b2c3d', end_sha: 'e4f5a6b', commits: 2, started: iso(midnight - 20 * MIN), ended: iso(midnight + 10 * MIN),
      cost_usd: 1.4, lane: straddleRepo.name, repo: straddleRepo.repo, worktree: '/work/x',
    })
  }

  // The day's costs add up to about 85% of the budget with the front desk's turns.
  const budget = 25
  const total = runs.reduce((n, r) => n + (r.cost_usd ?? 0), 0)
  const k = (budget * 0.85 - 2.6) / total
  for (const r of runs) if (r.cost_usd !== undefined) r.cost_usd = Math.round(r.cost_usd * k * 100) / 100
  const spentRuns = runs.reduce((n, r) => n + (r.cost_usd ?? 0), 0)
  const spendUsd = Math.round((spentRuns + 2.6) * 100) / 100

  const ended = runs.filter((r) => r.state === 'succeeded' && r.ended && Date.parse(r.ended) < now - 12 * MIN)
    .sort((a, b) => Date.parse(b.ended) - Date.parse(a.ended))
  const asker = ended[0]
  const flying = runs.find((r) => r.state === 'running')
  const decisions = [
    {
      id: 5, run_id: asker.id, question: 'The CSV export can stream rows or buffer the whole file first. Which should it do?', options: ['Stream', 'Buffer'],
      recommendation: 'Stream', why: 'Orders tables reach a million rows; buffering would hold them all in memory.', state: 'open',
      agent: asker.agent, lane: asker.lane, repo: asker.repo, created: iso(Date.parse(asker.ended) - 6 * MIN),
    },
    ...ended.filter((r) => r !== asker).slice(4, 6).map((r, i) => ({
      id: 3 - i, run_id: r.id, question: i === 0 ? 'Should session tokens live server side or in a signed cookie?' : 'Keep the retry helper in internal/retry or move it?',
      options: ['A', 'B'], recommendation: 'A', state: 'answered', answer: 'A', answered: r.ended, agent: r.agent, lane: r.lane, repo: r.repo,
      created: iso(Date.parse(r.started) + 4 * MIN),
    })),
  ]
  const requestFrom = (id, state, r, minutesAgo) => ({
    id, from_run: r.id, kind: 'question', state, message: 'Which lane owns the shared retry helper in internal/retry?', depth: 0,
    from_agent: r.agent, from_lane: r.lane, repo: r.repo, created: iso(now - minutesAgo * MIN), updated: iso(now - (minutesAgo - 1) * MIN),
    ...(state === 'routed' ? { agent: 'claude', lane: 'feat/queue', target_run: r.id } : {}),
  })
  const requests = [
    requestFrom(30, 'needs_routing', flying, 4),
    ...ended.slice(1, 7).map((r, i) => requestFrom(24 + i, 'routed', r, 60 + i * 25)),
  ]
  return { lanes, closedLanes, runs, decisions, requests, spendUsd, budget }
}

let busyCache = null
const busyNow = () => {
  // Rebuilt at most once a second: a page makes several reads at once and they should agree.
  if (!busyCache || Date.now() - busyCache.at > 1000) busyCache = { at: Date.now(), day: busyDay() }
  return busyCache.day
}
const toRecord = (r) => {
  const { prompt, log, start_sha, end_sha, worktree, progress, ...rest } = r
  return { ...rest, lane_state: 'open', task: String(prompt).split('\n')[0] }
}

const ms = (m) => m * 60000
const ago = (m) => new Date(Date.now() - ms(m)).toISOString()
const ahead = (m) => new Date(Date.now() + ms(m)).toISOString()

// Lanes. `forge` carries the forge's last word; its times are minutes ago.
const lane = (id, repo_id, repo, name, o = {}) => {
  const { mr, mr_state, ps, activity, by, mrCreated, mrUpdated, pStart, pEnd, created = 300, ...rest } = o
  const t = (m) => (m === undefined ? undefined : ago(m))
  return {
    id, repo_id, name, branch: name, base: 'dev', worktree: `/work/${repo}-${id}`,
    scope: [`${name.split('/')[1].split('-')[0]}/**`, 'docs/**'], state: 'open', created: ago(created),
    origin: { via: 'cli' }, repo,
    ...(mr ? { mr, mr_state, mr_url: `https://git.example/acme/${repo}/-/merge_requests/${mr}` } : {}),
    ...(ps ? { pipeline: 9000 + id, pipeline_status: ps } : {}),
    last_activity: t(activity ?? created), last_activity_by: by ?? 'created',
    ...(mrCreated !== undefined ? { mr_created: t(mrCreated), mr_updated: t(mrUpdated ?? mrCreated) } : {}),
    ...(pStart !== undefined ? { pipeline_started: t(pStart), ...(pEnd !== undefined ? { pipeline_finished: t(pEnd) } : {}) } : {}),
    ...rest,
  }
}
const lanes = () => BUSY ? busyNow().lanes : [
  // An MR whose pipeline passed: waiting for a merge.
  lane(1, 1, 'acme-api', 'feat/login-sessions', { mr: 41, mr_state: 'open', ps: 'passed', activity: 35, by: 'pipeline', mrCreated: 190, mrUpdated: 40, pStart: 48, pEnd: 35, created: 420 }),
  // A failed pipeline.
  lane(2, 1, 'acme-api', 'fix/cache-invalidation', { mr: 42, mr_state: 'open', ps: 'failed', activity: 14, by: 'pipeline', mrCreated: 120, mrUpdated: 16, pStart: 26, pEnd: 14, created: 260 }),
  // An idle lane: its last run ended and nothing waits.
  lane(3, 2, 'acme-web', 'chore/bump-deps', { activity: 1500, by: 'commit', created: 1800 }),
  // Runs in flight.
  lane(4, 2, 'acme-web', 'feat/export-csv', { mr: 43, mr_state: 'open', ps: 'running', activity: 3, by: 'commit', mrCreated: 30, mrUpdated: 3, pStart: 3, created: 110 }),
  lane(5, 1, 'acme-api', 'refactor/queue-worker', { activity: 2, by: 'commit', created: 95 }),
  lane(6, 3, 'acme-docs', 'docs/install-guide', { mr: 44, mr_state: 'open', ps: 'pending', activity: 60, by: 'mr', mrCreated: 75, mrUpdated: 60, created: 140 }),
]

const run = (id, lane_id, ln, repo, state, endedMinAgo, o = {}) => ({
  id, lane_id, agent: 'claude', model: 'claude-sonnet-5-5', prompt: 'Implement the change described in the lane brief.',
  state, log: '', start_sha: 'a1b2c3d', end_sha: state === 'running' ? undefined : 'e4f5a6b', commits: 2,
  started: ago(endedMinAgo + (o.minutes ?? 9)), ended: state === 'running' ? undefined : ago(endedMinAgo),
  cost_usd: Math.round((0.4 + id / 7) * 100) / 100, lane: ln, repo, worktree: `/work/${repo}-${lane_id}`, ...o,
})
const longRuns = () => LONG ? Array.from({ length: 24 }, (_, i) => run(100 + i, 4, 'feat/export-csv', 'acme-web', 'succeeded', 200 + i * 20, { minutes: 8, prompt: `Follow-up ${i + 1} on the CSV export.` })) : []
const runs = () => BUSY ? busyNow().runs.slice(-200).reverse() : [
  run(14, 2, 'fix/cache-invalidation', 'acme-api', 'failed', 140, { minutes: 22, commits: 0, exit_code: 1, error: 'make check failed: TestPriceChange timed out after 30s', prompt: 'Invalidate the product cache when a price changes.' }),
  run(15, 1, 'feat/login-sessions', 'acme-api', 'succeeded', 48, { minutes: 31, commits: 3, prompt: 'Store session tokens server side with a 12 hour expiry.' }),
  run(16, 2, 'fix/cache-invalidation', 'acme-api', 'succeeded', 26, { minutes: 14, prompt: 'Invalidate the product cache when a price changes.' }),
  run(17, 4, 'feat/export-csv', 'acme-web', 'running', 0, {
    prompt: 'Add a CSV export to the orders table.',
    progress: { line: 'running make check (2 commits, 9m)', step: 'running make check', commits: 2, elapsed_seconds: 540 },
  }),
  run(18, 5, 'refactor/queue-worker', 'acme-api', 'running', 0, {
    agent: 'opencode', model: 'acme-large', prompt: 'Split the queue worker into a poller and a handler.',
    progress: { line: 'editing worker/poller.go (1 commit, 6m)', step: 'editing worker/poller.go', commits: 1, elapsed_seconds: 360 },
  }),
  run(19, 3, 'chore/bump-deps', 'acme-web', 'succeeded', 1500, { minutes: 6, agent: 'cursor', prompt: 'Bump the minor versions of the dependencies.' }),
  run(20, 6, 'docs/install-guide', 'acme-docs', 'succeeded', 75, { minutes: 17, agent: 'copilot', prompt: 'Write an install guide for Linux and macOS.' }),
  ...longRuns(),
]

const PLAN_MD = `# Export orders as CSV

## Approach

Rows stream from the API into a Blob as they arrive, so memory stays flat however large the table is.

## Steps

- Add a streaming writer in \`web/orders/export.ts\`.
- Quote fields with commas, quotes and newlines.
- Add tests for an empty table and for a million rows.

| Concern | Choice |
|---|---|
| Memory | O(chunk) |
| Backpressure | pause the reader when the Blob is slow |
`
const CSV_BODY = `The export needs to handle three concerns: **memory usage**, **quoting rules**, and **backpressure**.

### Current state

| File | Lines | Notes |
|---|---|---|
| web/orders/table.tsx | 340 | Renders the orders grid, already paginated |
| web/orders/export.ts | 82 | Stub that returns an empty blob |
| web/orders/types.ts | 45 | \`Order\` interface, shared |

### Options

\`\`\`
Option      Memory   UX        Complexity
─────────────────────────────────────────────
Stream      O(chunk)  instant   medium
Buffer      O(n)      on done   low
Paginated   O(page)   stepwise  high
\`\`\`

### Recommendation: Stream

1. Chunk size: 64 KB (configurable, default in \`web/orders/export.ts\`).
2. Headers: \`Content-Disposition: attachment; filename=orders.csv\`.
3. Quoting: RFC 4180 — double-quote fields containing commas, quotes, or newlines.
4. Backpressure: the response writer already pauses the read loop when the buffer fills.

### Why not buffer?

\`\`\`go
// Buffering would allocate:
//   len(orders) * avg(200 bytes) ≈ 200 MB for 1M rows
// Stream needs: 64 KB + overhead ≈ 0.1 MB
\`\`\`

### Risks

- [ ] Existing tests for \`export.ts\` expect a synchronous result.
- [ ] The gateway must support chunked responses (confirmed: it does).
- [ ] Large exports may time out the browser tab — add a progress indicator.`
const decisions = () => NODEC ? [] : BUSY ? busyNow().decisions : [
  {
    id: 5, run_id: 17, question: 'The CSV export can stream rows or buffer the whole file first. Which should it do?\n' + CSV_BODY,
    options: ['Stream', 'Buffer'], recommendation: 'Stream',
    artifacts: [{ id: 1, path: 'docs/plans/export-csv.md', title: 'CSV export plan' }],
    why: 'Orders tables reach a million rows; buffering would hold them all in memory.',
    state: 'open', agent: 'claude', lane: 'feat/export-csv', repo: 'acme-web', created: ago(8) },
  {
    id: 4, run_id: 15, question: 'Should session tokens live server side or in a signed cookie?',
    options: ['Server side, in the store', 'Signed cookie'], recommendation: 'Server side, in the store',
    why: 'Server-side sessions can be revoked at once.', body: '',
    state: 'answered', answer: 'Server side, in the store', answered: ago(170), agent: 'claude', lane: 'feat/login-sessions', repo: 'acme-api', created: ago(200) },
]
const requests = () => NODEC ? [] : BUSY ? busyNow().requests : [
  { id: 3, from_run: 18, kind: 'question', state: 'needs_routing', message: 'Which lane owns the shared retry helper in internal/retry?', depth: 0, from_agent: 'opencode', from_lane: 'refactor/queue-worker', repo: 'acme-api', created: ago(5), updated: ago(5) },
]

// The feed: the event of each item is given in `events`, parallel to `items`.
const FEED = [
  ['lane_opened', 'lane opened: refactor/queue-worker in acme-api', 5, 95],
  ['lane_opened', 'lane opened: feat/export-csv in acme-web', 4, 110],
  ['run_started', 'run 17 started: claude on feat/export-csv', 17, 9],
  ['run_started', 'run 18 started: opencode on refactor/queue-worker', 18, 6],
  ['commit', 'run 17 committed: feat(web): export orders as CSV', 17, 5],
  ['decision', 'decision 5: The CSV export can stream rows or buffer the whole file first. Which should it do?', 5, 8],
  ['request_needs_routing', 'request 3 needs routing: Which lane owns the shared retry helper', 3, 5],
  ['run_passed', 'run 16 passed: fix/cache-invalidation (1 commit)', 16, 26],
  ['pipeline', 'pipeline failed: fix/cache-invalidation !42', 2, 14],
  ['run_passed', 'run 15 passed: feat/login-sessions (3 commits)', 15, 48],
  ['mr', 'merge request !41 opened: feat/login-sessions', 1, 190],
]
const feedItems = () => FEED.map(([k, text, ref, m], i) => ({
  id: 100 + i, kind: k === 'decision' ? 'decision' : k, text, ref, created: ago(m),
})).sort((a, b) => Date.parse(a.created) - Date.parse(b.created)).map((it, i) => ({ ...it, id: 100 + i }))
const feedEvents = () => {
  const evOf = (k) => (k === 'decision' ? 'decision_asked' : k)
  return FEED.map(([k, , , m]) => [evOf(k), m]).sort((a, b) => b[1] - a[1]).map(([e]) => e)
}

// The desk thread. history() holds the older part; the stream delivers the newer part
// after the page connects, so the thread is seen to arrive.
const deskAll = () => [
  [1, 'user', 'What is waiting on me?', 1, 'human', 42],
  [2, 'turn_start', '', 1, 'human', 42],
  [3, 'tool', 'decision_list {"state":"open"}', 1, undefined, 42],
  [4, 'assistant', NODEC
    ? 'Nothing needs you.\n\n- **fix/cache-invalidation** failed its pipeline; the agent has been continued once.\n- Run 17 is working on **feat/export-csv**.'
    : 'Needs you:\n1. Choose whether the CSV export streams or buffers. [Answer decision 5](#/?open=decision%3A5)\n2. Look at the failed pipeline on **fix/cache-invalidation**. [Open the lane](#/lanes/2)\n\n- The agent for **fix/cache-invalidation** has been continued once.\n- Run 17 is working on **feat/export-csv**.\n\nI would stream: the orders table is large.', 1, undefined, 41],
  [5, 'turn_end', 'done', 1, undefined, 41],
  [6, 'user', 'Stream it. And how is the login MR doing?', 6, 'human', 4],
  [7, 'turn_start', '', 6, 'human', 4],
  [8, 'tool', 'lane_show {"lane":"feat/login-sessions"}', 6, undefined, 4],
  [9, 'assistant', NODEC
    ? 'Nothing needs you.\n\n- !41 for **feat/login-sessions** passed its pipeline 35 minutes ago.\n- acme-api merges by a person, so it waits for your approval.'
    : 'Needs you:\n1. Answer decision 5; the agent carries on once you do. [Answer decision 5](#/?open=decision%3A5)\n2. Approve !41 for **feat/login-sessions**: acme-api is merged by a person. [Review !41](#/lanes/1)\n\n- !41 passed its pipeline 35 minutes ago.\n- acme-docs: Merged by rule docs-only after its pipeline passed.', 6, undefined, 3],
  [10, 'turn_end', 'done', 6, undefined, 3],
  [11, 'system', 'run 16 passed: fix/cache-invalidation (1 commit)', 11, 'system', 1],
  [12, 'turn_start', '', 11, 'system', 1],
  [13, 'assistant', NODEC
    ? 'Nothing needs you.\n\n- Run 16 passed its gate.\n- The pipeline for !42 is running again.'
    : 'Needs you:\n1. Decision 5 still waits for you. [Answer decision 5](#/?open=decision%3A5)\n\n- Run 16 passed its gate.\n- The pipeline for !42 is running again.', 11, undefined, 0.5],
  [14, 'turn_end', 'done', 11, undefined, 0.5],
].map(([seq, kind, text, turn, origin, m]) => ({
  seq, workspace_id: 1, kind, ...(text ? { text } : {}), turn, ...(origin ? { origin } : {}), created: ago(m),
}))
const HISTORY_UNTIL = 10

const status = (detail) => ({
  version: '0.9.0', pid: 4242, started: ago(2600), store: '~/.shepherd/shepherd.db', config: '~/.shepherd/config.yaml',
  workspaces: [{ id: 1, name: 'acme', path: '/work', created: ago(9000), repos: BUSY ? 5 : 3, lanes: BUSY ? busyNow().lanes.length : 6 }],
  ...(detail ? {
    detail: {
      commit: '1f2e3d4', built_at: ago(4000), os: 'linux', arch: 'amd64', host: 'build-host', now: new Date().toISOString(),
      config_loaded: ago(2600), running: 2, open_lanes: 6, decisions: NODEC ? 0 : 1,
      quota_holds: [{ agent: 'copilot', until: ahead(95) }],
      spend_usd: 20.4, budget_usd: 25, forge: { last_poll: ago(0.7) },
    },
  } : {}),
})
const localDay = () => { const d = new Date(); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` }
const spend = () => ({ day: localDay(), usd: BUSY ? busyNow().spendUsd : 20.4, budget: 25, credit_usd: 0.4, by_source: {} })

const runLog = (id) => `# run ${id} · claude · feat/export-csv
${LONG ? Array.from({ length: 40 }, (_, i) => `step ${i + 1}: read the next file and check the quoting rules\n`).join('') : ''}
I read the lane brief and the scope. The export belongs in web/orders/**.

> Read web/orders/table.tsx
> Edit web/orders/export.ts

The export builds a Blob from rows as they arrive, so memory stays flat.
Added tests for quoting, commas in names and an empty table.

> Bash make check

ok  \tacme/web/orders\t0.412s

Committed: feat(web): export orders as CSV
Committed: test(web): quoting in the CSV export
`
const runEvents = (id) => ({
  events: [
    { id: 1, run_id: id, kind: 'text', text: 'Reading the lane brief and the scope.', created: ago(8) },
    { id: 2, run_id: id, kind: 'tool', status: 'ok', text: 'Edit web/orders/export.ts', created: ago(6) },
    { id: 3, run_id: id, kind: 'tool', status: 'ok', text: 'Bash make check', created: ago(2) },
  ],
  decisions: [],
})

const laneThread = () => ({
  items: [
    { seq: 1, kind: 'run', run: 17, agent: 'claude', state: 'running', time: ago(9) },
    { seq: 2, kind: 'user', run: 17, time: ago(9), text: 'Add a CSV export to the orders table.' },
    { seq: 3, kind: 'agent', run: 17, time: ago(8), text: 'I will read the table component first, then add the export beside it.' },
    { seq: 4, kind: 'tool', run: 17, time: ago(7), tool: 'Read', summary: 'web/orders/table.tsx', status: 'ok', output: 'export function OrdersTable() {…}' },
    { seq: 5, kind: 'tool', run: 17, time: ago(5), tool: 'Edit', summary: 'web/orders/export.ts', status: 'ok' },
    { seq: 6, kind: 'agent', run: 17, time: ago(2), text: 'The export is in place. Running the gate now.' },
  ],
  last: 6,
})

const json = (res, o, s = 200) => { res.writeHead(s, { 'content-type': 'application/json' }); res.end(JSON.stringify(o)) }

// Settings: a sample config document (acme repos, made-up values) and a preview that renders
// the staged ops as a diff. Nothing is written; a save only answers.
const FIELD = (key, kind, label, extra = {}) => ({ key, kind, label, ...extra })
const MERGE = { 'acme-api': 'human', 'acme-web': 'human', 'acme-docs': 'agent', 'acme-infra': 'human' }
const SAMPLE_REPOS = Object.keys(MERGE)
function configDoc() {
  const profile = (merge) => ({ gate: 'make check', base_branch: 'dev', autonomy: { push: 'human', merge } })
  const repos = Object.fromEntries(SAMPLE_REPOS.map((r) => [r, { raw: { autonomy: { merge: MERGE[r] } }, effective: profile(MERGE[r]), overridden: ['autonomy.merge'] }]))
  const profileFields = [
    FIELD(['gate'], 'string', 'Gate', { group: 'Checks', risk: true }),
    FIELD(['autonomy', 'push'], 'enum', 'Push', { group: 'Autonomy', choices: ['human', 'shepherd', 'agent'], risk: true }),
    FIELD(['autonomy', 'merge'], 'enum', 'Merge', { group: 'Autonomy', choices: ['human', 'agent'], risk: true }),
  ]
  return {
    path: '~/.shepherd/config.yaml', etag: 'shot-etag', can_write: true,
    raw: { daemon: { budget: 25, max_runs: 4 }, repos: Object.fromEntries(SAMPLE_REPOS.map((r) => [r, { autonomy: { merge: MERGE[r] } }])) },
    effective: { daemon: { budget: 25, max_runs: 4, poll: '60s' }, keep_going: { cap: 5 }, web: { editor_url: 'vscode://file/{path}' } },
    defaults: { raw: {}, effective: { gate: 'make check', autonomy: { push: 'human', merge: 'human' }, agent: { model: { claude: 'anthropic/claude-sonnet-5-5', opencode: 'acme/acme-large' } } }, overridden: [] },
    repos,
    projects: {
      acme: { raw: { repos: ['acme-api', 'acme-web'], autonomy: { push: 'shepherd' } }, effective: { repos: ['acme-api', 'acme-web'] } },
      'acme-docs': { raw: { repos: ['acme-docs'] }, effective: { repos: ['acme-docs'] } },
    },
    sections: [
      { id: 'daemon', label: 'Daemon', root: ['daemon'], fields: [FIELD(['budget'], 'number', 'Daily budget (USD)'), FIELD(['max_runs'], 'number', 'Max runs'), FIELD(['poll'], 'string', 'Poll', { restart: true })] },
      { id: 'keep_going', label: 'Keep going', root: ['keep_going'], fields: [FIELD(['cap'], 'number', 'Spend cap (USD)', { risk: true })] },
      { id: 'defaults', label: 'Defaults', root: ['defaults'], fields: profileFields },
      { id: 'repo', label: 'Repo profile', root: ['repos'], many: true, inherits: 'defaults', fields: profileFields },
      { id: 'project', label: 'Project', root: ['projects'], many: true, fields: [FIELD(['repos'], 'list', 'Repos')] },
    ],
  }
}
function configPreview(req, res) {
  let body = ''
  req.on('data', (d) => (body += d))
  req.on('end', () => {
    const ops = JSON.parse(body || '{}').ops ?? []
    const diff = []
    const risky = []
    for (const op of ops) {
      op.path.forEach((k, i) => {
        if (i < op.path.length - 1) diff.push(`+${'  '.repeat(i + 1)}${k}:`)
      })
      const last = op.path.length - 1
      diff.push(`+${'  '.repeat(last + 1)}${op.path[last]}: ${op.unset ? '(unset)' : op.value}`)
      if (op.path.slice(-2).join('.') === 'autonomy.merge' && op.value === 'agent') {
        risky.push({ path: op.path.join('.'), from: 'human', to: 'agent', why: 'lets an agent arm merge on its own lane, so no person reads the change first' })
      }
    }
    json(res, { etag: 'shot-etag', diff: diff.join('\n') + '\n', risky })
  })
}
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.json': 'application/json' }

function sse(res) {
  res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' })
  res.write(': open\n\n')
  const ping = setInterval(() => res.write(': ping\n\n'), 15000)
  res.on('close', () => clearInterval(ping))
}

function deskStream(req, res, u) {
  sse(res)
  const after = Number(u.searchParams.get('after') ?? req.headers['last-event-id'] ?? 0)
  let t = 300
  for (const e of deskAll().filter((x) => x.seq > after)) {
    setTimeout(() => !res.destroyed && res.write(`id: ${e.seq}\nevent: ${e.kind}\ndata: ${JSON.stringify(e)}\n\n`), (t += 250))
  }
}

// A lane's changes: a few commits, one file outside the scope, one uncommitted, one untracked.
function laneDiff(id, file) {
  const ln = lanes().find((l) => l.id === id) ?? lanes()[0]
  const f = (path, status, added, removed, o = {}) => ({ path, status, added, removed, in_scope: true, ...o })
  const base = {
    lane_id: id, lane: ln.name, repo: ln.repo, branch: ln.branch, base: 'dev', base_ref: 'origin/dev',
    merge_base: '9f1c2ab0', head: '4be7d19c33aa', mode: file ? 'file' : 'stat', read_at: new Date().toISOString(),
    commits: [
      { sha: '1a2b3c4d5e6f', subject: 'feat(auth): store sessions server side', author: 'Claude', date: ago(95), files: 3 },
      { sha: '5e6f7a8b9c0d', subject: 'test(auth): session expiry and revoke', author: 'Claude', date: ago(70), files: 2 },
      { sha: '4be7d19c33aa', subject: 'fix(auth): drop the cookie on logout', author: 'Claude', date: ago(41), files: 1 },
    ],
    commits_total: 3,
    committed: [
      f('internal/auth/session.go', 'modified', 84, 12), f('internal/auth/session_test.go', 'added', 120, 0),
      f('internal/auth/logout.go', 'modified', 6, 2), f('docs/sessions.md', 'added', 31, 0, { in_scope: false }),
      f('internal/auth/old_cookie.go', 'deleted', 0, 58),
    ],
    uncommitted: [f('internal/auth/store.go', 'modified', 9, 3)],
    untracked: [f('scratch/notes.txt', 'untracked', 0, 0, { size: 412, in_scope: false }), f('docs/plans/export-csv.md', 'untracked', 0, 0, { size: 2800, in_scope: false })],
    out_of_scope: 2,
  }
  if (LONG) base.committed.push(...Array.from({ length: 30 }, (_, i) => f(`internal/auth/part${i + 1}.go`, 'modified', 10 + i, i)))
  if (!file) return base
  return { ...base, file, patch: [
    `diff --git a/${file} b/${file}`, 'index 3f2a1c4..9d8e7b6 100644', `--- a/${file}`, `+++ b/${file}`,
    '@@ -41,9 +41,14 @@ func (s *Store) Open(ctx context.Context, id string) (*Session, error) {',
    ' \tif id == "" {', ' \t\treturn nil, ErrNoSession', ' \t}',
    '-\tsess, err := s.cookies.Get(id)', '+\tsess, err := s.rows.Get(ctx, id)',
    ' \tif err != nil {', '+\t\tif errors.Is(err, sql.ErrNoRows) {', '+\t\t\treturn nil, ErrExpired', '+\t\t}',
    ' \t\treturn nil, err', ' \t}',
  ].join('\n') + '\n' }
}



// A tiny PNG for the image artifact: a gradient with a bar chart, drawn with zlib only.
function samplePng() {
  const W = 480, H = 270
  const raw = Buffer.alloc((W * 3 + 1) * H)
  const bars = [0.35, 0.6, 0.45, 0.8, 0.55, 0.9, 0.7]
  for (let y = 0; y < H; y++) {
    raw[y * (W * 3 + 1)] = 0
    for (let x = 0; x < W; x++) {
      const o = y * (W * 3 + 1) + 1 + x * 3
      let c = [24 + (y * 30) / H, 38 + (y * 40) / H, 58 + (x * 30) / W]
      const b = Math.floor((x - 40) / 56)
      if (b >= 0 && b < bars.length && (x - 40) % 56 < 40 && y > H - 30 - bars[b] * (H - 80) && y < H - 30) c = [214, 168, 70]
      if (y === H - 30 && x > 30 && x < W - 20) c = [200, 205, 215]
      raw[o] = c[0]; raw[o + 1] = c[1]; raw[o + 2] = c[2]
    }
  }
  const crcTable = Array.from({ length: 256 }, (_, n) => { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c >>> 0 })
  const crc = (buf) => { let c = 0xffffffff; for (const b of buf) c = crcTable[(c ^ b) & 255] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0 }
  const chunk = (type, data) => {
    const len = Buffer.alloc(4); len.writeUInt32BE(data.length)
    const td = Buffer.concat([Buffer.from(type), data])
    const c = Buffer.alloc(4); c.writeUInt32BE(crc(td))
    return Buffer.concat([len, td, c])
  }
  const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(W, 0); ihdr.writeUInt32BE(H, 4); ihdr[8] = 8; ihdr[9] = 2
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('IDAT', zlib.deflateSync(raw)), chunk('IEND', Buffer.alloc(0))])
}

const EXPORT_TS = `// Streams orders into a Blob as they arrive, so memory stays flat.
export async function exportOrders(rows: AsyncIterable<Order>): Promise<Blob> {
  const parts: string[] = [HEADER]
  for await (const order of rows) {
    parts.push(quote(order) + '\\n')
  }
  return new Blob(parts, { type: 'text/csv' })
}

function quote(o: Order): string {
  return [o.id, o.customer, o.total].map((f) => /[",\\n]/.test(String(f)) ? '"' + String(f).replace(/"/g, '""') + '"' : String(f)).join(',')
}
`
const SESSION_GO = `package auth

import (
\t"context"
\t"database/sql"
\t"errors"
)

// Open reads a session from the store; an expired or revoked session is ErrExpired.
func (s *Store) Open(ctx context.Context, id string) (*Session, error) {
\tif id == "" {
\t\treturn nil, ErrNoSession
\t}
\tsess, err := s.rows.Get(ctx, id)
\tif err != nil {
\t\tif errors.Is(err, sql.ErrNoRows) {
\t\t\treturn nil, ErrExpired
\t\t}
\t\treturn nil, err
\t}
\treturn sess, nil
}
`
const README_MD = `# acme

Orders, sessions and the web storefront.

## Layout

- \`internal/auth\`: sessions, stored server side.
- \`web/orders\`: the orders table and its CSV export.
- \`docs\`: install guide and plans.

Run \`make check\` before every push.
`
// The files each lane's worktree holds, by path; a directory lists its children.
const FILES = {
  'README.md': README_MD,
  'Makefile': 'check:\n\tgofmt -l .\n\tgo vet ./...\n\tgo test ./...\n',
  'internal/auth/session.go': SESSION_GO,
  'internal/auth/session_test.go': 'package auth\n\nimport "testing"\n\nfunc TestOpenExpired(t *testing.T) {\n\tt.Skip("sample")\n}\n',
  'internal/auth/logout.go': 'package auth\n\n// Logout drops the session row and the cookie.\nfunc (s *Store) Logout(id string) error { return nil }\n',
  'web/orders/export.ts': EXPORT_TS,
  'web/orders/table.tsx': 'export function OrdersTable() {\n  return null\n}\n',
  'docs/plans/export-csv.md': PLAN_MD,
  'docs/install.md': '# Install\n\nDownload the archive for your system and put the binary on your path.\n',
}
function fileNode(p) {
  p = p.replace(/\/$/, '')
  const name = (x) => x.split('/').pop()
  if (Object.hasOwn(FILES, p)) return { path: p, name: name(p), kind: 'file', size: FILES[p].length, mime: 'text/plain', data: FILES[p] }
  const prefix = p === '' ? '' : p + '/'
  const kids = new Map()
  for (const f of Object.keys(FILES)) {
    if (!f.startsWith(prefix)) continue
    const rest = f.slice(prefix.length)
    const first = rest.split('/')[0]
    if (rest.includes('/')) kids.set(first, { path: prefix + first, name: first, kind: 'dir' })
    else kids.set(first, { path: f, name: first, kind: 'file', size: FILES[f].length })
  }
  if (kids.size === 0 && p !== '') return null
  const children = [...kids.values()].sort((a, b) => (a.kind === b.kind ? a.name.localeCompare(b.name) : a.kind === 'dir' ? -1 : 1))
  return { path: p, name: p === '' ? '' : name(p), kind: 'dir', children }
}

// Stored artifacts: a plan kept local, an image, a committed code file and an undecided one.
const ARTIFACTS = () => [
  { id: 1, workspace_id: 1, repo_id: 2, lane_id: 4, run_id: 17, path: 'docs/plans/export-csv.md', title: 'CSV export plan', kind: 'markdown', source: 'run', intent: 'keep', intent_by: 'person', state: 'kept', size: PLAN_MD.length, created: ago(30), updated: ago(9), pinned: true,
    versions: [{ artifact_id: 1, sha256: 'a'.repeat(64), size: 1900, run_id: 17, seen: ago(30), snapshotted: ago(30) }, { artifact_id: 1, sha256: 'b'.repeat(64), size: PLAN_MD.length, run_id: 17, seen: ago(9), snapshotted: ago(9) }],
    links: [{ artifact_id: 1, kind: 'decision', ref: '5', created: ago(8) }] },
  { id: 2, workspace_id: 1, repo_id: 2, lane_id: 4, run_id: 17, path: 'docs/img/orders-by-week.png', kind: 'image', source: 'run', intent: 'keep', intent_by: 'agent', state: 'live', size: 18200, created: ago(20), updated: ago(20),
    versions: [{ artifact_id: 2, sha256: 'c'.repeat(64), size: 18200, run_id: 17, seen: ago(20), snapshotted: ago(20) }] },
  { id: 3, workspace_id: 1, repo_id: 2, lane_id: 4, run_id: 17, path: 'web/orders/export.ts', kind: 'code', source: 'run', intent: 'commit', intent_by: 'agent', state: 'committed', size: EXPORT_TS.length, created: ago(25), updated: ago(5),
    versions: [{ artifact_id: 3, sha256: 'd'.repeat(64), size: EXPORT_TS.length, run_id: 17, seen: ago(5), snapshotted: ago(5) }] },
  { id: 4, workspace_id: 1, repo_id: 1, lane_id: 1, run_id: 15, path: 'scratch/session-notes.txt', kind: 'text', source: 'run', intent: 'undecided', intent_by: '', state: 'live', size: 412, created: ago(95), updated: ago(70),
    versions: [{ artifact_id: 4, sha256: 'e'.repeat(64), size: 412, run_id: 15, seen: ago(70) }] },
]
const ARTIFACT_TEXT = { 1: LONG ? PLAN_MD + Array.from({ length: 12 }, (_, i) => `\n## Follow-up ${i + 1}\n\nRows stream in chunks of 64 KB, and the writer pauses when the buffer fills, so the export stays flat however long the table is.\n`).join('') : PLAN_MD, 3: EXPORT_TS, 4: 'Session expiry: 12 hours, sliding.\nRevoke on logout and on password change.\nOpen: should the cookie carry only the id?\n' }
const laneArtifactList = (id) => ({
  lane_id: id, lane: lanes().find((l) => l.id === id)?.name ?? '', globs: ['docs/**', 'scratch/**'],
  artifacts: ARTIFACTS().filter((a) => a.lane_id === id).map((a) => ({ path: a.path, kind: a.kind, source: a.source, status: a.state, size: a.size, readable: true, ...(a.kind === 'image' ? { mime: 'image/png' } : {}) })),
})

// Closed lanes for History and Done today: one merged by a person's approval, one by a repo rule.
const closedLanes = () => [
  { ...lane(7, 2, 'acme-web', 'feat/lane-diff-web', { mr: 40, mr_state: 'merged', activity: 180, by: 'mr', created: 1500 }), state: 'closed', closed: ago(180), outcome: 'merged', runs: 4, cost_usd: 3.1 },
  { ...lane(8, 3, 'acme-docs', 'docs/changelog-0-9', { mr: 38, mr_state: 'merged', activity: 240, by: 'mr', created: 1400 }), state: 'closed', closed: ago(240), outcome: 'merged', runs: 2, cost_usd: 0.9 },
  { ...lane(9, 1, 'acme-api', 'fix/rate-limit-header', { mr: 36, mr_state: 'merged', activity: 2900, by: 'mr', created: 3200 }), state: 'closed', closed: ago(2900), outcome: 'merged', runs: 3, cost_usd: 1.7 },
  { ...lane(10, 2, 'acme-web', 'chore/old-spike', { activity: 4400, by: 'commit', created: 5000 }), state: 'closed', closed: ago(4400), outcome: 'dropped', runs: 1, cost_usd: 0.4 },
]

const BRIEF = { project: 'acme', approved: { text: 'Acme ships orders, sessions and the storefront. Priorities this week: the CSV export, then login sessions.', approved_at: ago(2000) }, age_text: '1d 9h', max_age: 7 * 24 * 3600e9 }
const projectDetail = (name) => {
  const inProject = name === 'acme' ? ['acme-api', 'acme-web'] : [name]
  const ls = lanes().filter((l) => inProject.includes(l.repo))
  return {
    name, repos: inProject, lanes: ls.length, running: ls.filter((l) => l.id === 4 || l.id === 5).length,
    decisions: 1, requests: 1, waiting: 2, spend_today: 14.3, budget: { amount: 25, cap: 'soft', period: 'day', since: ago(9000), spent: 14.3 },
    open_lanes: ls, runs: runs().filter((r) => ls.some((l) => l.id === r.lane_id)), pending_decisions: decisions().filter((d) => d.state === 'open'), pending_requests: requests(),
    ...(name === 'acme' ? { brief: BRIEF } : {}),
  }
}
const workFor = (name) => ({
  project: name,
  repos: name !== 'acme' ? [{ repo: name, found: false, rows: [], warnings: [] }] : [{ repo: 'acme-api', found: true, ref: 'origin/dev', blob: '3f2a1c4', warnings: [], rows: [
    { id: 'A-1', area: 'Sessions', item: 'Store sessions server side', status: 'doing', lane: 'feat/login-sessions', lane_id: 1, pr: 41, pr_url: 'https://git.example/acme/acme-api/-/merge_requests/41', repo: 'acme-api' },
    { id: 'A-2', area: 'Sessions', item: 'Revoke all sessions on password change', status: 'todo', repo: 'acme-api' },
    { id: 'A-3', area: 'Cache', item: 'Invalidate product cache on price change', status: 'blocked', lane: 'fix/cache-invalidation', lane_id: 2, pr: 42, notes: 'pipeline failing', repo: 'acme-api' },
    { id: 'A-4', area: 'Webhooks', item: 'Retry with backoff', status: 'done', pr: 35, repo: 'acme-api' },
  ] }, { repo: 'acme-web', found: true, ref: 'origin/dev', blob: '7a8b9c0', warnings: [], rows: [
    { id: 'W-1', area: 'Orders', item: 'Export the orders table as CSV', status: 'doing', lane: 'feat/export-csv', lane_id: 4, pr: 43, repo: 'acme-web' },
    { id: 'W-2', area: 'Orders', item: 'Filter orders by status', status: 'todo', repo: 'acme-web' },
  ] }],
  drift: [{ kind: 'lane_untracked', repo: 'acme-web', lane: 'chore/bump-deps', lane_id: 3, text: 'chore/bump-deps has no row in the work file' }],
})

// The next-up list (docs/backlog-next.md), held in memory so the page's actions work in the stub.
const nextItems = (() => {
  const it = (id, stored, rank, title, o = {}) => ({
    id, title, state: stored, stored_state: stored, rank, source: 'person', created: ago(o.age ?? 2880), updated: ago(o.age ?? 2880), ...o, age: undefined,
  })
  return [
    it(3, 'approved', 1, 'Lane diff step 3: gate record, badge and the run page range view', { repo: 'acme-api', plan: 'docs/lane-diff.md', age: 4300 }),
    it(1, 'approved', 2, 'Export the orders table as CSV', {
      repo: 'acme-web', plan: 'docs/csv-export.md', lane: 'feat/export-csv', lane_id: 4, lane_name: 'feat/export-csv', lane_state: 'open',
      state: 'started', running: true, mr: '!43', mr_url: 'https://git.example/acme/acme-web/-/merge_requests/43', mr_state: 'open', age: 5000,
    }),
    it(2, 'approved', 3, 'Retry failed webhooks with a backoff', {
      repo: 'acme-api', plan: 'docs/webhooks.md', age: 1500, depends_on: [3],
      blocked_on: [{ id: 3, title: 'Lane diff step 3: gate record, badge and the run page range view', state: 'approved' }],
    }),
    it(4, 'planned', 4, 'Widen a lane scope when the person approves it in a decision', { repo: 'acme-api', age: 600 }),
    it(5, 'planned', 5, 'Answered decisions read with no time bound', { source: 'agent', proposed: true, repo: 'acme-api', note: 'Found while reading the decisions page: older answers are cut off.', age: 90 }),
    it(6, 'deferred', 6, 'In-app editor for a lane diff', { repo: 'acme-web', plan: 'docs/lane-diff.md', age: 9000 }),
    it(7, 'deferred', 7, 'Compare two lanes', { repo: 'acme-web', plan: 'docs/lane-diff.md', age: 9000 }),
    it(8, 'done', 8, 'Lane diff step 2: the Changes tab', { state: 'done', repo: 'acme-web', plan: 'docs/lane-diff.md', lane_id: 7, lane_name: 'feat/lane-diff-web', mr: '!40', mr_state: 'merged', done_at: ago(1200), age: 7000 }),
    it(9, 'dropped', 9, 'Per-lane fix-try count', { repo: 'acme-api', plan: 'docs/backlog-actions.md', age: 8000 }),
  ]
})()

const nextView = () => {
  const open = (x) => x.stored_state !== 'done' && x.stored_state !== 'dropped'
  const order = { approved: 0, planned: 1, deferred: 2, done: 3, dropped: 4 }
  const sorted = [...nextItems].sort((a, b) => order[a.stored_state] - order[b.stored_state] || a.rank - b.rank)
  return { sorted, open }
}

function nextRoute(req, res, u, p) {
  const m = p.match(/^\/v1\/next(?:\/(\d+))?$/)
  if (!m) return false
  const id = m[1] ? Number(m[1]) : 0
  if (req.method === 'GET' && !id) {
    const { sorted, open } = nextView()
    const all = u.searchParams.get('all')
    const items = sorted.filter((x) => all || open(x))
    const first = sorted.find((x) => x.stored_state === 'approved' && x.state === 'approved')
    json(res, { items, ...(first ? { next_up: first.id } : {}) })
    return true
  }
  let body = ''
  req.on('data', (c) => (body += c))
  req.on('end', () => {
    const b = body ? JSON.parse(body) : {}
    if (req.method === 'POST' && !id) {
      const st = b.state === 'approved' ? 'approved' : 'planned'
      const x = { id: Math.max(0, ...nextItems.map((n) => n.id)) + 1, title: b.title, state: st, stored_state: st, rank: 100 + nextItems.length, source: 'person', created: new Date().toISOString(), updated: new Date().toISOString(), ...(b.repo ? { repo: b.repo } : {}), ...(b.plan ? { plan: b.plan } : {}) }
      nextItems.push(x)
      return json(res, x, 201)
    }
    const x = nextItems.find((n) => n.id === id)
    if (!x) return json(res, { error: 'no such item' }, 404)
    if (b.state) { x.stored_state = b.state; if (x.state !== 'started') x.state = b.state; x.proposed = undefined }
    if (b.lane_id !== undefined) { x.lane_id = b.lane_id || undefined }
    if (b.move) {
      const peers = nextItems.filter((n) => n.stored_state === x.stored_state).sort((a, c) => a.rank - c.rank)
      const at = peers.indexOf(x)
      const to = b.move === 'top' ? 0 : b.move === 'up' ? at - 1 : at + 1
      if (to >= 0 && to < peers.length) {
        const other = peers[to]
        ;[x.rank, other.rank] = [other.rank, x.rank]
      }
    }
    return json(res, x)
  })
  return true
}

// Routes the client called that no stub answers; the catch-all below fills it in.
const missingRoutes = new Set()
function track() {}

http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x'), p = u.pathname
  if (nextRoute(req, res, u, p)) return
  if (p === '/v1/desk/stream') return deskStream(req, res, u)
  if (p === '/v1/feed/stream') return sse(res)
  if (p === '/v1/status') return json(res, status(u.searchParams.get('detail') === '1'))
  if (p === '/v1/web/session') return json(res, { role: 'operator', csrf: 'shots', allow_settings: true })
  if (p === '/v1/lanes') return json(res, lanes())
  if (p === '/v1/config' && req.method === 'GET') return json(res, configDoc())
  if (p === '/v1/config/preview') return configPreview(req, res)
  const diff = /^\/v1\/lanes\/(\d+)\/diff$/.exec(p)
  if (diff) return json(res, laneDiff(Number(diff[1]), u.searchParams.get('file')))
  if (p === '/v1/runs') {
    const lane = Number(u.searchParams.get('lane_id') || 0)
    const limit = Number(u.searchParams.get('limit') || 200)
    return json(res, runs().filter((r) => !lane || r.lane_id === lane).slice(0, limit))
  }
  let m
  if ((m = p.match(/^\/v1\/lanes\/(\d+)\/conversation$/))) return json(res, laneThread())
  if ((m = p.match(/^\/v1\/runs\/(\d+)\/events$/))) return json(res, runEvents(+m[1]))
  if ((m = p.match(/^\/v1\/runs\/(\d+)\/log$/))) return json(res, { data: runLog(+m[1]), offset: 900, done: true })
  if ((m = p.match(/^\/v1\/runs\/(\d+)\/diagnoses$/))) return json(res, [])
  if ((m = p.match(/^\/v1\/runs\/(\d+)$/))) return json(res, runs().find((r) => r.id === +m[1]) ?? runs()[0])
  if (p === '/v1/decisions') {
    const s = u.searchParams.get('state')
    return json(res, decisions().filter((d) => !s || d.state === s))
  }
  if (p === '/v1/requests') {
    const states = (u.searchParams.get('state') || '').split(',').filter(Boolean)
    return json(res, requests().filter((q) => states.length === 0 || states.includes(q.state)))
  }
  if (p === '/v1/unassigned/work') return json(res, { project: 'Unassigned', repos: [{ repo: 'acme-mobile', found: false, rows: [], warnings: [] }], drift: [] })
  if (p === '/v1/spend') return json(res, spend())
  if (p === '/v1/keep-going') return json(res, { on: true, spent_usd: 3.4, cap_usd: 5, runs_now: 2, max_runs: 3 })
  if (p === '/v1/feed') {
    const items = feedItems()
    const after = u.searchParams.get('after')
    const last = items[items.length - 1].id
    if (after === 'latest') return json(res, { items: [], last })
    return json(res, { items: items.filter((i) => i.id > Number(after || 0)), events: feedEvents().filter((_, i) => items[i].id > Number(after || 0)), last })
  }
  if (p === '/v1/desk/history') return json(res, { events: deskAll().filter((e) => e.seq <= HISTORY_UNTIL), more: false })
  if (p === '/v1/desk/status') return json(res, { workspace_id: 1, busy: false, queued: 0, attached: 1, session: 'shots', model: 'claude-sonnet-5-5', wake: 'attached' })
  if (p === '/v1/history/lanes') {
    const since = Date.parse(u.searchParams.get('since') || '') || 0
    const closed = BUSY ? busyNow().closedLanes : closedLanes()
    const id = Number(u.searchParams.get('lane_id') || 0)
    const all = [...closed, ...(id ? lanes() : [])]
    return json(res, all.filter((l) => (id ? l.id === id : Date.parse(l.closed) >= since)).map((l) => ({ ...l, runs: l.runs ?? 0, cost_usd: l.cost_usd ?? 0 })))
  }
  if (p === '/v1/history/runs') {
    const since = Date.parse(u.searchParams.get('since') || '') || 0
    const agent = u.searchParams.get('agent')
    const lane = Number(u.searchParams.get('lane_id') || 0)
    const limit = Number(u.searchParams.get('limit') || 500)
    const all = runs().map(toRecord).filter((r) => Date.parse(r.started) >= since && (!agent || r.agent === agent) && (!lane || r.lane_id === lane))
      .sort((a, b) => Date.parse(b.started) - Date.parse(a.started))
    return json(res, {
      runs: all.slice(0, limit), count: all.length, cost_usd: all.reduce((n, r) => n + (r.cost_usd ?? 0), 0),
      credits: all.reduce((n, r) => n + (r.credits ?? 0), 0), agents: [...new Set(all.map((r) => r.agent))].sort(),
      lanes: [...new Map(all.map((r) => [r.lane_id, { id: r.lane_id, repo: r.repo, name: r.lane }])).values()],
    })
  }

  // Stored artifacts: the list, one row, and its content (an image is a PNG, the rest text).
  if (p === '/v1/artifacts') {
    const f = (k) => Number(u.searchParams.get(k) || 0)
    const state = u.searchParams.get('state'), kind = u.searchParams.get('kind')
    return json(res, ARTIFACTS().filter((x) => (!f('lane_id') || x.lane_id === f('lane_id')) && (!f('run_id') || x.run_id === f('run_id')) && (!state || x.state === state) && (!kind || x.kind === kind)))
  }
  if ((m = p.match(/^\/v1\/artifacts\/(\d+)(\/content)?$/))) {
    const a = ARTIFACTS().find((x) => x.id === +m[1])
    if (!a) return json(res, { error: 'no such artifact' }, 404)
    if (!m[2]) return json(res, a)
    if (a.kind === 'image') { res.writeHead(200, { 'content-type': 'image/png' }); return res.end(samplePng()) }
    res.writeHead(200, { 'content-type': a.kind === 'markdown' ? 'text/markdown' : 'text/plain' })
    return res.end(ARTIFACT_TEXT[a.id] ?? '')
  }
  if ((m = p.match(/^\/v1\/lanes\/(\d+)\/artifacts(\/content)?$/))) {
    if (!m[2]) return json(res, laneArtifactList(+m[1]))
    const rel = u.searchParams.get('path') ?? ''
    const a = ARTIFACTS().find((x) => x.lane_id === +m[1] && x.path === rel)
    if (a?.kind === 'image') { res.writeHead(200, { 'content-type': 'image/png' }); return res.end(samplePng()) }
    res.writeHead(200, { 'content-type': 'text/plain' })
    return res.end(a ? (ARTIFACT_TEXT[a.id] ?? '') : '')
  }
  if ((m = p.match(/^\/v1\/lanes\/(\d+)\/files$/))) {
    const node = fileNode(u.searchParams.get('path') ?? '')
    return node ? json(res, node) : json(res, { error: 'no such path' }, 404)
  }

  // GET /v1/waiting: everything that waits on the person, with a version that moves.
  if (p === '/v1/waiting') {
    const version = 3
    return json(res, { version, count: 6, counts: { decisions: 1, mrs: 1, ship: 0, next: 1, questions: 1, blocked: 1, scope: 0, requests: 1 }, items: [
      { kind: 'decision', action: 'answer', repo: 'acme-web', lane: 'feat/export-csv', lane_id: 4, decision_id: 5, title: 'CSV export: stream or buffer?', detail: 'run 17', since: ago(8) },
      { kind: 'mr', action: 'approve', repo: 'acme-api', lane: 'feat/login-sessions', lane_id: 1, mr: 41, title: '!41 pipeline passed', detail: 'waits for merge', since: ago(35) },
      { kind: 'question', action: 'reply', repo: 'acme-infra', question_id: 2, turn: 11, title: 'Should we bump the Go minor version in acme-infra?', detail: 'front desk, turn 11', since: ago(2) },
      { kind: 'blocked', action: 'open', repo: 'acme-api', lane: 'fix/cache-invalidation', lane_id: 2, title: 'Pipeline failed !42', detail: 'agent continued once', since: ago(14) },
      { kind: 'next', action: 'approve', repo: 'acme-api', next_id: 5, title: 'Answered decisions read with no time bound', detail: 'proposed by an agent', since: ago(90) },
      { kind: 'request', action: 'route', repo: 'acme-api', lane: 'refactor/queue-worker', lane_id: 5, request_id: 3, title: 'Which lane owns the shared retry helper in internal/retry?', detail: 'from opencode', since: ago(5) },
    ]})
  }

  // GET /v1/questions: front desk questions to the person.
  if (p === '/v1/questions') {
    const s = u.searchParams.get('state')
    const qs = [
      { id: 1, workspace_id: 1, turn: 6, text: 'How is the login MR doing?', state: 'answered', answered_turn: 10, created: ago(42) },
      { id: 2, workspace_id: 1, turn: 11, text: 'Should we bump the Go minor version in acme-infra?', state: s === 'answered' ? 'answered' : 'open', created: ago(2) },
    ]
    return json(res, s === 'answered' ? qs.filter((q) => q.state === 'answered') : qs.filter((q) => q.state === 'open' || !s))
  }

  // GET /v1/topics: conversation topics.
  if (p === '/v1/topics' || p.startsWith('/v1/topics?')) {
    return json(res, { topics: [
      { id: 1, title: 'CSV export', status: 'open', summary: 'feat/export-csv', source: 'desk', created: ago(110), updated: ago(8), activity: ago(8), waits: 1, settled: false, links: [
        { kind: 'lane', ref: 'feat/export-csv', by: 'desk', state: 'open', time: ago(110) },
        { kind: 'decision', ref: '5', by: 'desk', state: 'open', time: ago(8) },
        { kind: 'run', ref: '17', by: 'desk', state: 'running', time: ago(9) },
      ]},
      { id: 2, title: 'Login sessions', status: 'open', summary: 'feat/login-sessions', source: 'desk', created: ago(420), updated: ago(3), activity: ago(3), waits: 1, settled: false, links: [
        { kind: 'lane', ref: 'feat/login-sessions', by: 'desk', state: 'open', time: ago(420) },
        { kind: 'decision', ref: '4', by: 'desk', state: 'answered', time: ago(200) },
        { kind: 'turn', ref: '6', by: 'desk', time: ago(4) },
      ]},
      { id: 3, title: 'Cache invalidation', status: 'cleared', summary: 'fix/cache-invalidation', source: 'desk', created: ago(260), updated: ago(14), activity: ago(14), waits: 0, settled: true, links: [
        { kind: 'lane', ref: 'fix/cache-invalidation', by: 'desk', state: 'open', time: ago(260) },
        { kind: 'run', ref: '16', by: 'desk', state: 'succeeded', time: ago(26) },
      ]},
    ]})
  }

  // PATCH /v1/topics/{id}: update a topic.
  if ((m = p.match(/^\/v1\/topics\/(\d+)$/))) {
    track('/v1/topics/{id}')
    return json(res, { id: +m[1], title: 'updated', status: 'open', source: 'person', created: ago(100), updated: new Date().toISOString(), activity: new Date().toISOString(), waits: 0, links: [] })
  }

  // GET /v1/keep: keep-going status.
  if (p === '/v1/keep') {
    const day = busyNow()
    const flying = day.runs.find((r) => r.state === 'running')
    return json(res, { enabled: true, run: flying?.id, lane: flying?.lane })
  }

  // POST /v1/desk/turn: send a front desk turn.
  if (p === '/v1/desk/turn') {
    track('/v1/desk/turn')
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => {
      try {
        const b = JSON.parse(body)
        return json(res, { workspace_id: b.workspace_id || 1, turn: 200, ahead: 0 })
      } catch {
        return json(res, { workspace_id: 1, turn: 200, ahead: 0 })
      }
    })
    return true
  }

  // POST /v1/runs/{id}/dismiss: dismiss a failed run from the board.
  if ((m = p.match(/^\/v1\/runs\/(\d+)\/dismiss$/))) {
    track('/v1/runs/{id}/dismiss')
    return json(res, { id: +m[1], dismissed: new Date().toISOString() })
  }

  // POST /v1/runs/{id}/undismiss: restore a dismissed run.
  if ((m = p.match(/^\/v1\/runs\/(\d+)\/undismiss$/))) {
    track('/v1/runs/{id}/undismiss')
    return json(res, { id: +m[1], dismissed: undefined })
  }

  // POST /v1/decisions/{id}/answer: answer a decision.
  if ((m = p.match(/^\/v1\/decisions\/(\d+)\/answer$/))) {
    track('/v1/decisions/{id}/answer')
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => {
      try {
        const b = JSON.parse(body)
        return json(res, { id: +m[1], answer: b.answer, state: 'answered', answered: new Date().toISOString() })
      } catch {
        return json(res, { id: +m[1], state: 'answered', answered: new Date().toISOString() })
      }
    })
    return true
  }

  // GET /v1/lanes/{id}/approval: a lane's merge request, as the approval control shows it.
  if ((m = p.match(/^\/v1\/lanes\/(\d+)\/approval$/))) {
    track('/v1/lanes/{id}/approval')
    const ln = lanes().find((l) => l.id === +m[1])
    if (!ln?.mr) return json(res, { supported: true, approved: false })
    return json(res, { supported: true, mr: ln.mr, mr_url: ln.mr_url, mr_state: 'opened', sha: '4be7d19c33aa91f0', approved: false, pipeline: ln.pipeline, pipeline_status: ln.pipeline_status === 'passed' ? 'success' : ln.pipeline_status })
  }

  // POST /v1/fold/review: judge a repo's worktrees (no apply).
  if (p === '/v1/fold/review') {
    track('/v1/fold/review')
    return json(res, [
      { branch: 'feat/export-csv', worktree: '/work/acme-web-feat-export-csv', lane: 'feat/export-csv', verdict: 'live', why: 'a run is in progress' },
      { branch: 'chore/old-spike', worktree: '/work/acme-web-old-spike', verdict: 'finished', why: 'clean, and every commit is in dev', in_base: true },
    ])
  }

  // POST /v1/lanes/{id}/close: close a lane.
  if ((m = p.match(/^\/v1\/lanes\/(\d+)\/close$/))) {
    track('/v1/lanes/{id}/close')
    return json(res, { lane: { id: +m[1], name: 'closed-lane' }, branch_deleted: false, notes: ['lane closed'] })
  }

  // POST /v1/lanes/{id}/approve: approve a lane's MR.
  if ((m = p.match(/^\/v1\/lanes\/(\d+)\/approve$/))) {
    track('/v1/lanes/{id}/approve')
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => {
      const b = body ? JSON.parse(body) : {}
      return json(res, { supported: true, approved: true, approved_by: ['shots'], sha: b.sha || 'abc123', retried: false })
    })
    return true
  }

  // POST /v1/lanes/{id}/ship: push and open MR for a lane.
  if ((m = p.match(/^\/v1\/lanes\/(\d+)\/ship$/))) {
    track('/v1/lanes/{id}/ship')
    return json(res, { lane: 'shipped-lane', commits: 2, pushed: true, mr: 99, url: 'https://git.example/acme/acme-api/-/merge_requests/99', message: 'shipped' })
  }

  // POST /v1/topics: add a new topic.
  if (p === '/v1/topics') {
    track('/v1/topics')
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => {
      try {
        const b = JSON.parse(body)
        return json(res, { id: 100, title: b.title, status: 'open', source: 'person', created: new Date().toISOString(), updated: new Date().toISOString(), activity: new Date().toISOString(), waits: 0, links: [] })
      } catch {
        return json(res, { id: 100, title: 'new', status: 'open', source: 'person', created: new Date().toISOString(), updated: new Date().toISOString(), activity: new Date().toISOString(), waits: 0, links: [] })
      }
    })
    return true
  }

  // GET /v1/update: release check.
  if (p === '/v1/update') {
    return json(res, { enabled: true, channel: 'latest', current: '0.9.0', current_known: false, available: false, dismissed: false, show: false })
  }

  // POST /v1/update/dismiss: dismiss an update notice.
  if (p === '/v1/update/dismiss') {
    track('/v1/update/dismiss')
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => json(res, { enabled: true, current: '0.9.0', current_known: false, dismissed: true, show: false }))
    return true
  }

  // POST /v1/update/upgrade: install a new version.
  if (p === '/v1/update/upgrade') {
    track('/v1/update/upgrade')
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => json(res, { enabled: true, current: '1.0.0', current_known: true, available: false, show: false, upgrade: { state: 'restarting', method: 'binary' } }))
    return true
  }

  // GET /v1/projects: list projects.
  if (p === '/v1/projects') {
    return json(res, { projects: [
      { name: 'acme', unassigned: false, repos: ['acme-api', 'acme-web'], lanes: 5, running: 2, decisions: 1, requests: 1, waiting: 4, spend_today: 14.3, budget: { amount: 25, cap: 'soft', period: 'day', since: ago(9000), spent: 14.3 } },
      { name: 'acme-docs', unassigned: false, repos: ['acme-docs'], lanes: 1, running: 0, decisions: 0, requests: 0, waiting: 0, spend_today: 1.4 },
    ]})
  }

  // GET /v1/projects/{name}: project detail.
  if ((m = p.match(/^\/v1\/projects\/([^/]+)$/))) {
    track('/v1/projects/{name}')
    return json(res, projectDetail(decodeURIComponent(m[1])))
  }

  // GET /v1/unassigned: unassigned repos.
  if (p === '/v1/unassigned') {
    return json(res, { name: 'Unassigned', unassigned: true, repos: ['acme-mobile'], lanes: 0, running: 0, decisions: 0, requests: 0, waiting: 0, spend_today: 0, open_lanes: [], runs: [], pending_decisions: [], pending_requests: [] })
  }

  // GET /v1/projects/{name}/work: project work matrix.
  if ((m = p.match(/^\/v1\/projects\/([^/]+)\/work$/))) {
    track('/v1/projects/{name}/work')
    return json(res, workFor(decodeURIComponent(m[1])))
  }

  // GET /v1/usage: usage stats.
  if (p === '/v1/usage') {
    return json(res, {
      from: u.searchParams.get('from') || '2026-10-01', to: u.searchParams.get('to') || '2026-10-14',
      total: { count: 47, measured: 47, input_tokens: 1200000, cache_read_tokens: 800000, cache_creation_tokens: 200000, output_tokens: 340000, peak_context: 128000, over_200k: 0, compactions: 3, cost_usd: 20.4, credits: 0.4 },
      runs: { count: 32, measured: 32, input_tokens: 900000, cache_read_tokens: 620000, cache_creation_tokens: 150000, output_tokens: 280000, peak_context: 128000, over_200k: 0, compactions: 3, cost_usd: 14.6 },
      desk: { count: 15, measured: 15, input_tokens: 300000, cache_read_tokens: 180000, cache_creation_tokens: 40000, output_tokens: 60000, peak_context: 90000, over_200k: 0, compactions: 0, cost_usd: 5.8 },
      by_model: [
        { key: 'claude-sonnet-5-5', agent: 'claude', model: 'claude-sonnet-5-5', count: 24, measured: 24, input_tokens: 700000, cache_read_tokens: 500000, cache_creation_tokens: 120000, output_tokens: 190000, peak_context: 128000, over_200k: 0, compactions: 3, cost_usd: 11.2 },
        { key: 'acme-large', agent: 'opencode', model: 'acme-large', count: 8, measured: 8, input_tokens: 200000, cache_read_tokens: 0, cache_creation_tokens: 0, output_tokens: 90000, peak_context: 64000, over_200k: 0, compactions: 0, cost_usd: 3.4 },
      ],
      desk_by_model: [{ key: 'claude-sonnet-5-5', agent: 'claude', model: 'claude-sonnet-5-5', count: 15, measured: 15, input_tokens: 300000, cache_read_tokens: 180000, cache_creation_tokens: 40000, output_tokens: 60000, peak_context: 90000, over_200k: 0, compactions: 0, cost_usd: 5.8 }],
      by_lane: [
        { key: 'acme-web/feat/export-csv', repo: 'acme-web', lane: 'feat/export-csv', count: 9, measured: 9, input_tokens: 300000, cache_read_tokens: 200000, cache_creation_tokens: 50000, output_tokens: 80000, peak_context: 120000, over_200k: 0, compactions: 1, cost_usd: 6.1 },
        { key: 'acme-api/feat/login-sessions', repo: 'acme-api', lane: 'feat/login-sessions', count: 7, measured: 7, input_tokens: 250000, cache_read_tokens: 160000, cache_creation_tokens: 30000, output_tokens: 60000, peak_context: 98000, over_200k: 0, compactions: 1, cost_usd: 4.4 },
      ],
    })
  }

  // PUT /v1/config: save config.
  if (p === '/v1/config' && req.method === 'PUT') {
    track('/v1/config (PUT)')
    return json(res, { etag: 'shot-etag', diff: '--- a/config.yaml\n+++ b/config.yaml\n@@ -1 +1 @@\n-reload: false\n+reload: true', reloaded: true })
  }

  // GET /__shots/missing: the unstubbed /v1 routes called so far, for shots.sh to fail on.
  if (p === '/__shots/missing') return json(res, { missing: [...missingRoutes].sort() })

  // Catch-all for /v1/: log and return empty (strict mode fails).
  if (p.startsWith('/v1/')) {
    missingRoutes.add(p)
    if (process.env.STUB_STRICT === '1') {
      console.error(`shots: missing stub for ${p}`)
      return json(res, { error: `no stub for ${p}` }, 501)
    }
    return json(res, {})
  }
  const f = path.join(dist, p === '/' ? 'index.html' : p)
  if (f.startsWith(dist) && fs.existsSync(f) && fs.statSync(f).isFile()) {
    res.writeHead(200, { 'content-type': mime[path.extname(f)] ?? 'application/octet-stream' })
    return fs.createReadStream(f).pipe(res)
  }
  res.writeHead(404); res.end()
}).listen(port, '127.0.0.1', () => console.log(`stub daemon on http://127.0.0.1:${port} serving ${dist}`))

// On exit, report any /v1/ routes the client called but we had no stub for.
process.on('exit', () => {
  if (missingRoutes.size > 0) {
    console.error(`\nshots: unstubbed routes called by the client:`)
    for (const r of [...missingRoutes].sort()) console.error(`  ${r}`)
  }
})
