import { BudgetMeter } from '../components/BudgetMeter'
import { mrBadge, MrBadge } from '../components/MrBadge'
import { GateBadge } from '../components/GateBadge'
import { Work } from '../components/Work'
import { age as ageOf, duration, runCost, since, usd } from '../format'
import { useKeys, useNow } from '../hooks'
import { UNASSIGNED } from '../model/projects'
import { go, href } from '../router'
import { isOpenNow, openHref, useHashOpen } from '../components/BacklogOpen'
import { useProject } from '../state/projects'
import type { BriefView, LaneView, ProjectBrief } from '../api/types'
import { Count, EmptyState, GroupHeader, ListRow, Skeleton } from '../ui'
import './Projects.css'

const NS_PER_MS = 1_000_000

function Brief({ view }: { view: BriefView }) {
  const approved = view.approved
  const pending = view.pending
  return (
    <section aria-labelledby="project-brief">
      <GroupHeader variant="section" label={<span id="project-brief">Brief</span>} hint="Read-only here. Only the approved text is given to agents; a draft waits until the person approves it through the front desk." />
      {approved ? (
        <BriefText
          brief={approved}
          heading={`Approved${view.age_text ? `: ${view.age_text.replace(/^approved /, '')}` : ''}`}
          stale={view.stale ? `Older than ${ageOf(view.max_age / NS_PER_MS)}: the focus may have moved.` : ''}
        />
      ) : (
        <EmptyState title="No brief has been approved">Agents in this project are given none.</EmptyState>
      )}
      {pending && <BriefText brief={pending} heading={`Draft by ${pending.drafted_by}, not approved`} draft />}
    </section>
  )
}

function BriefText({ brief, heading, stale, draft }: { brief: ProjectBrief; heading: string; stale?: string; draft?: boolean }) {
  return (
    <div className={`sh-brief${draft ? ' is-draft' : ''}`}>
      <h3>
        {heading}
        {brief.approved_by && <span className="faint"> · by {brief.approved_by}</span>}
      </h3>
      {stale && <p className="tone-warn">{stale}</p>}
      <pre className="sh-brief-text">{brief.text}</pre>
    </div>
  )
}

/** A lane row: the lane over its repo and gate, the merge request at the right. A link to
 * the inspector address, so middle-click and open-in-new-tab keep working. */
function LaneRow({ lane, now }: { lane: LaneView; now: number }) {
  const open = { kind: 'lane' as const, id: lane.id }
  const badge = mrBadge(lane)
  return (
    <ListRow
      variant="lane"
      glyph="●"
      tone="live"
      glyphLabel="open"
      title={lane.name}
      sub={
        <>
          {lane.repo}
          {lane.gate && <> · <GateBadge gate={lane.gate} now={now} /></>}
        </>
      }
      right={badge ? <MrBadge lane={lane} link={false} /> : undefined}
      rightTone={badge?.tone === 'ok' ? 'ok' : badge?.tone === 'bad' ? 'bad' : 'muted'}
      age={since(lane.created, now)}
      selected={isOpenNow(open)}
      href={openHref(open)}
    />
  )
}

