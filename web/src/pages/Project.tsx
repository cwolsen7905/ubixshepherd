import { BudgetMeter } from '../components/BudgetMeter'
import { Glyph } from '../components/Glyph'
import { GateBadge } from '../components/GateBadge'
import { MrBadge } from '../components/MrBadge'
import { Work } from '../components/Work'
import { age as ageOf, duration, runCost, since, usd } from '../format'
import { useNow } from '../hooks'
import { UNASSIGNED } from '../model/projects'
import { go, href } from '../router'
import { useProject } from '../state/projects'
import type { BriefView, ProjectBrief } from '../api/types'
import '../styles/projects.css'

const NS_PER_MS = 1_000_000

function Brief({ view }: { view: BriefView }) {
  const approved = view.approved
  const pending = view.pending
  return (
    <section aria-labelledby="project-brief" className="project-section">
      <h2 id="project-brief">Brief</h2>
      <p className="faint brief-note">
        Read-only here. Only the approved text is given to agents; a draft waits until the person approves it
        through the front desk.
      </p>
      {approved ? (
        <BriefText
          brief={approved}
          heading={`Approved${view.age_text ? `: ${view.age_text.replace(/^approved /, '')}` : ''}`}
          stale={view.stale ? `Older than ${ageOf(view.max_age / NS_PER_MS)}: the focus may have moved.` : ''}
        />
      ) : (
        <p className="faint">No brief has been approved, so agents in this project are given none.</p>
      )}
      {pending && <BriefText brief={pending} heading={`Draft by ${pending.drafted_by}, not approved`} draft />}
    </section>
  )
}

function BriefText({ brief, heading, stale, draft }: { brief: ProjectBrief; heading: string; stale?: string; draft?: boolean }) {
  return (
    <div className={`brief ${draft ? 'is-draft' : ''}`}>
      <h3>
        {heading}
        {brief.approved_by && <span className="faint"> · by {brief.approved_by}</span>}
      </h3>
      {stale && <p className="tone-warn">{stale}</p>}
      <pre className="brief-text">{brief.text}</pre>
    </div>
  )
}

/** One project (or the Unassigned group, name null): the whole picture on one page. */
export function ProjectPage({ name }: { name: string | null }) {
  const { data, error, loading } = useProject(name)
  const now = useNow()
  const title = name ?? UNASSIGNED
  if (!data) {
    return (
      <div className="page">
        <header className="page-head">
          <h1>{title}</h1>
        </header>
        {error ? (
          <p className="tone-bad">
            Could not load {name === null ? 'the unassigned repos' : `project ${name}`}: {error}. <a href={href.projects()}>All projects</a>.
          </p>
        ) : (
          loading && <p className="faint">Loading…</p>
        )}
      </div>
    )
  }
  return (
    <div className="page project-page">
      <header className="page-head">
        <h1>{title}</h1>
        <p className="page-sub">
          <a href={href.projects()}>All projects</a>
          {name !== null && (
            <>
              {' · '}
              <a
                href={href.settings(`project:${name}`)}
                onClick={(e) => {
                  e.preventDefault()
                  go(href.settings(`project:${name}`))
                }}
              >
                Edit
              </a>
            </>
          )}
          {' · '}
          {data.repos.length === 0 ? 'no repos' : data.repos.join(', ')}
        </p>
        <p className="project-spend">
          <BudgetMeter budget={data.budget} today={data.spend_today} /> · <a href={href.usage()}>Usage detail</a>
        </p>
      </header>
      {error && <p className="tone-bad">Could not refresh: {error}. Showing what was last loaded.</p>}

      <section aria-labelledby="project-wait" className="project-section">
        <h2 id="project-wait">
          Waiting on you <span className="group-count">{data.pending_decisions.length + data.pending_requests.length}</span>
        </h2>
        {data.pending_decisions.length + data.pending_requests.length === 0 && <p className="faint">Nothing waits on you here.</p>}
        <ul className="rows">
          {data.pending_decisions.map((d) => (
            <li key={`d${d.id}`}>
              <a className="row project-row" href={href.decisions(d.id)} onClick={(e) => (e.preventDefault(), go(href.decisions(d.id)))}>
                <Glyph glyph="?" tone="warn" label="decision" />
                <span className="row-lane">{d.lane}</span>
                <span className="row-detail">{d.question}</span>
                <span className="row-age" title={d.created}>{since(d.created, now)}</span>
              </a>
            </li>
          ))}
          {data.pending_requests.map((q) => (
            <li key={`q${q.id}`}>
              <a className="row project-row" href={href.decisions()} onClick={(e) => (e.preventDefault(), go(href.decisions()))}>
                <Glyph glyph="!" tone="warn" label="request" />
                <span className="row-lane">{q.from_lane}</span>
                <span className="row-detail">
                  {q.state === 'needs_routing' ? 'needs routing: ' : `${q.state}: `}
                  {q.message}
                </span>
                <span className="row-age" title={q.created}>{since(q.created, now)}</span>
              </a>
            </li>
          ))}
        </ul>
      </section>

      <section aria-labelledby="project-lanes" className="project-section">
        <h2 id="project-lanes">
          Open lanes <span className="group-count">{data.open_lanes.length}</span>
        </h2>
        {data.open_lanes.length === 0 && <p className="faint">No lane is open.</p>}
        <ul className="rows">
          {data.open_lanes.map((l) => (
            <li key={l.id}>
              <a className="row project-row" href={href.lane(l.id)} onClick={(e) => (e.preventDefault(), go(href.lane(l.id)))}>
                <Glyph glyph="●" tone="accent" label="open" />
                <span className="row-lane">{l.name}</span>
                <span className="row-repo">{l.repo}</span>
                <span className="row-mr">
                  <MrBadge lane={l} link={false} />
                  {l.gate && <> <GateBadge gate={l.gate} /></>}
                </span>
                <span className="row-age" title={l.created}>{since(l.created, now)}</span>
              </a>
            </li>
          ))}
        </ul>
      </section>

      <section aria-labelledby="project-runs" className="project-section">
        <h2 id="project-runs">
          Recent runs <span className="group-count">{data.runs.length}</span>
        </h2>
        {data.runs.length === 0 && <p className="faint">No run yet.</p>}
        <ul className="rows">
          {data.runs.map((r) => (
            <li key={r.id}>
              <a className="row project-row" href={href.log(r.id)} onClick={(e) => (e.preventDefault(), go(href.log(r.id)))}>
                <Glyph glyph={r.state === 'running' ? '●' : r.state === 'succeeded' ? '✓' : '✗'} tone={r.state === 'running' ? 'accent' : r.state === 'succeeded' ? 'ok' : 'bad'} label={r.state} />
                <span className="row-lane">{r.lane}</span>
                <span className="row-agent">{r.agent}</span>
                <span className="row-detail">
                  {r.state} · {duration(r.started, r.ended, now)} · {runCost(r) || usd(0)}
                </span>
                <span className="row-age" title={r.started}>{since(r.started, now)}</span>
              </a>
            </li>
          ))}
        </ul>
      </section>

      <Work name={name} />

      {data.brief && <Brief view={data.brief} />}
    </div>
  )
}
