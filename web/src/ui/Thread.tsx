import { useMemo, type ReactNode } from 'react'
import { Markdown } from '../chat/md'
import { threadId } from '../model/backlog'
import { since } from '../format'
import { useNow } from '../hooks'
import { cx } from './cx'
import { StatusGlyph, type GlyphTone } from './StatusGlyph'
import './Thread.css'

/** What every row of a thread carries. */
interface Base {
  key: string
  /** The row's anchor: a jump lands on `thread-<anchor>`. */
  anchor?: string
  /** A jump just landed here. */
  flash?: boolean
  /** The topics the row belongs to. Under a topic filter a row with none is left out. */
  topics?: readonly number[]
  /**
   * When the row happened (RFC 3339). A message or event line with one shows its local
   * 24-hour clock time in place of `age`, and a date line goes in wherever the day changes.
   */
  at?: string
}

/** One step behind a desk message: a tool call, or a note that Shepherd woke the desk. */
export interface WorkStep {
  key: string | number
  kind: 'tool' | 'note'
  text: string
  /** A note's age. */
  when?: ReactNode
}

export type ThreadEntry =
  | (Base & {
      kind: 'you'
      age: ReactNode
      /** The topic pill. */
      pill?: ReactNode
      /** The typed-ID chip of the question this answers ("question 3"). */
      reply?: ReactNode
      /** Files the message carried. */
      attachments?: ReactNode
      children: ReactNode
    })
  | (Base & {
      kind: 'desk'
      age?: ReactNode
      pill?: ReactNode
      /** The message asked something of the person that is still open. */
      asked?: boolean
      unseen?: boolean
      /** The message is still arriving. */
      partial?: boolean
      /** The message's text. It opens with "Needs you:" and a list, or "Nothing needs you." */
      text: string
      /** The tool calls and wake notes behind it, in "Show work", closed. */
      work?: readonly WorkStep[]
      /** Reply and Skip, and the anchors of the questions asked. */
      actions?: ReactNode
    })
  | (Base & {
      kind: 'event'
      glyph: string
      tone?: GlyphTone
      label: string
      age?: ReactNode
      children: ReactNode
    })
  | (Base & { kind: 'decision'; children: ReactNode })
  | (Base & { kind: 'new'; label: ReactNode })
  | (Base & { kind: 'node'; children: ReactNode })

export interface ThreadProps {
  items: readonly ThreadEntry[]
  /** The scroll region, for a caller that follows the bottom. */
  scrollRef?: (el: HTMLDivElement | null) => void
  /** Above the first row, in the scroll: the "Since you were away" card, notes. */
  before?: ReactNode
  /** `side`: the column beside a main-body page, as wide as its panel. */
  layout?: 'main' | 'side'
  busy?: boolean
  /** Only the rows of this topic. */
  topic?: number | null
  /** Shown when nothing is left to show. */
  empty?: ReactNode
  /** The thread's name for a screen reader. */
  label?: string
  /** The time the relative ages and "Today" are counted from: the caller's ticking clock. Left out, the thread keeps one of its own. */
  now?: number
  className?: string
}

const pad = (n: number) => String(n).padStart(2, '0')
const parse = (iso: string | undefined) => {
  if (!iso) return null
  const t = new Date(iso)
  return isNaN(t.getTime()) ? null : t
}
const sameDay = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
/** A local day as a key: "2026-11-22". */
const dayKey = (t: Date) => `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())}`

/** A local time of day, 24-hour: "14:05", "00:30". Built by hand, as Intl writes midnight "24:00" in some engines. */
export function clockOf(t: Date): string {
  return `${pad(t.getHours())}:${pad(t.getMinutes())}`
}

/** The date line's words: "Today", "Yesterday", "Friday, Nov 20", or with its year when not this year's. */
export function dayLabel(t: Date, now: number): string {
  const n = new Date(now)
  if (sameDay(t, n)) return 'Today'
  const y = new Date(n.getFullYear(), n.getMonth(), n.getDate() - 1)
  if (sameDay(t, y)) return 'Yesterday'
  return t.toLocaleDateString('en-US', { weekday: 'long', month: 'short', day: 'numeric', ...(t.getFullYear() === n.getFullYear() ? {} : { year: 'numeric' }) })
}

