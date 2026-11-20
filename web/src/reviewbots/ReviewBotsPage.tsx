import { useState } from 'react'
import type { ReviewBot, ReviewBotProject } from '../api/types'
import { since } from '../format'
import { useNow } from '../hooks'
import { Button, Count, EmptyState, ErrorState, LoadingRows, StatusGlyph, Tabs } from '../ui'
import { botMeta, COLUMNS, plural, spend, summary } from './model'
import { useReviewBots } from './useReviewBots'

/** A count: faint at zero, lantern for what waits on the person, with its glyph and words. */
function Cell({ n, waiting = false }: { n: number; waiting?: boolean }) {
  if (n === 0) return <span className="sh-bots-zero">0</span>
  if (waiting) {
    return (
      <span className="sh-bots-waiting">
        <StatusGlyph glyph="?" tone="warn" label="Needs you" /> {n} needs you
      </span>
    )
  }
  return <>{n}</>
}

function Row({ p }: { p: ReviewBotProject }) {
  return (
    <>
      <tr aria-label={p.name} data-component="ReviewBotRow">
        <td>
          <code className="sh-bots-name">{p.name}</code>
          {p.truncated && <span className="sh-bots-note"> newest merge requests only</span>}
          {!!p.failed_mrs && <span className="sh-bots-note"> counts incomplete: {plural(p.failed_mrs, 'merge request')} not read</span>}
          {!!p.partial_mrs && <span className="sh-bots-note"> counts partial on {plural(p.partial_mrs, 'merge request')}</span>}
        </td>
        {COLUMNS.map((c) => (
          <td key={c.key} className="num">
            <Cell n={p[c.key] as number} waiting={c.key === 'needs_you'} />
          </td>
        ))}
        <td className="num">{spend(p.spend_usd, p.stamped)}</td>
      </tr>
      {p.error && (
        <tr className="sh-bots-error" data-component="ReviewBotError">
          <td colSpan={COLUMNS.length + 2}>
            <StatusGlyph glyph="✗" tone="bad" label="Failed" /> {p.name}: {p.error}
          </td>
        </tr>
      )}
    </>
  )
}

function BotPanel({ bot, asOf }: { bot: ReviewBot; asOf: string }) {
  const now = useNow(30_000)
  const items = bot.projects.flatMap((p) => (p.needs_you_items ?? []).map((i) => ({ ...i, project: p.name })))
  return (
    <section className="sh-bots-panel" data-component="ReviewBotPanel" aria-label={bot.name}>
      <p className="sh-bots-meta">
        {botMeta(bot)}
        {asOf && ` · read ${since(asOf, now) || 'just now'}${since(asOf, now) ? ' ago' : ''}`}
      </p>
      <div className="sh-bots-scroll">
        <table className="sh-bots-table" data-component="ReviewBotTable">
          <thead>
            <tr>
              <th>Project</th>
              {COLUMNS.map((c) => (
                <th key={c.key} className="num" title={c.title}>
                  {c.label}
                </th>
              ))}
              <th className="num" title="Cost stamped on the bot's notes">Spend</th>
            </tr>
          </thead>
          <tbody>
            {bot.projects.map((p) => (
              <Row key={p.name} p={p} />
            ))}
          </tbody>
          {bot.projects.length > 1 && (
            <tfoot>
              <tr aria-label="Total">
                <th scope="row">Total</th>
                {COLUMNS.map((c) => (
                  <td key={c.key} className="num">
                    <Cell n={bot.total[c.key] as number} waiting={c.key === 'needs_you'} />
                  </td>
                ))}
                <td className="num">{spend(bot.total.spend_usd, bot.total.stamped)}</td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
      {items.length > 0 && (
        <div className="sh-bots-waiting-list" data-component="ReviewBotWaiting">
          <h2>Waiting on you</h2>
          <ul>
            {items.map((i) => (
              <li key={`${i.project}!${i.iid}`}>
                <StatusGlyph glyph="?" tone="warn" label="Needs you" />{' '}
                <a className="sh-link" href={i.url} target="_blank" rel="noreferrer">
                  {i.project}!{i.iid}
                </a>{' '}
                {i.title}
              </li>
            ))}
          </ul>
        </div>
      )}
      <p className="sh-bots-note">
        A merge request counts once, under its latest status label. Dismissals are threads the bot opened that a person
        resolved. Spend is the cost stamped on the bot's notes, so a note it edits each round shows the latest round.
      </p>
    </section>
  )
}

/**
 * The review bots page: one tab per configured bot, a table of what it did in each project, and
 * the merge requests that wait on the person. Read only; the bots are set in the configuration.
 */
export function ReviewBotsPage() {
  const { data, error, loading, reload } = useReviewBots()
  const [tab, setTab] = useState('')
  const bots = data?.bots ?? []
  return (
    <div className="page sh-bots-page" data-component="ReviewBotsPage">
      <header className="page-head">
        <h1>Review bots</h1>
        <div className="page-actions">
          {data && bots.length > 0 && <span className="sh-bots-summary">{summary(bots)}</span>}
          <Button size="small" onClick={() => reload(true)} disabled={loading}>
            {loading ? 'Reading…' : 'Refresh'}
          </Button>
        </div>
      </header>
      {error && <ErrorState what="Review bots" error={error} onRetry={() => reload()} />}
      {loading && !data && !error && <LoadingRows />}
      {data && bots.length === 0 && (
        <EmptyState title="No review bots configured">
          Add a <code>review_bots:</code> section to the configuration file with the bot's forge user, its label prefix and the
          repos to read.
        </EmptyState>
      )}
      {bots.length > 0 && (
        <Tabs
          label="Review bots"
          value={tab}
          onChange={setTab}
          items={bots.map((b) => ({
            id: b.name,
            label: (
              <>
                {b.name} <Count count={b.total.needs_you} tone="attention" label="needing you" />
              </>
            ),
            content: <BotPanel bot={b} asOf={data?.as_of ?? ''} />,
          }))}
        />
      )}
    </div>
  )
}
