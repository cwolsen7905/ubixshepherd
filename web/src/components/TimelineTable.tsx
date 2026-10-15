import { useMemo, useState } from 'react'
import { clockTime, duration, ESTIMATE_NOTE, runCost } from '../format'
import { EventGlyph } from './Glyph'
import { href } from '../router'
import type { TRun } from '../model/timeline'
import { taskLine, taskText } from '../timeline/task'
import '../styles/pages/runs.css'

type Key = 'id' | 'agent' | 'lane' | 'state' | 'commits' | 'cost' | 'started' | 'took'

interface Row {
  run: TRun
  repo: string
  lane: string
}

const STATE_EVENT: Record<TRun['state'], string> = {
  running: 'run_started', succeeded: 'run_passed', failed: 'run_failed', interrupted: 'run_interrupted', stopped: 'run_interrupted',
}

/** The columns that read best from A to Z; the rest start with the most. */
const TEXT: Key[] = ['agent', 'lane', 'state']

const COLUMNS: { key: Key; label: string; num?: boolean }[] = [
  { key: 'id', label: 'Run' },
  { key: 'agent', label: 'Agent' },
  { key: 'lane', label: 'Lane' },
  { key: 'state', label: 'Outcome' },
  { key: 'commits', label: 'Commits', num: true },
  { key: 'cost', label: 'Cost', num: true },
  { key: 'started', label: 'Started' },
  { key: 'took', label: 'Took', num: true },
]

function value(r: Row, key: Key, now: number): string | number {
  switch (key) {
    case 'id': return r.run.id
    case 'agent': return r.run.agent
    case 'lane': return `${r.repo}/${r.lane}`
    case 'state': return r.run.state
    case 'commits': return r.run.commits
    case 'cost': return r.run.costUsd ?? 0
    case 'started': return r.run.started
    case 'took': return (r.run.ended ?? now) - r.run.started
  }
}

/** The same runs as the chart, as a list that sorts: nothing on the page is reachable only by hovering. */
export function TimelineTable({ rows, now }: { rows: Row[]; now: number }) {
  const [sort, setSort] = useState<{ key: Key; desc: boolean }>({ key: 'started', desc: true })
  const sorted = useMemo(() => {
    const out = [...rows].sort((a, b) => {
      const x = value(a, sort.key, now)
      const y = value(b, sort.key, now)
      const c = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y))
      return c || a.run.id - b.run.id
    })
    return sort.desc ? out.reverse() : out
  }, [rows, sort, now])
  const click = (key: Key) => setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: !TEXT.includes(key) }))
  const iso = (ms: number) => new Date(ms).toISOString()

  return (
    <table className="runs runs-table tl-table-list">
      <caption className="visually-hidden">Runs in the timeline’s range</caption>
      <thead>
        <tr>
          <th scope="col"><span className="visually-hidden">State</span></th>
          {COLUMNS.map((c) => (
            <th key={c.key} scope="col" className={c.num ? 'num' : undefined} aria-sort={sort.key !== c.key ? undefined : sort.desc ? 'descending' : 'ascending'}>
              <button type="button" className="tl-sort" onClick={() => click(c.key)}>
                {c.label}
                <span aria-hidden="true">{sort.key === c.key ? (sort.desc ? ' ↓' : ' ↑') : ''}</span>
              </button>
            </th>
          ))}
          <th scope="col">Task</th>
        </tr>
      </thead>
      <tbody>
        {sorted.map(({ run, repo, lane }) => (
          <tr key={run.id}>
            <td><EventGlyph event={STATE_EVENT[run.state]} /></td>
            <td><a href={href.log(run.id)} title="Open the log">{run.id}</a></td>
            <td>{run.agent}</td>
            <td className="run-lane">
              <a href={href.lane(run.laneId)} title="Open the lane"><span className="faint">{repo} /</span> {lane}</a>
            </td>
            <td>{run.state}</td>
            <td className="num">{run.commits}</td>
            <td className="num" title={run.costEstimated ? ESTIMATE_NOTE : undefined}>
              {runCost({ cost_usd: run.costUsd, credits: run.credits, cost_estimated: run.costEstimated, agent: run.agent })}
            </td>
            <td className="faint" title={iso(run.started)}>{clockTime(iso(run.started), now)}</td>
            <td className="num">{duration(iso(run.started), run.ended !== undefined ? iso(run.ended) : undefined, now)}</td>
            <td className="run-task" title={taskText(run.task)}>{taskLine(run.task)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
