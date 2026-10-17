import { go, href, withOpen, type Open } from '../router'
import { useSnapshot } from '../state/context'
import { useNow } from '../hooks'
import { since } from '../format'
import { LaneDetail, laneDetailTab, defaultLaneDetailTab, type LaneDetailTab } from '../ui/LaneDetail'
import { rememberedTab } from '../ui/LaneDetailMemory'
import type { ConversationSource } from '../conversation/types'
import { PageHeader, type PageChrome } from '../ui/PageHeader'

export const LANE_TABS = ['overview', 'changes', 'artifacts', 'conversation', 'runs', 'files'] as const
export type LaneTab = (typeof LANE_TABS)[number]

/** The tab in an `open=lane:4/changes` address; anything else is the overview. */
export function laneTab(tab: string | undefined): LaneTab {
  return (LANE_TABS as readonly string[]).includes(tab ?? '') ? (tab as LaneTab) : 'overview'
}

/**
 * A lane in the inspector. The tab lives in the address (`open=lane:4/changes`), so Back and a
 * shared link return to it. The full page (`#/lanes/4`) is its "open as page" form.
 */
export function LaneInspector({ open, conversation, chrome }: { open: Open; conversation?: (laneId: number) => ConversationSource; chrome?: PageChrome }) {
  const snap = useSnapshot()
  const now = useNow()
  const lane = snap.lanes.find((l) => l.id === open.id)
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const runs = snap.runs.filter((r) => r.lane_id === open.id).sort((a, b) => b.id - a.id)

  const select = (t: LaneDetailTab) =>
    go(withOpen(window.location.hash, { kind: 'lane', id: open.id, tab: t, ...(open.full ? { full: true } : {}) }))

  if (!lane) {
    return (
      <div className="inspector-body">
        {chrome && <PageHeader {...chrome} title={`Lane ${open.id}`} />}
        <p className="faint">{snap.ready ? `No open lane ${open.id}.` : 'Loading…'}</p>
        <p><a href={href.lane(open.id)}>Open as page</a></p>
      </div>
    )
  }

  // The address wins; then the tab the person last had on this lane (the page's too); then the state's own.
  const effective = open.tab ? laneDetailTab(open.tab) : (rememberedTab(lane.id) ?? defaultLaneDetailTab(lane, runs))

  return (
    <div className="inspector-body lane-inspector">
      <LaneDetail
        lane={lane}
        runs={runs}
        now={now}
        tab={effective}
        onTab={select}
        feedLast={feedLast}
        conversation={conversation}
        full={!!chrome}
        chrome={chrome}
      />
    </div>
  )
}

/** A lane's runs, newest first; each opens in the inspector. */
export function LaneRuns({ laneId }: { laneId: number }) {
  const snap = useSnapshot()
  const now = useNow()
  const runs = snap.runs.filter((r) => r.lane_id === laneId).sort((a, b) => b.id - a.id)
  if (runs.length === 0) return <p className="faint">This lane has not run an agent yet.</p>
  return (
    <ul className="lane-runs">
      {runs.map((r) => (
        <li key={r.id}>
          <a href={withOpen(window.location.hash, { kind: 'run', id: r.id })}>Run {r.id}</a>{' '}
          <span className="faint">
            {r.agent} · {r.state} · {since(r.started, now)} ago
          </span>
        </li>
      ))}
    </ul>
  )
}
