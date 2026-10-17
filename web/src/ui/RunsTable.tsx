import { openRow } from '../components/BacklogOpen'
import { taskLine, taskText } from '../timeline/task'
import { StatusGlyph } from './StatusGlyph'
import type { RunView } from '../api/types'
import { costNote, duration, runCost, since } from '../format'
import { useNow } from '../hooks'
import { href } from '../router'

/** Run fields both variants need. */
interface RunRow extends Pick<RunView, 'id' | 'state' | 'started' | 'ended' | 'agent' | 'model' | 'repo' | 'lane'> {
  lane_id?: number
  commits?: number
  task?: string
  parent?: number
  exit_code?: number
  outside?: string[]
  error?: string
  cost_usd?: number
  credits?: number
  cost_estimated?: boolean
}

type Variant = 'full' | 'compact' | 'list'

function glyphTone(state: RunView['state']) {
  if (state === 'succeeded') return 'ok'
  if (state === 'running') return 'live'
  if (state === 'failed' || state === 'interrupted' || state === 'stopped') return 'bad'
  return 'idle'
}

function glyphFor(state: RunView['state']) {
  if (state === 'running') return '▸'
  if (state === 'succeeded') return '✓'
  if (state === 'failed' || state === 'interrupted' || state === 'stopped') return '✗'
  return '●'
}

function runTitle(state: RunView['state'], id: number) {
  if (state === 'succeeded') return `Run ${id} succeeded`
  if (state === 'failed') return `Run ${id} failed`
  if (state === 'running') return `Run ${id} running`
  if (state === 'interrupted' || state === 'stopped') return `Run ${id} interrupted`
  return `Run ${id}`
}

function shortModel(model: string | undefined): string {
  if (!model) return ''
  const i = model.lastIndexOf('/')
  return i >= 0 ? model.slice(i + 1) : model
}

/**
 * A table of a lane's runs, newest first. `full` shows every column; `compact` drops the
 * model and cost; `list` is the Runs page layout: glyph, run #, lane over agent · model ·
 * started, result over duration, and cost (tabular numbers).
 */
export function RunsTable({ runs, variant = 'full' }: { runs: RunRow[]; variant?: Variant }) {
  const now = useNow(5000)
  if (runs.length === 0) return <p className="faint">No runs yet.</p>
  if (variant === 'list') {
    return (
      <table className="sh-runs sh-runs--list" data-component="RunsTable" data-variant={variant}>
        <thead>
          <tr>
            <th scope="col"><span className="visually-hidden">State</span></th>
            <th scope="col">Run</th>
            <th scope="col">Lane</th>
            <th scope="col">Result</th>
            <th scope="col" className="num">Cost</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.id} {...openRow({ kind: 'run', id: r.id })}>
              <td className="sh-runs-glyph">
                <StatusGlyph glyph={r.state === 'running' ? '●' : glyphFor(r.state)} tone={glyphTone(r.state)} label={r.state} />
              </td>
              <td className="sh-runs-run">
                <a href={href.log(r.id)} title={runTitle(r.state, r.id)}>{r.id}</a>
              </td>
              <td className="sh-runs-lane">
                <span className="sh-cell-trunc">
                  {r.lane_id ? (
                    <a href={href.lane(r.lane_id)} title="Open the lane">{r.repo} / {r.lane}</a>
                  ) : (
                    <>{r.repo} / {r.lane}</>
                  )}
                </span>
                <span className="sh-cell-meta">
                  {[r.agent, shortModel(r.model)].filter(Boolean).join(' · ')} · started {since(r.started, now)} ago
                </span>
                {r.task ? <span className="sh-cell-meta sh-cell-trunc" title={taskText(r.task)}>{taskLine(r.task)}</span> : null}
              </td>
              <td className="sh-runs-result">
                <span>{r.state}</span>
                {r.exit_code !== undefined && r.exit_code !== 0 && (
                  <span className="faint"> (exit {r.exit_code})</span>
                )}
                {r.outside && r.outside.length > 0 && (
                  <span className="tone-bad" title={r.outside.join(', ')}> outside scope</span>
                )}
                <span className="sh-cell-meta">
                  {duration(r.started, r.ended, now)}
                  {r.commits !== undefined && ` · ${r.commits} ${r.commits === 1 ? 'commit' : 'commits'}`}
                </span>
              </td>
              <td className="sh-runs-cost num" title={costNote(r) ?? undefined}>{runCost(r)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    )
  }
  return (
    <table className="sh-runs" data-component="RunsTable" data-variant={variant}>
      <thead>
        <tr>
          <th scope="col"><span className="visually-hidden">Outcome</span></th>
          <th scope="col">Run</th>
          <th scope="col">Agent</th>
          {variant === 'full' && <th scope="col">Model</th>}
          <th scope="col">Outcome</th>
          {variant !== 'compact' && <th scope="col" className="num">Took</th>}
          {variant !== 'compact' && <th scope="col" className="num">Cost</th>}
          <th scope="col">Started</th>
        </tr>
      </thead>
      <tbody>
        {runs.map((r) => (
          <tr key={r.id}>
            <td><StatusGlyph glyph={glyphFor(r.state)} tone={glyphTone(r.state)} label={r.state} /></td>
            <td>
              <a href={href.log(r.id)}>{r.id}</a>
              {r.parent ? <span className="faint" title={`continues run ${r.parent}`}> ↰{r.parent}</span> : null}
            </td>
            <td>{r.agent}</td>
            {variant === 'full' && <td className="faint">{r.model ?? ''}</td>}
            <td title={r.error ?? undefined}>
              {r.state}
              {r.exit_code !== undefined && r.exit_code !== 0 && <span className="faint"> (exit {r.exit_code})</span>}
              {r.outside?.length ? <span className="tone-bad" title={r.outside.join(', ')}> outside scope</span> : null}
            </td>
            {variant !== 'compact' && <td className="num">{duration(r.started, r.ended, now)}</td>}
            {variant !== 'compact' && <td className="num" title={costNote(r)}>{runCost(r)}</td>}
            <td className="faint" title={r.started}>{clockTime(r.started, now)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

import { clockTime } from '../format'