/** The whole date and time, for a title: "Sun, Nov 22, 2026 14:05". */
function fullTime(t: Date): string {
  return `${t.toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' })} ${clockOf(t)}`
}

/**
 * A row's clock time (`.sh-clock`): "14:05" in local 24-hour time, the full date and how long
 * ago in its title. `fallback` (a relative age) shows when the time is missing or unparsable.
 */
export function Clock({ at, now, fallback = null }: { at?: string; now: number; fallback?: ReactNode }) {
  const t = parse(at)
  if (!t) return <>{fallback}</>
  const ago = since(at, now)
  return (
    <time className="sh-clock" dateTime={at} title={ago ? `${fullTime(t)}, ${ago} ago` : fullTime(t)}>
      {clockOf(t)}
    </time>
  )
}

/** The date line (`.sh-dayline`) where the thread's day changes. */
function DayLine({ t, now }: { t: Date; now: number }) {
  const words = dayLabel(t, now)
  const full = t.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' })
  return (
    <div className="sh-dayline" role="separator" aria-label={words === 'Today' || words === 'Yesterday' ? `${words}, ${full}` : full} data-component="DayLine">
      <time dateTime={dayKey(t)} title={full}>
        {words}
      </time>
    </div>
  )
}

const NEEDS = /^\s*Needs you:[ \t]*(?:\n|$)/
const NOTHING = /^\s*Nothing needs you\.?[ \t]*(?:\n|$)/

/**
 * A desk message's text. A message that opens with "Needs you:" shows that line in lantern ink
 * over its numbered list, one item per line with its link; "Nothing needs you." opens in bold.
 * What follows (status bullets, background) reads as the quieter part. Other text is markdown.
 */
export function DeskBody({ text }: { text: string }) {
  const needs = NEEDS.exec(text)
  const nothing = needs ? null : NOTHING.exec(text)
  const lead = needs ?? nothing
  if (!lead) return <Markdown text={text} />
  const rest = text.slice(lead[0].length)
  return (
    <div className="sh-desk-lead">
      <p className={needs ? 'sh-desk-needs' : 'sh-desk-nothing'}>{needs ? 'Needs you:' : 'Nothing needs you.'}</p>
      {rest.trim() !== '' && <Markdown text={rest} />}
    </div>
  )
}

/** "Show work (2 tool calls)": the steps behind a message, folded, closed until opened. */
export function WorkFold({ steps }: { steps: readonly WorkStep[] }) {
  const tools = steps.filter((s) => s.kind === 'tool').length
  const notes = steps.length - tools
  const label = [tools > 0 ? `${tools} tool ${tools === 1 ? 'call' : 'calls'}` : '', notes > 0 ? 'woke the front desk' : ''].filter(Boolean).join(', ')
  return (
    <details className="sh-work">
      <summary>Show work ({label})</summary>
      <div className="sh-work-body">
        {steps.map((s) =>
          s.kind === 'tool' ? (
            <div key={s.key}>
              <span aria-hidden="true">▸ </span>
              <span className="visually-hidden">tool call </span>
              {s.text}
            </div>
          ) : (
            <div key={s.key} className="sh-work-note">
              <span aria-hidden="true">· </span>Shepherd woke the front desk {s.when}
              {s.text && <div>{s.text}</div>}
            </div>
          ),
        )}
      </div>
    </details>
  )
}