/** One project (or the Unassigned group, name null): brief, budget, repos and lanes. */
export function ProjectPage({ name }: { name: string | null }) {
  const { data, error, loading } = useProject(name)
  const now = useNow()
  useHashOpen()
  const title = name ?? UNASSIGNED
  useKeys(
    {
      // Each press moves to the next waiting row after the focused one, wrapping round.
      ']': () => {
        const rows = [...document.querySelectorAll<HTMLElement>('[data-waiting] [data-component="ListRow"]')]
        if (rows.length === 0) return
        const at = rows.findIndex((el) => el === document.activeElement || el.contains(document.activeElement))
        rows[(at + 1) % rows.length]?.focus()
      },
    },
    [data],
  )
  if (!data) {
    return (
      <div className="page sh-project-page">
        <header className="page-head">
          <h1>{title}</h1>
        </header>
        {error ? (
          <p className="tone-bad">
            Could not load {name === null ? 'the unassigned repos' : `project ${name}`}: {error}. <a href={href.projects()}>All projects</a>.
          </p>
        ) : (
          loading && <Skeleton lines={3} />
        )}
      </div>
    )
  }
  const waiting = data.pending_decisions.length + data.pending_requests.length
  return (
    <div className="page sh-project-page">
      <header className="page-head">
        <h1>{title}</h1>
        <span className="page-head-repos">{data.repos.length === 0 ? 'no repos' : data.repos.join(', ')}</span>
        <div className="page-actions">
          <span className="project-spend">
            <BudgetMeter budget={data.budget} today={data.spend_today} /> · <a href={href.usage()}>Usage detail</a>
          </span>
          <a href={href.projects()}>All projects</a>
          {name !== null && (
            <a
              className="sh-btn sh-btn--sm ui-btn ui-btn-sm"
              href={href.settings(`project:${name}`)}
              onClick={(e) => {
                e.preventDefault()
                go(href.settings(`project:${name}`))
              }}
            >
              Edit
            </a>
          )}
        </div>
      </header>
      {error && <p className="tone-bad">Could not refresh: {error}. Showing what was last loaded.</p>}

      <section aria-labelledby="project-wait" data-waiting={waiting > 0 ? true : undefined}>
        <GroupHeader
          variant="section"
          warn={waiting > 0}
          label={<span id="project-wait">Waiting on you</span>}
          hint={<Count count={waiting} tone="attention" label="waiting on you" />}
        />
        {waiting === 0 && <EmptyState title="Nothing waits on you here." />}
        {data.pending_decisions.map((d) => {
          const open = { kind: 'decision' as const, id: d.id }
          return (
            <ListRow
              key={`d${d.id}`}
              glyph="?"
              tone="warn"
              glyphLabel="decision"
              title={d.question}
              sub={`${d.lane} · ${d.agent}`}
              age={since(d.created, now)}
              selected={isOpenNow(open)}
              href={openHref(open)}
            />
          )
        })}
        {data.pending_requests.map((q) => (
          <ListRow
            key={`q${q.id}`}
            glyph="!"
            tone="warn"
            glyphLabel="request"
            title={q.state === 'needs_routing' ? 'needs routing' : q.state}
            sub={`${q.from_lane} · ${q.message}`}
            age={since(q.created, now)}
            href={href.decisions()}
          />
        ))}
      </section>

      <section aria-labelledby="project-lanes">
        <GroupHeader
          variant="section"
          label={<span id="project-lanes">Open lanes</span>}
          hint={<Count count={data.open_lanes.length} label="open lanes" />}
        />
        {data.open_lanes.length === 0 && <EmptyState title="No lane is open." />}
        {data.open_lanes.map((l) => (
          <LaneRow key={l.id} lane={l} now={now} />
        ))}
      </section>

      <section aria-labelledby="project-runs">
        <GroupHeader
          variant="section"
          label={<span id="project-runs">Recent runs</span>}
          hint={<Count count={data.runs.length} label="recent runs" />}
        />
        {data.runs.length === 0 && <EmptyState title="No run yet." />}
        {data.runs.map((r) => {
          const open = { kind: 'run' as const, id: r.id }
          return (
            <ListRow
              key={r.id}
              glyph={r.state === 'running' ? '●' : r.state === 'succeeded' ? '✓' : r.state === 'failed' ? '✗' : '○'}
              tone={r.state === 'running' ? 'live' : r.state === 'succeeded' ? 'ok' : r.state === 'failed' ? 'bad' : 'idle'}
              glyphLabel={r.state}
              title={r.lane}
              sub={`${r.agent}${r.model ? ` ${r.model.replace(/^.*\//, '')}` : ''} · ${r.state} · ${duration(r.started, r.ended, now)} · ${runCost(r) || usd(0)}`}
              age={since(r.started, now)}
              selected={isOpenNow(open)}
              href={openHref(open)}
            />
          )
        })}
      </section>

      <Work name={name} />

      {data.brief && <Brief view={data.brief} />}
    </div>
  )
}
