import { useMemo } from 'react'
import { history } from '../api/client'
import type { RunHistory, RunRecord } from '../api/types'
import { costNote, runCost, usd } from '../format'
import { DEFAULT_RANGE, mergeHistories, RANGES, RANGE_PHRASE, rangeStart } from '../model/history'
import { go, href, type Range } from '../router'
import { useHistory } from '../state/history'
import { Button, Chip, EmptyState, ErrorState } from '../ui'
import { RunsTable } from '../ui/RunsTable'
import './Runs.css'

const RANGE_CHIPS: Record<Range, string> = { today: 'Today', week: 'Week' }

function filterAgents(agents: string[], current: string): string[] {
  return current && !agents.includes(current) ? [current, ...agents] : agents
}

/**
 * Runs page: filter chips for range/agent/lane plus a RunsTable in list variant.
 */
export function RunsPage({ range = DEFAULT_RANGE, agent = '', lane = 0 }: { range?: Range; agent?: string; lane?: number }) {
  const { data, error, loading } = useHistory<RunHistory>(`runs:${range}:${agent}:${lane}`, async (ids) => {
    const since = rangeStart(range, Date.now()).toISOString()
    return mergeHistories(await Promise.all(ids.map((id) => history.runs(id, since, agent, lane))))
  })
  const agents = useMemo(() => filterAgents(data?.agents ?? [], agent), [data?.agents, agent])
  const label = RANGES.find((r) => r.id === range)?.label ?? ''
  const lanes = data?.lanes ?? []
  const laneKnown = lane === 0 || lanes.some((l) => l.id === lane)
  const cost = data ? runCost({ cost_usd: data.cost_usd, credits: data.credits, estimated_usd: data.estimated_usd }) || usd(0) : ''

  // URL helpers; keep undefined values out of the address.
  const nav = (o: { range?: Range; agent?: string | null; lane?: number | null }) => {
    const next = { range, agent: agent || null, lane: lane || null, ...o }
    return href.runs({ range: next.range, agent: next.agent ?? undefined, lane: next.lane ?? undefined })
  }

  const header = data ? (
    <>
      <strong title={costNote({ estimated_usd: data.estimated_usd }) || undefined}>{cost}</strong> spent {RANGE_PHRASE[range]}
      <span className="faint">
        {' '}
        across {data.count} {data.count === 1 ? 'run' : 'runs'}
        {(agent || lane > 0) && ' matching the filters'}
      </span>
    </>
  ) : (
    <span className="faint">{error ? 'Could not load runs.' : 'Loading…'}</span>
  )

  return (
    <div className="page page-wide runs-page">
      <header className="page-head runs-head">
        <h1>Runs</h1>
        <p className="page-sub runs-total" aria-live="polite">
          {header}
        </p>
        <div className="page-actions">
          {(agent || lane > 0) && (
            <Button variant="ghost" size="small" onClick={() => go(href.runs({ range }))}>
              Clear filters
            </Button>
          )}
        </div>
      </header>

      <nav className="runs-filters" aria-label="Filter runs">
        <span className="runs-filter-group" role="group" aria-label="Range">
          {RANGES.map(({ id: r }) => (
            <Chip key={r} kind="filter" selected={range === r} onClick={() => go(nav({ range: r }))}>
              {RANGE_CHIPS[r]}
            </Chip>
          ))}
        </span>
        <span className="runs-filter-group" role="group" aria-label="Agent">
          <Chip kind="filter" selected={agent === ''} onClick={() => go(nav({ agent: null }))}>
            All agents
          </Chip>
          {agents.map((a) => (
            <Chip key={a} kind="filter" selected={agent === a} onClick={() => go(nav({ agent: a }))}>
              {a}
            </Chip>
          ))}
        </span>
        <span className="runs-filter-group" role="group" aria-label="Lane">
          <Chip kind="filter" selected={lane === 0} onClick={() => go(nav({ lane: null }))}>
            All lanes
          </Chip>
          {lane > 0 && !laneKnown && (
            <Chip kind="filter" selected onClick={() => go(nav({ lane }))}>
              lane {lane}
            </Chip>
          )}
          {lanes.map((l) => (
            <Chip key={l.id} kind="filter" selected={lane === l.id} onClick={() => go(nav({ lane: l.id }))}>
              {l.repo} / {l.name}
            </Chip>
          ))}
        </span>
      </nav>

      {error && <ErrorState what="Runs" error={error} />}
      {data && data.runs.length === 0 && (
        <EmptyState title={`${agent || lane > 0 ? 'No run matches these filters' : 'No run started'} in ${label.toLowerCase()}.`} />
      )}
      {data && data.runs.length > 0 && <RunsTable runs={data.runs as RunRecord[]} variant="list" />}
      {data && data.count > data.runs.length && (
        <p className="faint">Showing the newest {data.runs.length} of {data.count} runs; the total covers all of them.</p>
      )}
      {loading && data && <p className="visually-hidden">Updating…</p>}
    </div>
  )
}