/** One row of a thread, for a caller that draws rows of its own (a fold of events). */
export function ThreadRow({ it, now }: { it: ThreadEntry; now: number }) {
  const id = it.anchor ? threadId(it.anchor) : undefined
  switch (it.kind) {
    case 'new':
      return (
        <div id={id} className="sh-newdiv" role="separator">
          {it.label}
        </div>
      )
    case 'you':
      return (
        <div id={id} className={cx('sh-msg', 'sh-msg--you', it.flash && 'is-flash')} data-kind="you">
          <div className="sh-who">
            <b>You</b> <Clock at={it.at} now={now} fallback={it.age} />
            {it.pill}
          </div>
          {it.reply}
          <p>{it.children}</p>
          {it.attachments}
        </div>
      )
    case 'desk':
      return (
        <div id={id} className={cx('sh-msg', 'sh-msg--desk', it.asked && 'is-asked', it.flash && 'is-flash', it.partial && 'is-partial')} data-kind="desk">
          <div className="sh-who">
            <b>Front desk</b> <Clock at={it.at} now={now} fallback={it.age} />
            {it.unseen && <span className="sh-unseen" role="img" aria-label="unseen" />}
            {it.pill}
          </div>
          <DeskBody text={it.text} />
          {it.work && it.work.length > 0 && <WorkFold steps={it.work} />}
          {it.actions}
        </div>
      )
    case 'event':
      return (
        <div id={id} className={cx('sh-event', it.flash && 'is-flash')} data-kind="event">
          <StatusGlyph glyph={it.glyph} tone={it.tone ?? 'idle'} label={it.label} />
          <span>{it.children}</span>
          {(parse(it.at) || it.age) && (
            <span className="sh-event-time">
              <Clock at={it.at} now={now} fallback={it.age} />
            </span>
          )}
        </div>
      )
    case 'decision':
      return (
        <div id={id} className={cx('sh-thread-decision', it.flash && 'is-flash')} data-kind="decision">
          {it.children}
        </div>
      )
    case 'node':
      return (
        <div id={id} className={cx('sh-thread-node', it.flash && 'is-flash')}>
          {it.children}
        </div>
      )
  }
}

/**
 * The rows with a date line before each that starts a new local day, the first included. A
 * row with no time leaves the day as it was; a date line that falls just after the "n new"
 * divider goes before it, so the divider sits next to the row it names.
 */
function withDayLines(rows: readonly ThreadEntry[], now: number): ReactNode[] {
  const out: ReactNode[] = []
  let last = ''
  const lineFor = (it: ThreadEntry | undefined) => {
    const t = parse(it?.at)
    if (!t || dayKey(t) === last) return null
    last = dayKey(t)
    return <DayLine key={`day-${it?.key}`} t={t} now={now} />
  }
  rows.forEach((it, i) => {
    if (it.kind === 'new') {
      out.push(lineFor(rows.slice(i + 1).find((r) => parse(r.at))))
    } else {
      out.push(lineFor(it))
    }
    out.push(<ThreadRow key={it.key} it={it} now={now} />)
  })
  return out
}

/**
 * The thread (`.sh-thread`): your messages on the right over a sunk ground, the front desk's
 * on the left, event lines between them and decision cards inline. One list serves the
 * Conversation, the column beside a decision or any other main-body page (filtered to its
 * topic) and a lane's Conversation tab: the caller builds the rows, this lays them out.
 */
export function Thread(props: ThreadProps) {
  return props.now === undefined ? <ClockedThread {...props} /> : <ThreadBody {...props} now={props.now} />
}

/** A thread for a caller with no clock: it ticks one of its own, so "Today" and the ages stay true. */
function ClockedThread(props: ThreadProps) {
  return <ThreadBody {...props} now={useNow()} />
}

function ThreadBody({ items, topic = null, empty, label = 'Conversation with the front desk', className, scrollRef, before, layout = 'main', busy, now }: ThreadProps & { now: number }) {
  const shown = useMemo(
    () => (topic === null ? items : items.filter((it) => it.kind !== 'new' && (it.topics?.includes(topic) ?? false))),
    [items, topic],
  )
  return (
    <div
      ref={scrollRef}
      className={cx('sh-scroll', 'sh-scroll--thread', layout === 'side' && 'sh-scroll--side', className)}
      data-component="Thread"
      data-layout={layout}
      data-filtered={topic !== null ? 'true' : undefined}
      role="log"
      aria-live="polite"
      aria-relevant="additions"
      aria-label={label}
      aria-busy={busy}
      tabIndex={0}
    >
      {before}
      {shown.length === 0 && empty}
      {withDayLines(shown, now)}
    </div>
  )
}
