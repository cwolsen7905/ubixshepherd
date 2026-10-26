import { useMemo, type ReactNode } from 'react'
import { Markdown } from '../chat/md'
import { threadId } from '../model/backlog'
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
  className?: string
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
export function ThreadRow({ it }: { it: ThreadEntry }) {
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
            <b>You</b> {it.age}
            {it.pill}
          </div>
          {it.reply}
          <p>{it.children}</p>
        </div>
      )
    case 'desk':
      return (
        <div id={id} className={cx('sh-msg', 'sh-msg--desk', it.asked && 'is-asked', it.flash && 'is-flash', it.partial && 'is-partial')} data-kind="desk">
          <div className="sh-who">
            <b>Front desk</b> {it.age}
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
          {it.age && <span className="sh-event-time">{it.age}</span>}
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
 * The thread (`.sh-thread`): your messages on the right over a sunk ground, the front desk's
 * on the left, event lines between them and decision cards inline. One list serves the
 * Conversation, the column beside a decision or any other main-body page (filtered to its
 * topic) and a lane's Conversation tab: the caller builds the rows, this lays them out.
 */
export function Thread({ items, topic = null, empty, label = 'Conversation with the front desk', className, scrollRef, before, layout = 'main', busy }: ThreadProps) {
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
      {shown.map((it) => (
        <ThreadRow key={it.key} it={it} />
      ))}
    </div>
  )
}
