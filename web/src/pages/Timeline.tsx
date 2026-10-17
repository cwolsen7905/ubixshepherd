import { useMemo, useState, type ReactNode } from 'react'
import { TimelineChart, useNarrow } from '../components/Timeline'
import { TimelineLegend } from '../components/TimelineLegend'
import { TimelineTable } from '../components/TimelineTable'
import { useNow } from '../hooks'
import {
  agentsIn, assemble, DEFAULT_TIMELINE_RANGE, overlaps, summarise, summaryText, tableRows, TIMELINE_PHRASE, TIMELINE_RANGES, windowFor,
} from '../model/timeline'
import { go, href, type TimelineRange } from '../router'
import { useSnapshot } from '../state/context'
import { Button } from '../ui'
import { useTimelineHistory } from '../timeline/data'
import './Timeline.css'

/** How often the window is recomputed: running bars move on their own between these. */
export const WINDOW_REFRESH = 15_000

const EMPTY: Record<TimelineRange, ReactNode> = {
  today: <>No run started today. Start one with <code>shepherd lane run</code>, or ask the front desk.</>,
  '24h': <>No run started in the last 24 hours. Start one with <code>shepherd lane run</code>, or ask the front desk.</>,
  week: <>No run started in the last 7 days. Start one with <code>shepherd lane run</code>, or ask the front desk.</>,
}

/**
 * The whole day's work at once: a track per lane, grouped by repo, a bar per run coloured
 * by its agent. Built from the live snapshot and the history reads (docs/swarm-timeline.md).
 */
export function TimelinePage({ range = DEFAULT_TIMELINE_RANGE, agent = '' }: { range?: TimelineRange; agent?: string }) {
  const snap = useSnapshot()
  const now = useNow(WINDOW_REFRESH)
  const narrow = useNarrow()
  const hist = useTimelineHistory(range)
  const [showIdle, setShowIdle] = useState(false)

  const h = hist.data
  const data = useMemo(
    () =>
      assemble({
        lanes: snap.lanes, closedLanes: h?.closedLanes, runs: snap.runs, historyRuns: h?.runs,
        decisions: snap.decisions, answered: h?.answered, requests: snap.requests, spend: snap.spend,
      }),
    [snap.lanes, snap.runs, snap.decisions, snap.requests, snap.spend, h],
  )
  const win = useMemo(() => windowFor(range, now), [range, now])
  const summary = useMemo(() => summarise(data, win), [data, win])
  const agents = useMemo(() => agentsIn(data), [data])
  const rows = useMemo(() => tableRows(data, win).filter((r) => !agent || r.run.agent === agent), [data, win, agent])
  const started = useMemo(() => data.runs.some((r) => overlaps(r, win)), [data, win])
  const label = TIMELINE_RANGES.find((r) => r.id === range)?.label ?? ''
  const line = summaryText(summary, range)
  const cut = h && h.count > h.runs.length ? { shown: h.runs.length, total: h.count } : null
  const stale = !!snap.error

  return (
    <div className={`timeline-page${stale ? ' is-stale' : ''}`} data-component="Timeline">
      <header className="sh-head tl-page-head">
        <div>
          <h1>Timeline</h1>
          <p className="tl-page-sub" aria-live="polite">
            {snap.ready ? line || <span className="faint">Nothing running, waiting or merged.</span> : <span className="faint">Loading…</span>}
          </p>
        </div>
        <div className="sh-head-right">
          {summary.idle > 0 && (
            <Button variant="ghost" size="small" onClick={() => setShowIdle((v) => !v)}>
              {showIdle ? 'Hide' : 'Show'} idle lanes ({summary.idle})
            </Button>
          )}
        </div>
      </header>

      <div className="tl-controls">
        {narrow ? (
          <label>
            <span className="faint">Range </span>
            <select className="tl-select" value={range} onChange={(e) => go(href.timeline({ range: e.target.value as TimelineRange, agent: agent || undefined }))}>
              {TIMELINE_RANGES.map((r) => (
                <option key={r.id} value={r.id}>{r.label}</option>
              ))}
            </select>
          </label>
        ) : (
          <nav className="sh-segmented" aria-label="Time range">
            {TIMELINE_RANGES.map((r) => (
              <a key={r.id} href={href.timeline({ range: r.id, agent: agent || undefined })} aria-current={r.id === range ? 'true' : undefined}>
                {r.label}
              </a>
            ))}
          </nav>
        )}
        <TimelineLegend agents={agents} agent={agent} range={range} />
        <span className="tl-key-glyphs" aria-label="Marks on the tracks">
          <span><b className="is-lantern">?</b> waiting on you</span>
          <span><b className="is-ok">◆</b> merged</span>
          <span><b className="is-broken">✗</b> failed</span>
          <span><b className="is-ok">✓</b> passed</span>
          <span><b className="is-broken">⌀</b> out of quota</span>
        </span>
      </div>

      {hist.error && <p className="tone-bad">Could not read the history: {hist.error}. Showing what the daemon last sent.</p>}

      <TimelineChart
        data={data}
        range={range}
        now={now}
        agent={agent}
        showIdle={showIdle}
        empty={started || !snap.ready ? undefined : EMPTY[range]}
      />

      {cut && (
        <p className="faint tl-note">
          Showing the newest {cut.shown.toLocaleString('en-US')} runs of {cut.total.toLocaleString('en-US')} {TIMELINE_PHRASE[range]}.
        </p>
      )}

      <details className="tl-table">
        <summary><span className="tl-table-mark" aria-hidden="true">▸</span>View as table</summary>
        {rows.length === 0 ? (
          <p className="faint">{agent ? `No ${agent} run` : 'No run'} in {label.toLowerCase()}.</p>
        ) : (
          <TimelineTable rows={rows} now={now} />
        )}
      </details>
    </div>
  )
}
