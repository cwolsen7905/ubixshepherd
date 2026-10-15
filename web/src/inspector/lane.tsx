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
import '../styles/conversation.css'

export const LANE_TABS = ['overview', 'conversation', 'changes', 'artifacts'] as const
export type LaneTab = (typeof LANE_TABS)[number]

const TAB_NAMES: Record<LaneTab, string> = { overview: 'Overview', conversation: 'Conversation', changes: 'Changes', artifacts: 'Artifacts' }

/** The tab in an `open=lane:4/changes` address; anything else is the overview. */
export function laneTab(tab: string | undefined): LaneTab {
  return (LANE_TABS as readonly string[]).includes(tab ?? '') ? (tab as LaneTab) : 'overview'
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

  const select = (t: LaneTab) => go(withOpen(window.location.hash, { kind: 'lane', id: open.id, ...(t === 'overview' ? {} : { tab: t }) }))

  if (!lane) {
    return (
      <div className="inspector-body">
        <p className="faint">{snap.ready ? `No open lane ${open.id}.` : 'Loading…'}</p>
        <p><a href={href.lane(open.id)}>Open as page</a></p>
      </div>
    )
  }

  return (
    <div className="inspector-body lane-inspector">
      <p className="lane-sub">
        <strong>{lane.name}</strong> <span className="faint">{lane.repo}</span>{' '}
        <a href={href.lane(lane.id)}>Open as page</a>
      </p>
      <div className="lane-tabs" role="tablist" aria-label="Lane views">
        {LANE_TABS.map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            id={`lane-inspector-tab-${t}`}
            className="lane-tab"
            aria-selected={tab === t}
            aria-controls="lane-inspector-panel"
            onClick={() => select(t)}
          >
            {TAB_NAMES[t]}
          </button>
        ))}
      </div>
      <div id="lane-inspector-panel" role="tabpanel" aria-labelledby={`lane-inspector-tab-${tab}`}>
        {tab === 'overview' && (
          <dl className="lane-facts">
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
        )}
        {tab === 'conversation' && <Conversation source={source} refreshKey={feedLast} now={now} />}
        {tab === 'changes' && (
          <>
            <LaneApprove laneId={lane.id} refreshKey={feedLast} />
            <Changes laneId={lane.id} worktree={lane.worktree} editorUrl={null} refreshKey={feedLast} now={now} />
          </>
        )}
        {tab === 'artifacts' && <LaneArtifactsPanel laneId={lane.id} refreshKey={feedLast} />}
      </div>
    </div>
  )
}
