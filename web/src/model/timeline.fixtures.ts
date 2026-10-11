// A busy day, built in code: what the timeline's tests and the page's tests draw. 24 lanes in
// five repos, all four agents, a run that straddles midnight, two failures, a decision
// waiting and two answered, a request that needs routing, two merges and five runs in flight.
// The stub daemon (web/shots/stub-daemon.mjs) builds a day of its own with the same shape.
import type { DecisionView, LaneRecord, LaneView, RequestView, RunRecord, RunView } from '../api/types'
import type { Sources } from './timeline'

/** 15:00 local on Saturday 10 October 2026. */
export const BUSY_NOW = new Date(2026, 9, 10, 15, 0, 0).getTime()

const MIN = 60_000
const iso = (ms: number) => new Date(ms).toISOString()

export const BUSY_REPOS: Record<string, number> = { 'acme-api': 7, 'acme-web': 6, 'acme-docs': 3, 'acme-infra': 5, 'acme-mobile': 3 }

const AGENTS = ['claude', 'opencode', 'cursor', 'copilot'] as const

/** A small deterministic generator, so a fixture is the same on every run. */
function seeded(seed: number) {
  let s = seed
  return () => {
    s = (s * 1664525 + 1013904223) % 4294967296
    return s / 4294967296
  }
}

export interface Busy {
  src: Required<Sources>
  now: number
  /** Ids worth naming in a test. */
  ids: { straddling: number; failed: number[]; running: number[]; waiting: number; answered: number[]; merged: number[]; openMerged: number }
}

