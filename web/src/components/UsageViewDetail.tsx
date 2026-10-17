import { Fragment, useMemo, useState } from 'react'
import type { UsageGroup, UsageStats } from '../api/types'
import { costNote, tokens } from '../format'
import {
  chartSections, DEFAULT_SORT, nextSort, sortGroups, USAGE_RANGES, type Sort, type SortKey,
} from '../model/usage'
import { figure, summaryCards, totalCost } from '../model/usageSummary'
import { go, href, type UsageRange } from '../router'
import { EmptyState, GroupHeader } from '../ui'
import { ContextChart } from './ContextChart'

/** The one range control: Today, 7 days, 30 days or Custom with its two days. It drives every section. */
export function UsageRangeControl({ range, from, to }: { range: UsageRange; from: string; to: string }) {
  const pick = (id: UsageRange) => go(href.usage(id === 'custom' ? { range: 'custom', from, to } : { range: id }))
  const custom = (f: string, t: string) => go(href.usage({ range: 'custom', from: f, to: t }))
  return (
    <div className="sh-usage-range" data-component="UsageRangeControl">
      <div className="sh-segmented" role="group" aria-label="Date range">
        {USAGE_RANGES.map((r) => (
          <button key={r.id} type="button" aria-pressed={r.id === range} onClick={() => pick(r.id)}>
            {r.label}
          </button>
        ))}
      </div>
      {range === 'custom' && (
        <>
          <label className="sh-usage-day">
            <span>From</span>
            <input type="date" value={from} max={to} onChange={(e) => e.target.value && custom(e.target.value, to)} />
          </label>
          <label className="sh-usage-day">
            <span>To</span>
            <input type="date" value={to} min={from} onChange={(e) => e.target.value && custom(from, e.target.value)} />
          </label>
        </>
      )}
    </div>
  )
}

/** Total cost, Desk vs agents, Runs and turns, Over 200k. */
export function SummaryCards({ stats }: { stats: UsageStats }) {
  return (
    <dl className="sh-usage-cards" aria-label="Summary" data-component="SummaryCards">
      {summaryCards(stats).map((c) => (
        <div key={c.label} className="sh-spend-card sh-usage-stat">
          <dt>{c.label}</dt>
          <dd>
            <strong>{c.value}</strong>
            <span>{c.sub}</span>
          </dd>
        </div>
      ))}
    </dl>
  )
}

const COLUMNS: { key: SortKey; label: string; title?: string }[] = [
  { key: 'count', label: 'Runs', title: 'Records in the group; the ones with token counts are measured' },
  { key: 'input_tokens', label: 'Input', title: 'Prompt tokens that were not cached' },
  { key: 'cache_read_tokens', label: 'Cache read', title: 'Prompt tokens served from the cache, counted again on every request' },
  { key: 'cache_creation_tokens', label: 'Cache write', title: 'Prompt tokens written to the cache' },
  { key: 'output_tokens', label: 'Output' },
  { key: 'peak_context', label: 'Peak context', title: 'The largest single request’s prompt' },
  { key: 'over_200k', label: '>200k', title: 'Records whose peak went past 200,000 tokens' },
  { key: 'compactions', label: 'Compactions', title: 'Times the CLI summarised the conversation to make room' },
  { key: 'cost_usd', label: 'Cost' },
]

function ariaSort(sort: Sort, key: SortKey) {
  return sort.key !== key ? undefined : sort.desc ? ('descending' as const) : ('ascending' as const)
}

/** A token figure of a measured group: "–" for a group without token counts, nothing for a missing number. */
const tok = (g: UsageGroup, v: number | undefined) => (g.measured ? figure(v, tokens) : '–')

