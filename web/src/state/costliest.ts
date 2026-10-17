import { useMemo } from 'react'
import { history } from '../api/client'
import type { RunHistory } from '../api/types'
import { useHistory } from '../state/history'
import { useSnapshot } from '../state/context'

/** How many expensive runs the view names. */
export const COSTLIEST_LIMIT = 5

/**
 * The most expensive runs of the chart's window: the snapshot's recent runs and the
 * history's, as one list. The snapshot holds the 200 newest; the history read covers the
 * window up to the daemon's limit. Either may be missing; what is here is what is shown.
 */
export function useCostliest(since: string): { runs: RunHistory['runs']; error: string | null; loading: boolean } {
  const snap = useSnapshot()
  const hist = useHistory(`usage:${since}`, async (ids) => {
    const parts = await Promise.all(ids.map((id) => history.runs(id, since, '', 0)))
    return {
      runs: parts.flatMap((p) => p.runs), count: 0, cost_usd: 0, credits: 0, agents: [], lanes: [],
    }
  })
  const runs = useMemo(() => {
    const start = Date.parse(since)
    const seen = new Map<number, RunHistory['runs'][number]>()
    for (const r of hist.data?.runs ?? []) seen.set(r.id, r)
    for (const r of snap.runs) {
      if (Date.parse(r.started) >= start) {
        seen.set(r.id, {
          id: r.id, lane_id: r.lane_id, repo: r.repo, lane: r.lane, lane_state: 'open', agent: r.agent,
          model: r.model, state: r.state, commits: r.commits, cost_usd: r.cost_usd, credits: r.credits,
          cost_estimated: r.cost_estimated, started: r.started, ended: r.ended, task: r.prompt.split('\n')[0] ?? '',
        })
      }
    }
    return [...seen.values()]
  }, [hist.data, snap.runs, since])
  return { runs, error: hist.error, loading: hist.loading && runs.length === 0 }
}
