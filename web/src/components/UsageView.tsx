import type { CSSProperties, ReactNode } from 'react'
import { usd } from '../format'
import { budgetUse, heroSub, type CardRow } from '../model/spend'
import { href } from '../router'
import { EmptyState, GroupHeader, ListRow, Meter, Skeleton, StatusGlyph, type GlyphTone } from '../ui'

/** The big spend figure: today's dollars, "of $X today", "near budget" past 80% of the cap. */
export function SpendHero({ usdToday, budget, sub, offline }: { usdToday: number; budget: number; sub: { of: string; note: string }; offline: boolean }) {
  const use = budgetUse({ day: '', usd: usdToday, budget, credit_usd: 0, by_source: {} })
  return (
    <section className="sh-spend-hero" aria-label="Spent today" data-component="SpendHero">
      <p className="sh-spend-figure">{usd(usdToday) || '$0.00'}</p>
      <p className="sh-spend-of">
        {sub.of}
        {sub.note && (
          <span className="sh-spend-note">
            {' · '}
            <span className={use?.over ? 'is-over' : 'is-near'}>{sub.note}</span>
          </span>
        )}
      </p>
      {budget > 0 && <Meter value={usdToday} max={budget} label="Spent today" valueText={`${usd(usdToday) || '$0.00'} of ${usd(budget)} today`} />}
      {offline && <p className="sh-spend-stale">Showing what was last loaded.</p>}
    </section>
  )
}

/** When the spend resets: budgets count a local day, so the figure starts over at midnight. */
export function ResetNote(): ReactNode {
  return <p className="sh-spend-reset">Resets at midnight, local time.</p>
}