/** The busy day at `now` (15:00 on 10 October 2026 unless given). */
export function busyDay(now = BUSY_NOW): Busy {
  const rnd = seeded(7)
  const midnight = new Date(now)
  midnight.setHours(0, 0, 0, 0)
  const lanes: LaneView[] = []
  const closedLanes: LaneRecord[] = []
  const runs: RunView[] = []
  const historyRuns: RunRecord[] = []
  const ids = { straddling: 0, failed: [] as number[], running: [] as number[], waiting: 0, answered: [] as number[], merged: [] as number[], openMerged: 0 }

  let laneId = 0
  let runId = 100
  const repoNames = Object.keys(BUSY_REPOS)

  repoNames.forEach((repo, repoIndex) => {
    for (let i = 0; i < (BUSY_REPOS[repo] as number); i++) {
      laneId++
      const name = `${['feat', 'fix', 'chore', 'refactor', 'docs'][(laneId + repoIndex) % 5]}/${repo.split('-')[1]}-${laneId}`
      const base: LaneView = {
        id: laneId, repo_id: repoIndex + 1, name, branch: name, base: 'dev', worktree: `/work/${repo}-${laneId}`, scope: [`${repo}/**`],
        state: 'open', created: iso(midnight.getTime() - 5 * 60 * MIN), origin: { via: 'cli' }, repo,
      }
      // 3 to 12 runs through the working hours, one after another; a running lane's last is in flight.
      const count = 3 + Math.floor(rnd() * 10)
      const isRunningLane = [3, 9, 14, 17, 21].includes(laneId)
      let t = midnight.getTime() + (6.5 + rnd() * 5) * 60 * MIN
      const add = (k: number, end: number | undefined) => {
        runId++
        const running = end === undefined
        const agent = AGENTS[(laneId + k) % 4] as string
        const fails = (laneId === 5 && k === 2) || (laneId === 12 && k === 1)
        const rec: RunRecord = {
          id: runId, lane_id: laneId, repo, lane: name, lane_state: 'open', agent, model: `${agent}-model`,
          state: running ? 'running' : fails ? 'failed' : 'succeeded', commits: fails ? 0 : 1 + Math.floor(rnd() * 3),
          cost_usd: agent === 'copilot' ? undefined : Math.round((0.3 + rnd() * 1.1) * 100) / 100,
          credits: agent === 'copilot' ? 2 : undefined, started: iso(t), ...(end !== undefined ? { ended: iso(end) } : {}),
          task: `Task ${runId} for ${name}`,
        }
        historyRuns.push(rec)
        runs.push({
          id: rec.id, lane_id: laneId, agent, model: rec.model, prompt: `${rec.task}\nWith a second line.`, state: rec.state, log: '',
          start_sha: 'a1', commits: rec.commits, started: rec.started, ended: rec.ended, cost_usd: rec.cost_usd, credits: rec.credits,
          lane: name, repo, worktree: base.worktree,
          ...(running ? { progress: { line: `running make check (${rec.commits} commits)`, step: 'running make check', commits: rec.commits, elapsed_seconds: Math.round((now - t) / 1000) } } : {}),
        })
        if (fails) ids.failed.push(runId)
        if (running) ids.running.push(runId)
      }
      const finishBy = now - (isRunningLane ? 50 : 2) * MIN
      for (let k = 0; k < (isRunningLane ? count - 1 : count); k++) {
        const end = t + (6 + rnd() * 34) * MIN
        if (end > finishBy) break
        add(k, end)
        t = end + (3 + rnd() * 22) * MIN
      }
      if (isRunningLane) {
        t = Math.max(t, now - (8 + rnd() * 40) * MIN)
        add(count - 1, undefined)
      }
      // Two lanes merged and closed today; one more merged and not yet closed.
      if (laneId === 2 || laneId === 8) {
        const closedAt = midnight.getTime() + (laneId === 2 ? 11 : 13.5) * 60 * MIN
        closedLanes.push({
          ...base, state: 'closed', closed: iso(closedAt), outcome: 'merged', mr: 200 + laneId, mr_state: 'merged',
          mr_url: `https://git.example/acme/${repo}/-/merge_requests/${200 + laneId}`, mr_updated: iso(closedAt - 2 * MIN), runs: count, cost_usd: 3.1,
        })
        ids.merged.push(laneId)
        continue
      }
      if (laneId === 15) {
        lanes.push({ ...base, mr: 215, mr_state: 'merged', mr_url: 'https://git.example/acme/x/-/merge_requests/215', mr_updated: iso(now - 40 * MIN) })
        ids.openMerged = laneId
        continue
      }
      lanes.push(base)
    }
  })

  // One lane with nothing today: idle, and hidden unless asked for.
  laneId++
  lanes.push({
    id: laneId, repo_id: 1, name: 'chore/old-bump', branch: 'chore/old-bump', base: 'dev', worktree: '/work/idle', scope: [],
    state: 'open', created: iso(midnight.getTime() - 3 * 24 * 60 * MIN), origin: {}, repo: 'acme-api',
  })

  // A run that began before midnight and ended after it: only the history reads it.
  const straddleLane = lanes.find((l) => l.repo === 'acme-web') as LaneView
  runId++
  ids.straddling = runId
  historyRuns.push({
    id: runId, lane_id: straddleLane.id, repo: straddleLane.repo, lane: straddleLane.name, lane_state: 'open', agent: 'claude',
    state: 'succeeded', commits: 2, cost_usd: 1.4, started: iso(midnight.getTime() - 20 * MIN), ended: iso(midnight.getTime() + 10 * MIN),
    task: 'Overnight migration dry run',
  })

  // Decisions: one waiting on the person (on a running lane), two answered earlier.
  const flying = historyRuns.find((r) => r.state === 'running') as RunRecord
  const asker = historyRuns
    .filter((r) => r.state === 'succeeded' && r.ended !== undefined && Date.parse(r.ended) < now - 12 * MIN)
    .sort((a, b) => Date.parse(b.ended as string) - Date.parse(a.ended as string))[0] as RunRecord
  const askedAt = Date.parse(asker.ended as string) - 6 * MIN
  ids.waiting = 900
  const answeredFrom = historyRuns.filter((r) => r.state === 'succeeded').slice(3, 5)
  const decisions: DecisionView[] = [
    {
      id: 900, run_id: asker.id, question: 'Stream the export or buffer the whole file first?', options: ['Stream', 'Buffer'],
      recommendation: 'Stream', state: 'open', agent: asker.agent, lane: asker.lane, repo: asker.repo, created: iso(askedAt),
    },
  ]
  const answered: DecisionView[] = answeredFrom.map((r, i) => {
    ids.answered.push(901 + i)
    return {
      id: 901 + i, run_id: r.id, question: `Which of two options for ${r.lane}?`, options: ['A', 'B'], recommendation: 'A', state: 'answered' as const,
      answer: 'A', answered: iso(Date.parse(r.started) + 12 * MIN), agent: r.agent, lane: r.lane, repo: r.repo, created: iso(Date.parse(r.started) + 5 * MIN),
    }
  })
  const requests: RequestView[] = [
    {
      id: 950, from_run: flying.id, kind: 'question', state: 'needs_routing', message: 'Which lane owns the shared retry helper?\nIt is used by two repos.',
      depth: 0, from_agent: flying.agent, from_lane: flying.lane, repo: flying.repo, created: iso(now - 4 * MIN), updated: iso(now - 4 * MIN),
    },
  ]

  // The runs' costs add up to part of the day's $18.40; the rest is the front desk's.
  const total = historyRuns.reduce((n, r) => n + (r.cost_usd ?? 0), 0)
  const k = 15.5 / total
  for (const r of historyRuns) if (r.cost_usd !== undefined) r.cost_usd = Math.round(r.cost_usd * k * 100) / 100
  for (const r of runs) if (r.cost_usd !== undefined) r.cost_usd = historyRuns.find((h) => h.id === r.id)?.cost_usd

  return {
    src: {
      lanes, closedLanes, runs: runs.slice(-200), historyRuns, decisions, answered, requests,
      spend: { day: '2026-10-10', usd: 18.4, budget: 25, credit_usd: 0.4, by_source: {} },
    },
    now, ids,
  }
}
