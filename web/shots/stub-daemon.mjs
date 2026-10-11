// A stand-in for the Shepherd daemon, for screenshots: it serves the built web UI and
// canned /v1 answers with made-up data (acme-* repos, git.example). It never touches a
// real daemon or ~/.shepherd. Usage: node stub-daemon.mjs DIST_DIR [PORT]
// NODEC=1 leaves no open decisions or requests.
import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'

const dist = path.resolve(process.argv[2] ?? 'dist')
const port = Number(process.argv[3] || process.env.SHOTS_PORT || 5199)
const NODEC = !!process.env.NODEC

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
const lanes = () => [
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
  started: ago(endedMinAgo + 9), ended: state === 'running' ? undefined : ago(endedMinAgo),
  cost_usd: Math.round((0.4 + id / 7) * 100) / 100, lane: ln, repo, worktree: `/work/${repo}-${lane_id}`, ...o,
})
const runs = () => [
  run(15, 1, 'feat/login-sessions', 'acme-api', 'succeeded', 48, { commits: 3, prompt: 'Store session tokens server side with a 12 hour expiry.' }),
  run(16, 2, 'fix/cache-invalidation', 'acme-api', 'succeeded', 26, { prompt: 'Invalidate the product cache when a price changes.' }),
  run(17, 4, 'feat/export-csv', 'acme-web', 'running', 0, {
    prompt: 'Add a CSV export to the orders table.',
    progress: { line: 'running make check (2 commits, 9m)', step: 'running make check', commits: 2, elapsed_seconds: 540 },
  }),
  run(18, 5, 'refactor/queue-worker', 'acme-api', 'running', 0, {
    agent: 'opencode', model: 'acme-large', prompt: 'Split the queue worker into a poller and a handler.',
    progress: { line: 'editing worker/poller.go (1 commit, 6m)', step: 'editing worker/poller.go', commits: 1, elapsed_seconds: 360 },
  }),
  run(19, 3, 'chore/bump-deps', 'acme-web', 'succeeded', 1500, { agent: 'cursor', prompt: 'Bump the minor versions of the dependencies.' }),
  run(20, 6, 'docs/install-guide', 'acme-docs', 'succeeded', 75, { agent: 'copilot', prompt: 'Write an install guide for Linux and macOS.' }),
]

const decisions = () => NODEC ? [] : [
  { id: 5, run_id: 17, question: 'The CSV export can stream rows or buffer the whole file first. Which should it do?', options: ['Stream', 'Buffer'], recommendation: 'Stream', why: 'Orders tables reach a million rows; buffering would hold them all in memory. Streaming needs a chunked response, which the gateway already allows.', state: 'open', agent: 'claude', lane: 'feat/export-csv', repo: 'acme-web', created: ago(8) },
  { id: 4, run_id: 15, question: 'Should session tokens live server side or in a signed cookie?', options: ['Server side, in the store', 'Signed cookie'], recommendation: 'Server side, in the store', why: 'Server-side sessions can be revoked at once.', state: 'answered', answer: 'Server side, in the store', answered: ago(170), agent: 'claude', lane: 'feat/login-sessions', repo: 'acme-api', created: ago(200) },
]
const requests = () => NODEC ? [] : [
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
  [4, 'assistant', 'One decision, and one stuck lane.\n\n- **feat/export-csv** asks whether the export should stream or buffer. I would stream: the orders table is large.\n- **fix/cache-invalidation** failed its pipeline; the agent has been continued once.\n\nNothing else needs you.', 1, undefined, 41],
  [5, 'turn_end', 'done', 1, undefined, 41],
  [6, 'user', 'Stream it. And how is the login MR doing?', 6, 'human', 4],
  [7, 'turn_start', '', 6, 'human', 4],
  [8, 'tool', 'lane_show {"lane":"feat/login-sessions"}', 6, undefined, 4],
  [9, 'assistant', 'I cannot answer the decision for you: answer it from the backlog or the Decisions page and the agent carries on.\n\n!41 for **feat/login-sessions** passed its pipeline 35 minutes ago and waits for a merge. Merging is yours: Shepherd never merges.', 6, undefined, 3],
  [10, 'turn_end', 'done', 6, undefined, 3],
  [11, 'system', 'run 16 passed: fix/cache-invalidation (1 commit)', 11, 'system', 1],
  [12, 'turn_start', '', 11, 'system', 1],
  [13, 'assistant', 'Run 16 finished and passed its gate. The pipeline for !42 is running again.', 11, undefined, 0.5],
  [14, 'turn_end', 'done', 11, undefined, 0.5],
].map(([seq, kind, text, turn, origin, m]) => ({
  seq, workspace_id: 1, kind, ...(text ? { text } : {}), turn, ...(origin ? { origin } : {}), created: ago(m),
}))
const HISTORY_UNTIL = 10

