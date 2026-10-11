// What the timeline page reads that the snapshot does not hold: the history of runs and of
// closed lanes since the start of the window, and the decisions already answered. It is
// reloaded when the feed moves, but never more than once in FETCH_GAP, so a busy feed does
// not refetch on every commit.
import { useEffect, useRef, useState } from 'react'
import { api, history } from '../api/client'
import type { DecisionView, LaneRecord, RunRecord } from '../api/types'
import { fetchSince, windowFor } from '../model/timeline'
import type { TimelineRange } from '../router'
import { useSnapshot } from '../state/context'

/** The shortest time between two history reads. */
export const FETCH_GAP = 5000

/** The most runs asked of the daemon, per workspace: its limit. */
export const RUN_LIMIT = 1000

export interface TimelineHistory {
  runs: RunRecord[]
  /** Runs the daemon counted in the range, which may be more than it listed. */
  count: number
  closedLanes: LaneRecord[]
  answered: DecisionView[]
}

export interface Loaded {
  data: TimelineHistory | null
  error: string | null
}

async function load(ids: number[], range: TimelineRange): Promise<TimelineHistory> {
  const since = new Date(fetchSince(windowFor(range, Date.now()).from)).toISOString()
  const [runs, lanes, answered] = await Promise.all([
    Promise.all(ids.map((id) => history.runs(id, since, '', 0, RUN_LIMIT))),
    Promise.all(ids.map((id) => history.lanes(id, since))),
    // Not bounded by time yet: the daemon has no filter, so the page keeps what is in range.
    api.decisions('answered').catch(() => [] as DecisionView[]),
  ])
  return {
    runs: runs.flatMap((r) => r.runs),
    count: runs.reduce((n, r) => n + r.count, 0),
    closedLanes: lanes.flat(),
    answered,
  }
}

/**
 * The history for a range, for the workspaces the daemon lists. A range's answer is not
 * another's: a changed range shows nothing until its own arrives. A failed read keeps the
 * last answer and says what went wrong.
 */
export function useTimelineHistory(range: TimelineRange): Loaded {
  const snap = useSnapshot()
  const ids = snap.status?.workspaces.map((w) => w.id).join(',') ?? ''
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const [state, setState] = useState<Loaded & { range: TimelineRange }>({ data: null, error: null, range })
  const lastFetch = useRef({ at: 0, range })
  const seq = useRef(0)

  useEffect(() => {
    if (!ids) return
    let timer: number | undefined
    const run = () => {
      lastFetch.current = { at: Date.now(), range }
      const mine = ++seq.current
      load(ids.split(',').map(Number), range).then(
        (data) => mine === seq.current && setState({ data, error: null, range }),
        (e: unknown) => mine === seq.current && setState((s) => ({ ...s, error: e instanceof Error ? e.message : String(e), range })),
      )
    }
    // A new range is read at once; a feed that moves is read when the gap since the last read has passed.
    const wait = lastFetch.current.range === range ? lastFetch.current.at + FETCH_GAP - Date.now() : 0
    if (wait > 0) timer = window.setTimeout(run, wait)
    else run()
    return () => window.clearTimeout(timer)
  }, [ids, range, feedLast])

  if (state.range !== range) return { data: null, error: null }
  return { data: state.data, error: state.error }
}
