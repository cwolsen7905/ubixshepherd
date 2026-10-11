import type { ReactNode } from 'react'
import { Glyph } from './Glyph'
import { go, href } from '../router'
import { useProjectWork } from '../state/projects'
import type { Tone } from '../model/marks'
import type { ProjectWork, WorkRow, WorkStatus } from '../api/types'

/** Doing and blocked lead; done and dropped are folded away. */
const OPEN: WorkStatus[] = ['doing', 'blocked', 'todo']
const FOLDED: WorkStatus[] = ['done', 'dropped']

const TONE: Record<WorkStatus, { glyph: string; tone: Tone }> = {
  doing: { glyph: '●', tone: 'accent' },
  blocked: { glyph: '!', tone: 'warn' },
  todo: { glyph: '○', tone: 'muted' },
  done: { glyph: '✓', tone: 'ok' },
  dropped: { glyph: '–', tone: 'muted' },
}

function Row({ row, showRepo }: { row: WorkRow; showRepo: boolean }) {
  return (
    <li className="row work-row">
      <Glyph glyph={TONE[row.status].glyph} tone={TONE[row.status].tone} label={row.status} />
      <span className="work-id">{row.id}</span>
      <span className="work-item">
        {row.item}
        {row.notes && <span className="faint"> · {row.notes}</span>}
      </span>
      <span className="work-area faint">{[showRepo ? row.repo : '', row.area].filter(Boolean).join(' · ')}</span>
      <span className="work-lane">
        {row.lane_id ? (
          <a href={href.lane(row.lane_id)} onClick={(e) => (e.preventDefault(), go(href.lane(row.lane_id as number)))}>
            {row.lane}
          </a>
        ) : (
          (row.lane ?? '')
        )}
      </span>
      <span className="work-pr">
        {row.pr ? (
          row.pr_url ? (
            <a href={row.pr_url} target="_blank" rel="noreferrer noopener">
              #{row.pr}
            </a>
          ) : (
            `#${row.pr}`
          )
        ) : null}
      </span>
    </li>
  )
}

function Group({ status, rows, showRepo, fold }: { status: WorkStatus; rows: WorkRow[]; showRepo: boolean; fold: boolean }) {
  if (rows.length === 0) return null
  const list = (
    <ul className="rows">
      {rows.map((r) => (
        <Row key={`${r.repo}:${r.id}`} row={r} showRepo={showRepo} />
      ))}
    </ul>
  )
  const title = (
    <>
      {status} <span className="group-count">{rows.length}</span>
    </>
  )
  if (fold) {
    return (
      <details className="work-group">
        <summary>{title}</summary>
        {list}
      </details>
    )
  }
  return (
    <div className="work-group">
      <h3>{title}</h3>
      {list}
    </div>
  )
}

/** The pure view of a loaded matrix, for the page and its tests. */
export function WorkView({ work }: { work: ProjectWork }): ReactNode {
  const rows = work.repos.flatMap((r) => r.rows)
  const showRepo = work.repos.length > 1
  const missing = work.drift.filter((d) => d.kind === 'no_work_file')
  const drift = work.drift.filter((d) => d.kind !== 'no_work_file')
  const warnings = work.repos.flatMap((r) => r.warnings.map((w) => ({ repo: r.repo, ...w })))
  const found = work.repos.some((r) => r.found)
  return (
    <>
      {rows.length === 0 && found && <p className="faint">No rows yet.</p>}
      {OPEN.map((s) => (
        <Group key={s} status={s} rows={rows.filter((r) => r.status === s)} showRepo={showRepo} fold={false} />
      ))}
      {FOLDED.map((s) => (
        <Group key={s} status={s} rows={rows.filter((r) => r.status === s)} showRepo={showRepo} fold />
      ))}
      {drift.length > 0 && (
        <div className="work-drift">
          <h3>Drift</h3>
          <ul>
            {drift.map((d, i) => (
              <li key={i} className="tone-warn">
                {d.lane_id ? (
                  <a href={href.lane(d.lane_id)} onClick={(e) => (e.preventDefault(), go(href.lane(d.lane_id as number)))}>
                    {d.message}
                  </a>
                ) : (
                  d.message
                )}
                {showRepo && <span className="faint"> · {d.repo}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}
      {warnings.length > 0 && (
        <div className="work-warnings">
          <h3>Could not read</h3>
          <ul>
            {warnings.map((w, i) => (
              <li key={i} className="tone-warn">
                {w.repo}
                {w.line ? `:${w.line}` : ''}: {w.reason}
                {w.text && <code className="faint"> {w.text}</code>}
              </li>
            ))}
          </ul>
        </div>
      )}
      {missing.map((d) => (
        <p key={d.repo} className="faint work-hint">
          {d.message}. Add docs/WORK.md to show its work here.
        </p>
      ))}
    </>
  )
}

/** A project's Work section: its repos' docs/WORK.md as of the base branch's last fetch. */
export function Work({ name }: { name: string | null }) {
  const { data, error, loading } = useProjectWork(name)
  return (
    <section aria-labelledby="project-work" className="project-section">
      <h2 id="project-work">
        Work{data && <span className="group-count">{data.repos.flatMap((r) => r.rows).length}</span>}
      </h2>
      {!data && loading && <p className="faint">Loading…</p>}
      {!data && error && <p className="faint">Work is not available: {error}.</p>}
      {data && <WorkView work={data} />}
    </section>
  )
}
