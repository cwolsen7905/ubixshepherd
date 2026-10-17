import { useEffect, useState } from 'react'
import { api, history } from '../api/client'
import type { LaneView, RequestView, RunView } from '../api/types'
import { BackLink } from '../components/BackLink'
import { boardItems } from '../model/board'
import { useNow } from '../hooks'
import { useLive, useSnapshot } from '../state/context'
import { LaneDetail, type LaneDetailTab } from '../ui/LaneDetail'
import { laneSource } from '../conversation/api'
import '../styles/conversation.css'

interface LaneData {
  runs: RunView[]
  requests: RequestView[]
}

export const PROGRESS_POLL = 5000

/** `conversation` replaces the daemon's reader; tests give it a fake. */
export function LanePage({ id, conversation }: { id: number; conversation?: (laneId: number) => import('../conversation/types').ConversationSource }) {
  const [tab, setTab] = useState<LaneDetailTab>('overview')
  const snap = useSnapshot()
  const live = useLive()
  const now = useNow()
  const [data, setData] = useState<LaneData | null>(null)
  const [reload, setReload] = useState(0)
  const feedLast = snap.feed.at(-1)?.id ?? 0

  const listed = snap.lanes.find((l) => l.id === id)
  const [record, setRecord] = useState<import('../api/types').LaneRecord | null>(null)
  const unlisted = listed === undefined
  useEffect(() => {
    if (!unlisted) return
    let current = true
    history.lane(id).then(
      (rs) => current && setRecord(rs[0] ?? null),
      () => {},
    )
    return () => {
      current = false
    }
  }, [id, unlisted, feedLast])
  const lane: LaneView | undefined = listed ?? record ?? (data?.runs[0] ? closedLane(id, data.runs[0]) : undefined)

  useEffect(() => {
    let current = true
    Promise.all([api.runs(id, 200), api.requests('')]).then(
      ([runs, requests]) => {
        if (!current) return
        setData({ runs, requests })
      },
      (e: unknown) => current && console.error(e),
    )
    return () => {
      current = false
    }
  }, [id, feedLast, reload])

  const going = data?.runs[0]?.state === 'running'
  useEffect(() => {
    if (!going) return
    const t = window.setInterval(() => setReload((n) => n + 1), PROGRESS_POLL)
    return () => window.clearInterval(t)
  }, [going])

  const latest = data?.runs[0]
  useEffect(() => {
    if (latest?.ended) live.markSeen(`run:${latest.id}`)
    if (lane?.closed) live.markSeen(`lane:${lane.id}`)
  }, [live, latest?.id, latest?.ended, lane?.id, lane?.closed])

  useEffect(() => {
    if (lane && lane.state !== 'closed') {
      const group = boardItems({ ...snap, lanes: [lane], decisions: [], requests: [] })[0]?.group
      setTab(group === 'review' ? 'changes' : 'overview')
    }
  }, [lane?.id, snap])

  if (!lane) {
    const known = snap.ready && data
    return (
      <>
        <p className="empty">{known ? <>No open lane {id}, and no runs to show for it.</> : 'Loading…'}</p>
        {known && <BackLink />}
      </>
    )
  }

  return (
    <div className="page lane">
      <BackLink />
      <LaneDetail
        lane={lane}
        runs={data?.runs ?? []}
        now={now}
        tab={tab}
        onTab={setTab}
        feedLast={feedLast}
        conversation={conversation ?? laneSource}
        full
      />
    </div>
  )
}

function closedLane(id: number, r: RunView): LaneView {
  return {
    id, repo_id: 0, name: r.lane, repo: r.repo, branch: r.lane, base: '', worktree: r.worktree,
    scope: [], state: 'closed', created: r.started, origin: {},
  }
}