const status = (detail) => ({
  version: '0.9.0', pid: 4242, started: ago(2600), store: '~/.shepherd/shepherd.db', config: '~/.shepherd/config.yaml',
  workspaces: [{ id: 1, name: 'acme', path: '/work', created: ago(9000), repos: 3, lanes: 6 }],
  ...(detail ? {
    detail: {
      commit: '1f2e3d4', built_at: ago(4000), os: 'linux', arch: 'amd64', host: 'build-host', now: new Date().toISOString(),
      config_loaded: ago(2600), running: 2, open_lanes: 6, decisions: NODEC ? 0 : 1,
      quota_holds: [{ agent: 'copilot', until: ahead(95) }],
      spend_usd: 20.4, budget_usd: 25, forge: { last_poll: ago(0.7) },
    },
  } : {}),
})
const spend = () => ({ day: new Date().toISOString().slice(0, 10), usd: 20.4, budget: 25, credit_usd: 0.4, by_source: {} })

const runLog = (id) => `# run ${id} · claude · feat/export-csv

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

http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x'), p = u.pathname
  if (p === '/v1/desk/stream') return deskStream(req, res, u)
  if (p === '/v1/feed/stream') return sse(res)
  if (p === '/v1/status') return json(res, status(u.searchParams.get('detail') === '1'))
  if (p === '/v1/web/session') return json(res, { role: 'operator', csrf: 'shots', allow_settings: false })
  if (p === '/v1/lanes') return json(res, lanes())
  if (p === '/v1/runs') {
    const lane = Number(u.searchParams.get('lane_id') || 0)
    return json(res, runs().filter((r) => !lane || r.lane_id === lane))
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
  if (p === '/v1/requests') return json(res, requests())
  if (p === '/v1/spend') return json(res, spend())
  if (p === '/v1/feed') {
    const items = feedItems()
    const after = u.searchParams.get('after')
    const last = items[items.length - 1].id
    if (after === 'latest') return json(res, { items: [], last })
    return json(res, { items: items.filter((i) => i.id > Number(after || 0)), events: feedEvents().filter((_, i) => items[i].id > Number(after || 0)), last })
  }
  if (p === '/v1/desk/history') return json(res, { events: deskAll().filter((e) => e.seq <= HISTORY_UNTIL), more: false })
  if (p === '/v1/desk/status') return json(res, { workspace_id: 1, busy: false, queued: 0, attached: 1, session: 'shots', model: 'claude-sonnet-5-5', wake: 'attached' })
  if (p === '/v1/history/lanes') return json(res, [])
  if (p === '/v1/history/runs') return json(res, { runs: [], count: 0, cost_usd: 0, credits: 0, agents: [], lanes: [] })
  if (p.startsWith('/v1/')) return json(res, {})
  const f = path.join(dist, p === '/' ? 'index.html' : p)
  if (f.startsWith(dist) && fs.existsSync(f) && fs.statSync(f).isFile()) {
    res.writeHead(200, { 'content-type': mime[path.extname(f)] ?? 'application/octet-stream' })
    return fs.createReadStream(f).pipe(res)
  }
  res.writeHead(404); res.end()
}).listen(port, '127.0.0.1', () => console.log(`stub daemon on http://127.0.0.1:${port} serving ${dist}`))