/** One breakdown card: By project, By agent or By model. */
export function SpendCard({ title, rows, empty }: { title: string; rows: CardRow[]; empty: string }) {
  return (
    <section className="sh-spend-card" aria-label={title} data-component="SpendCard">
      <GroupHeader label={title} />
      {rows.length === 0 ? (
        <EmptyState title={empty} />
      ) : (
        <ul className="sh-spend-rows">
          {rows.map((r) => (
            <li key={r.key} className="sh-spend-row" title={r.title}>
              <span className="sh-spend-name">{r.label}</span>
              <span className="sh-spend-amount">{usd(r.usd) || '$0.00'}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/** The quota holds: which agents Shepherd holds, and until when. */
export function HoldsCard({ holds, now }: { holds: { agent: string; until: string }[] | undefined; now: number }) {
  if (holds === undefined) return null
  return (
    <section className="sh-spend-card" aria-label="Quota holds" data-component="HoldsCard">
      <GroupHeader label="Quota holds" />
      {holds.length === 0 ? (
        <p className="sh-spend-none">None.</p>
      ) : (
        <ul className="sh-spend-rows">
          {holds.map((h) => (
            <li key={h.agent} className="sh-spend-row">
              <span className="sh-spend-name">
                <StatusGlyph glyph="⌀" tone="held" label="out of quota" /> {h.agent}
              </span>
              <span className="sh-spend-amount">until {clockShort(h.until, now)}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/** A chart day as the chart's axis names it: "Oct 3". */
export function dayShort(day: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day)
  if (!m) return day
  return `${['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'][Number(m[2]) - 1]} ${Number(m[3])}`
}

/** A chart day as its number alone: "3". The narrow chart names only this; the bar's
 * hover and its screen-reader words keep the full day. */
export function dayNum(day: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day)
  return m ? String(Number(m[3])) : day
}

export function clockShort(iso: string, now: number): string {
  const t = new Date(iso)
  if (isNaN(t.getTime())) return iso
  const hm = `${String(t.getHours()).padStart(2, '0')}:${String(t.getMinutes()).padStart(2, '0')}`
  const n = new Date(now)
  const sameDay = t.getFullYear() === n.getFullYear() && t.getMonth() === n.getMonth() && t.getDate() === n.getDate()
  return sameDay ? hm : `${dayShort(`${t.getFullYear()}-${String(t.getMonth() + 1).padStart(2, '0')}-${String(t.getDate()).padStart(2, '0')}`)} ${hm}`
}

/** The daily chart: a bar per day, lichen under 80% of the cap, lantern at 80% and past
 * it, broken at the cap. Only the bar heights are inline, as they are computed. */
export function SpendChart({ bars, max, cap }: { bars: { day: string; usd: number; state: 'over' | 'near' | 'under' }[]; max: number; cap: number }) {
  const pct = (n: number) => `${max > 0 ? Math.min(100, (n / max) * 100) : 0}%`
  const capPct = cap > 0 ? pct(cap) : null
  return (
    <section className="sh-spend-chart" aria-label={`Spend, last ${bars.length} days`} data-component="SpendChart">
      <GroupHeader label={`Last ${bars.length} days`} hint={cap > 0 ? `daily cap ${usd(cap)}` : undefined} />
      <div role="img" aria-label={`Spend per day for the last ${bars.length} days${cap > 0 ? `, daily cap ${usd(cap)}` : ''}`}>
        <div className="sh-chart-plot">
          {capPct !== null && <span className="sh-chart-cap" style={{ '--cap-at': capPct } as CSSProperties} title={`Daily cap ${usd(cap)}`} />}
          {bars.map((b) => (
            <span key={b.day} className={`sh-chart-bar is-${b.state}`} style={{ height: pct(b.usd) }} title={`${b.day}: ${usd(b.usd) || '$0.00'}`}>
              <span className="visually-hidden">{`${b.day}: ${usd(b.usd) || '$0.00'}`}</span>
            </span>
          ))}
        </div>
        <div className="sh-chart-days" aria-hidden="true">
          {bars.map((b) => (
            <span key={b.day}><span className="sh-chart-day-full">{dayShort(b.day)}</span><span className="sh-chart-day-num">{dayNum(b.day)}</span></span>
          ))}
        </div>
      </div>
    </section>
  )
}

const RUN_MARK: Record<string, { glyph: string; tone: GlyphTone }> = {
  running: { glyph: '●', tone: 'live' },
  succeeded: { glyph: '✓', tone: 'ok' },
  failed: { glyph: '✗', tone: 'bad' },
  interrupted: { glyph: '↯', tone: 'bad' },
  stopped: { glyph: '○', tone: 'idle' },
}

/** The most expensive runs of the window, most expensive first. */
export function CostliestList({ runs }: {
  runs: { id: number; lane_id: number; repo: string; lane: string; agent: string; model?: string; state: string; commits: number; cost_usd?: number; credits?: number; cost_estimated?: boolean; started: string; ended?: string; task: string }[]
}) {
  return (
    <section className="sh-spend-runs" aria-label="Most expensive runs" data-component="CostliestList">
      <GroupHeader label="Most expensive runs" />
      {runs.length === 0 ? (
        <EmptyState title="No run in this range." />
      ) : (
        <ul className="sh-spend-runlist">
          {runs.map((r) => (
            <li key={r.id} className="sh-spend-run">
              <ListRow
                glyph={RUN_MARK[r.state]?.glyph ?? '○'}
                tone={RUN_MARK[r.state]?.tone ?? 'idle'}
                glyphLabel={r.state}
                title={<>run {r.id} · {r.repo} / {r.lane}</>}
                sub={`${r.agent}${r.model ? ` · ${r.model}` : ''} · ${r.state} · ${r.commits} ${r.commits === 1 ? 'commit' : 'commits'}`}
                href={href.log(r.id)}
                titleAttr={`run ${r.id}: ${r.task || r.state}`}
              />
              <span className={`sh-spend-cost${r.cost_usd !== undefined && r.cost_usd > 0 ? ' is-ok' : ''}`}>
                {usd(r.cost_usd ?? 0) || '$0.00'}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/** The view's loading and failure states. */
export function UsageLoading(): ReactNode {
  return <Skeleton lines={5} />
}

export function UsageError({ error }: { error: string }): ReactNode {
  return <p className="sh-spend-error">Could not load usage: {error}</p>
}

export { heroSub }
