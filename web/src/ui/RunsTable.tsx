import { StatusGlyph } from './StatusGlyph'
import type { RunView } from '../api/types'
import { clockTime, costNote, duration, runCost, since } from '../format'
import { useNow } from '../hooks'
import { href } from '../router'

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

/**
 * A table of a lane's runs, newest first. `full` shows every column; `compact` drops the
 * model and cost; `list` is a single-line row for use inside a panel.
 */
export function RunsTable({ runs, variant = 'full' }: { runs: RunView[]; variant?: Variant }) {
  const now = useNow(5000)
  if (runs.length === 0) return <p className="faint">No runs yet.</p>
  if (variant === 'list') {
    return (
      <ul className="sh-run-list" data-component="RunsTable" data-variant={variant}>
        {runs.map((r) => (
          <li key={r.id}>
            <a href={href.log(r.id)}>
              <StatusGlyph glyph={glyphFor(r.state)} tone={glyphTone(r.state)} label={r.state} />
              Run {r.id}
            </a>{' '}
            <span className="faint">{r.agent} · {since(r.started, now)} ago</span>
          </li>
        ))}
      </ul>
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
