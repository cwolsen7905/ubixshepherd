import { useMemo } from 'react'
import { LaneArtifactsPanel } from '../artifacts'
import { Changes } from '../components/Changes'
import { LaneApprove } from '../components/LaneApprove'
import { GateBadge } from '../components/GateBadge'
import { MrBadge } from '../components/MrBadge'
import { laneSource } from '../conversation/api'
import { Conversation } from '../conversation/Conversation'
import type { ConversationSource } from '../conversation/types'
import { since } from '../format'
import { useNow } from '../hooks'
import { go, href, withOpen, type Open } from '../router'
import { useSnapshot } from '../state/context'
import { Tabs } from '../ui'
import { tabLabel, useLaneCounts } from './laneCounts'
import '../styles/conversation.css'

export const LANE_TABS = ['overview', 'conversation', 'changes', 'artifacts', 'runs'] as const
export type LaneTab = (typeof LANE_TABS)[number]

/** The tab in an `open=lane:4/changes` address; anything else is the overview. */
export function laneTab(tab: string | undefined): LaneTab {
  return (LANE_TABS as readonly string[]).includes(tab ?? '') ? (tab as LaneTab) : 'overview'
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

/**
 * A lane in the inspector. The tab lives in the address (`open=lane:4/changes`), so Back and a
 * shared link return to it. The full page (`#/lanes/4`) is its "open as page" form.
 */
export function LaneInspector({ open, conversation }: { open: Open; conversation?: (laneId: number) => ConversationSource }) {
  const snap = useSnapshot()
  const now = useNow()
  const tab = laneTab(open.tab)
  const lane = snap.lanes.find((l) => l.id === open.id)
  const source = useMemo(() => (conversation ?? laneSource)(open.id), [conversation, open.id])
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const counts = useLaneCounts(open.id, feedLast)
  const runCount = snap.runs.filter((r) => r.lane_id === open.id).length

  const select = (t: string) =>
    go(withOpen(window.location.hash, { kind: 'lane', id: open.id, ...(t === 'overview' ? {} : { tab: t }), ...(open.full ? { full: true } : {}) }))

  if (!lane) {
    return (
      <div className="inspector-body">
        <p className="faint">{snap.ready ? `No open lane ${open.id}.` : 'Loading…'}</p>
        <p><a href={href.lane(open.id)}>Open as page</a></p>
      </div>
    )
  }

  const items = [
    {
      id: 'overview',
      label: 'Overview',
      content: (
        <dl className="lane-facts">
          <dt>Repo</dt>
          <dd>{lane.repo}</dd>
          <dt>Branch</dt>
          <dd>
            <code>{lane.branch}</code>
            {lane.base && <> onto <code>{lane.base}</code></>}
          </dd>
          <dt>Opened</dt>
          <dd>{since(lane.created, now)} ago</dd>
          <dt>Merge request</dt>
          <dd>{lane.mr ? <><MrBadge lane={lane} /> <span className="faint">{lane.mr_state}</span></> : <span className="faint">None yet</span>}</dd>
          {lane.gate && (
            <>
              <dt>Gate</dt>
              <dd><GateBadge gate={lane.gate} now={now} /></dd>
            </>
          )}
          <dt>Scope</dt>
          <dd>
            <ul className="scope">
              {lane.scope.map((s) => (
                <li key={s}><code>{s}</code></li>
              ))}
            </ul>
          </dd>
        </dl>
      ),
    },
    { id: 'conversation', label: 'Conversation', content: <Conversation source={source} refreshKey={feedLast} now={now} /> },
    {
      id: 'changes',
      label: tabLabel('Changes', counts.changes),
      content: (
        <>
          {lane.state === 'open' && <LaneApprove laneId={lane.id} refreshKey={feedLast} />}
          <Changes laneId={lane.id} worktree={lane.worktree} editorUrl={null} refreshKey={feedLast} now={now} />
        </>
      ),
    },
    { id: 'artifacts', label: tabLabel('Artifacts', counts.artifacts), content: <LaneArtifactsPanel laneId={lane.id} refreshKey={feedLast} /> },
    { id: 'runs', label: tabLabel('Runs', runCount), content: <LaneRuns laneId={lane.id} /> },
  ]

  return (
    <div className="inspector-body lane-inspector">
      <Tabs items={items} value={tab} onChange={select} label="Lane views" />
    </div>
  )
}
