import type { ReactNode } from 'react'
import type { DecisionView, DeskEvent, DeskQuestion } from '../api/types'
import { DecisionCard } from '../components/DecisionCard'
import { markFor, type Tone } from '../model/marks'
import type { TopicView } from '../model/topic'
import { since } from '../format'
import { href } from '../router'
import type { DeskThread } from '../state/desk'
import { glyphTone } from '../ui/StatusGlyph'
import type { ThreadEntry, WorkStep } from '../ui/Thread'
import { QuestionActions } from './questions'
import { replyRef } from './reply'
import { threadId } from '../model/backlog'

const RUN_KINDS = new Set(['run_ended', 'run_passed', 'run_failed', 'run_interrupted', 'run_quota'])

const when = (iso: string, now: number) => (
  <time dateTime={iso} title={iso} className="sh-when">
    {since(iso, now)}
  </time>
)

/** A topic's name on the message of a turn tied to it: a click filters the thread, or lifts the filter. */
export function TopicTag({ topic, active, onFilter }: { topic: TopicView; active: boolean; onFilter: (id: number | null) => void }) {
  return (
    <button type="button" className="sh-chip sh-topic-tag" aria-pressed={active} title={topic.title} aria-label={`Topic: ${topic.title}`} onClick={() => onFilter(active ? null : topic.id)}>
      {topic.title}
    </button>
  )
}

/** Steps behind a desk reply, in the order they happened. */
export function workSteps(work: readonly DeskEvent[], now: number): WorkStep[] {
  return work.map((w) => ({
    key: w.seq,
    kind: w.kind === 'tool' ? 'tool' : 'note',
    text: w.text ?? '',
    ...(w.kind === 'tool' ? {} : { when: when(w.created, now) }),
  }))
}

/** A feed event as an event line, or, for a decision that waits, the decision card. */
export function feedEntry({
  f, now, decision, flash = false, unseen = false, selected = false,
}: {
  f: { id?: number; event: string; text: string; ref?: number; created: string }
  now: number
  decision?: DecisionView
  flash?: boolean
  /** The decision here waits on the person and has not been opened. */
  unseen?: boolean
  /** The decision here is the one open in the main column. */
  selected?: boolean
}): ThreadEntry {
  const key = `f${f.id ?? f.created}`
  const anchor = f.id ? `f${f.id}` : undefined
  if (decision) return { kind: 'decision', key, ...(anchor ? { anchor } : {}), flash, children: <DecisionCard d={decision} variant="inline" unseen={unseen} selected={selected} /> }
  const m = markFor(f.event)
  return {
    kind: 'event',
    key,
    ...(anchor ? { anchor } : {}),
    flash,
    glyph: m.glyph,
    tone: glyphTone(m.tone),
    label: m.label,
    age: when(f.created, now),
    children: (
      <>
        {f.text} {RUN_KINDS.has(f.event) && f.ref ? <a href={href.log(f.ref)}>log</a> : null}
      </>
    ),
  }
}

/** A line of the desk's stream that is not a message: a note, an error, the end of a turn. */
function lineEntry(key: string, glyph: string, tone: Tone, label: string, body: ReactNode, anchor?: string): ThreadEntry {
  return { kind: 'event', key, ...(anchor ? { anchor } : {}), glyph, tone: glyphTone(tone), label, children: body }
}

/** One event of the desk's stream as a row, or null when it needs none. */
export function deskEntry({
  e, key, now, work = [], questions = [], flash = '', thread, topicFilter = null, onCleared, onReplyRef, unseen = false, topic, activeTopic = null, onTopic,
}: {
  e: DeskEvent
  key: string
  now: number
  /** Tool calls and wake notes of this reply's turn, folded under it. */
  work?: DeskEvent[]
  /** Desk questions asked in this message's turn, of any state: each is an anchor, the open ones carry actions. */
  questions?: DeskQuestion[]
  flash?: string
  thread?: DeskThread
  topicFilter?: number | null
  onCleared?: () => void
  /** Scrolls to the row of the question a reply names. */
  onReplyRef?: (questionId: number) => void
  /** A question asked here waits on the person and has not been opened. */
  unseen?: boolean
  topic?: TopicView
  activeTopic?: number | null
  onTopic?: (id: number | null) => void
}): ThreadEntry | null {
  const text = e.text ?? ''
  const pill = topic && onTopic ? <TopicTag topic={topic} active={activeTopic === topic.id} onFilter={onTopic} /> : null
  switch (e.kind) {
    case 'user': {
      const ref = replyRef(text)
      return {
        kind: 'you',
        key,
        age: when(e.created, now),
        pill,
        reply:
          ref !== null && onReplyRef ? (
            <button type="button" className="sh-link sh-replyref" onClick={() => onReplyRef(ref)}>
              Replying to your question {ref}
            </button>
          ) : null,
        children: text,
      }
    }
    case 'assistant': {
      const asked = questions[0]
      const open = questions.filter((q) => q.state === 'open')
      return {
        kind: 'desk',
        key,
        ...(asked ? { anchor: `q${asked.id}` } : {}),
        flash: questions.some((q) => flash === `q${q.id}`),
        age: when(e.created, now),
        pill,
        asked: open.length > 0,
        unseen,
        text,
        work: workSteps(work, now),
        actions: (
          <>
            {questions.slice(1).map((q) => (
              <span key={`a${q.id}`} id={threadId(`q${q.id}`)} />
            ))}
            {thread && open.map((q) => <QuestionActions key={q.id} q={q} thread={thread} topicId={topicFilter} onCleared={() => onCleared?.()} onReplied={() => onCleared?.()} />)}
          </>
        ),
      }
    }
    case 'system':
      return {
        kind: 'node',
        key,
        children: (
          <details className="sh-woke">
            <summary>
              Shepherd woke the front desk {when(e.created, now)}
            </summary>
            <p>{text}</p>
          </details>
        ),
      }
    case 'tool':
      return lineEntry(key, '▸', 'muted', 'tool call', <span className="sh-mono">{text}</span>)
    case 'error':
      return lineEntry(key, '✗', 'bad', 'error', <span role="alert">{text}</span>)
    case 'new':
      return lineEntry(key, '+', 'accent', 'new conversation', <>New conversation {when(e.created, now)}</>)
    case 'turn_end':
      // A turn that ended well needs no row; one that did not says how.
      return text && text !== 'done' ? lineEntry(key, '↯', 'broken', 'turn ended early', <>Turn ended: {text}</>) : null
    case 'cost':
      return text ? lineEntry(key, '$', 'muted', 'cost', <>Turn cost ${text}</>) : null
    case 'turn_start':
      return null
    default:
      // A kind a newer daemon adds still shows, as a note.
      return lineEntry(key, '·', 'muted', e.kind, <>{text || e.kind}</>)
  }
}