/** A token table: one row per group, sortable by any column, cost first by default. */
export function UsageGroupTable({
  caption, nameLabel, groups, sort, onSort, lane,
}: {
  caption: string
  nameLabel: string
  groups: UsageGroup[]
  sort: Sort
  onSort: (k: SortKey) => void
  lane?: boolean
}) {
  const rows = useMemo(() => sortGroups(groups, sort), [groups, sort])
  const head = (key: SortKey, label: string, title?: string, num?: boolean) => (
    <th scope="col" className={num ? 'num' : undefined} aria-sort={ariaSort(sort, key)} title={title}>
      <button type="button" className="sh-sort" onClick={() => onSort(key)}>
        {label}
        <span aria-hidden="true">{sort.key === key ? (sort.desc ? ' ↓' : ' ↑') : ''}</span>
      </button>
    </th>
  )
  return (
    <section className="sh-spend-card sh-usage-group" aria-label={caption} data-component="UsageGroupTable">
      <GroupHeader label={caption} />
      {rows.length === 0 ? (
        <EmptyState title="Nothing recorded in this range." />
      ) : (
        <div className="sh-usage-scroll">
          <table className="sh-runs sh-usage-table">
            <thead>
              <tr>
                {head('name', nameLabel)}
                {COLUMNS.map((c) => <Fragment key={c.key}>{head(c.key, c.label, c.title, true)}</Fragment>)}
              </tr>
            </thead>
            <tbody>
              {rows.map((g) => (
                <tr key={g.key}>
                  <td className="sh-usage-name" title={g.key}>
                    {lane && g.repo ? <><span className="sh-usage-faint">{g.repo} /</span> {g.lane}</> : g.key}
                  </td>
                  <td className="num" title={g.measured === g.count ? undefined : `${g.measured} with token counts`}>
                    {figure(g.count)}
                    {g.measured < g.count && <span className="sh-usage-faint"> ({figure(g.measured)})</span>}
                  </td>
                  <td className="num">{tok(g, g.input_tokens)}</td>
                  <td className="num">{tok(g, g.cache_read_tokens)}</td>
                  <td className="num">{tok(g, g.cache_creation_tokens)}</td>
                  <td className="num">{tok(g, g.output_tokens)}</td>
                  <td className="num">
                    {tok(g, g.peak_context)}
                    {g.context_window ? <span className="sh-usage-faint"> / {tokens(g.context_window)}</span> : null}
                  </td>
                  <td className={`num${g.over_200k > 0 ? ' sh-usage-warn' : ''}`}>{g.measured ? figure(g.over_200k) : '–'}</td>
                  <td className="num">{g.measured ? figure(g.compactions) : '–'}</td>
                  <td className="num" title={costNote(g)}>{totalCost(g)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

/** Tokens and context: the peak-context bars against the 200k line, then the token tables. Open by default. */
export function TokensSection({ stats }: { stats: UsageStats }) {
  const [sort, setSort] = useState<Sort>(DEFAULT_SORT)
  const onSort = (k: SortKey) => setSort((s) => nextSort(s, k))
  const sections = useMemo(
    () => chartSections(stats.by_model ?? [], stats.desk_by_model ?? [], stats.by_lane ?? []),
    [stats],
  )
  return (
    <details className="sh-usage-tokens" open data-component="TokensSection">
      <summary><GroupHeader label="Tokens and context" hint="peak context is the measure of window need; cache read counts the same prompt again on every request" /></summary>
      <section className="sh-spend-card sh-usage-group" aria-label="Peak context">
        <GroupHeader label="Peak context" />
        {sections.length === 0 ? (
          <EmptyState title="No record in this range carries token counts." />
        ) : (
          <ContextChart sections={sections} />
        )}
      </section>
      <UsageGroupTable caption="By agent and model" nameLabel="Agent and model" groups={stats.by_model ?? []} sort={sort} onSort={onSort} />
      <UsageGroupTable caption="Front desk" nameLabel="Model" groups={stats.desk_by_model ?? []} sort={sort} onSort={onSort} />
      <UsageGroupTable caption="By lane" nameLabel="Lane" groups={stats.by_lane ?? []} sort={sort} onSort={onSort} lane />
    </details>
  )
}
