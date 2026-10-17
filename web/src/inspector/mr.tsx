import { useMemo } from 'react'
import { LaneArtifactsPanel } from '../artifacts'
import { Changes } from '../components/Changes'
import { LaneApprove } from '../components/LaneApprove'
import { laneSource } from '../conversation/api'
import { Conversation } from '../conversation/Conversation'
import { useNow } from '../hooks'
import { go, href, withOpen, type Open } from '../router'
import { useSnapshot } from '../state/context'
import { Tabs } from '../ui'
import { LaneRuns } from './lane'
import { tabLabel, useLaneCounts } from './laneCounts'

export const MR_TABS = ['approval', 'changes', 'artifacts', 'conversation', 'runs'] as const
export type MrTab = (typeof MR_TABS)[number]

/** The tab in an `open=mr:4/changes` address; anything else is the approval. */
export function mrTab(tab: string | undefined): MrTab {
  return (MR_TABS as readonly string[]).includes(tab ?? '') ? (tab as MrTab) : 'approval'
}

/**
 * A lane's merge request in the inspector: `open=mr:N` is the merge request of lane N, as the
 * forge has it. The Approval tab holds the person's own Approve (the same action the lane's
 * Changes tab has); the other tabs are the lane's, so the merge request can be judged here.
 */
export function MrInspector({ open }: { open: Open }) {
  const snap = useSnapshot()
  const now = useNow()
  const tab = mrTab(open.tab)
  const lane = snap.lanes.find((l) => l.id === open.id)
  const source = useMemo(() => laneSource(open.id), [open.id])
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const counts = useLaneCounts(open.id, feedLast)
  const runCount = snap.runs.filter((r) => r.lane_id === open.id).length

  const select = (t: string) =>
    go(withOpen(window.location.hash, { kind: 'mr', id: open.id, ...(t === 'approval' ? {} : { tab: t }), ...(open.full ? { full: true } : {}) }))

  if (!lane) {
    return (
      <div className="inspector-body">
        <p className="faint">{snap.ready ? `No open lane ${open.id}, so no merge request.` : 'Loading…'}</p>
        <p><a href={href.lane(open.id)}>Open the lane</a></p>
      </div>
    )
  }

  const items = [
    { id: 'approval', label: 'Approval', content: <LaneApprove laneId={lane.id} refreshKey={feedLast} /> },
    {
      id: 'changes',
      label: tabLabel('Changes', counts.changes),
      content: <Changes laneId={lane.id} worktree={lane.worktree} editorUrl={null} refreshKey={feedLast} now={now} />,
    },
    { id: 'artifacts', label: tabLabel('Artifacts', counts.artifacts), content: <LaneArtifactsPanel laneId={lane.id} refreshKey={feedLast} /> },
    { id: 'conversation', label: 'Conversation', content: <Conversation source={source} refreshKey={feedLast} now={now} /> },
    { id: 'runs', label: tabLabel('Runs', runCount), content: <LaneRuns laneId={lane.id} /> },
  ]

  return (
    <div className="inspector-body mr-inspector">
      <Tabs items={items} value={tab} onChange={select} label="Merge request views" />
    </div>
  )
}
