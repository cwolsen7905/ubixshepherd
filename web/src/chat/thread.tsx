import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { DecisionView, DeskEvent, DeskQuestion, WaitingItem } from '../api/types'
import { Backlog } from '../components/Backlog'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { TopicBar } from '../components/TopicBar'
import { Topics } from '../components/Topics'
import { TopicPill } from '../components/TopicPill'
import { DecisionCard, splitQuestion } from '../components/DecisionCard'
import { EventGlyph, Glyph } from '../components/Glyph'
import { since } from '../format'
import { MARKS } from '../model/marks'
import { inTopic, memberKeys, readTopicFilter, topicByTurn, withTopicFilter, type TopicView } from '../model/topic'
import { deskQuestions, topics as topicsApi } from '../api/client'
import { href } from '../router'
import { useSnapshot } from '../state/context'
import { DeskThread, SIGN_IN_HINT, timeline } from '../state/desk'
import { useNow } from '../hooks'
import { useFollowBottom, Countdown, useThread, CONNECTION } from './md'
import { Composer } from './composer'
import { QuestionActions } from './questions'
import { PendingStrip } from './strip'
import { Markdown } from './md'
import { clipHead } from './md'
import { prefillDraft } from '../model/chatDraft'
import { backlogItems, threadId } from '../model/backlog'
import { takeStep, STEP_EVENT, stepAnchor } from '../model/waitingStep'
import { draftKey } from '../model/chatDraft'

const RUN_KINDS = new Set(['run_ended', 'run_passed', 'run_failed', 'run_interrupted', 'run_quota'])

function FeedRow({
  item, now, decision, flash = false,
}: {
  item: { id?: number; event: string; text: string; ref?: number; created: string }
  now: number
  decision?: DecisionView
  flash?: boolean
}) {
  const age = since(item.created, now)
  return (
    <div id={item.id ? threadId(`f${item.id}`) : undefined} className={`trow${decision ? ' trow-decision' : ''}${flash ? ' is-flash' : ''}`}>
      <EventGlyph event={item.event} />
      <div className="trow-body">
        <p className="trow-line">
          {!decision && <span>{item.text}</span>}{' '}
          {RUN_KINDS.has(item.event) && item.ref ? <a href={href.log(item.ref)}>log</a> : null}{' '}
          {!decision && <time dateTime={item.created} title={item.created} className="faint trow-age">{age}</time>}
        </p>
        {decision && <DecisionCard d={decision} variant="inline" />}
      </div>
    </div>
  )
}

function DeskRow({
  e, now, topic, activeTopic = null, onTopic, questions = [], flash = '', thread, topicFilter = null, onCleared,
}: {
  e: DeskEvent
  now: number
  /** Open desk questions asked in this message's turn. */
  questions?: DeskQuestion[]
  flash?: string
  thread?: DeskThread
  topicFilter?: number | null
  onCleared?: () => void
  /** The topic this turn is tied to; none, no pill. */
  topic?: TopicView
  activeTopic?: number | null
  onTopic?: (id: number | null) => void
}) {
  const text = e.text ?? ''
  const age = since(e.created, now)
  const when = <time dateTime={e.created} title={e.created} className="faint trow-age">{age}</time>
  const pill = topic && onTopic ? <TopicPill topic={topic} active={activeTopic === topic.id} onFilter={onTopic} /> : null
  switch (e.kind) {
    case 'user':
      return (
        <div className="msg msg-user">
          <span className="msg-who">You {when}{pill}</span>
          <p className="msg-text">{text}</p>
        </div>
      )
    case 'assistant': {
      const asked = questions[0]
      return (
        <div
          id={asked ? threadId(`q${asked.id}`) : undefined}
          className={`msg msg-assistant${asked ? ' msg-asked' : ''}${asked && flash === `q${asked.id}` ? ' is-flash' : ''}`}
        >
          <span className="msg-who">Front desk {when}{pill}</span>
          <Markdown text={text} />
          {thread && questions.map((q) => (
            <QuestionActions key={q.id} q={q} thread={thread} topicId={topicFilter} onCleared={() => onCleared?.()} />
          ))}
        </div>
      )
    }
    case 'system':
      return (
        <details className="trow trow-system">
          <summary>
            <Glyph glyph={MARKS.info.glyph} tone="muted" label="note" /> Shepherd woke the front desk {when}
          </summary>
          <p className="msg-text">{text}</p>
        </details>
      )
    case 'tool':
      return (
        <p className="trow trow-tool faint">
          <Glyph glyph={MARKS.report.glyph} tone="muted" label="tool call" />
          <span className="mono">{text}</span>
        </p>
      )
    case 'error':
      return (
        <p className="trow tone-bad" role="alert">
          <Glyph glyph={MARKS.run_failed.glyph} tone="bad" label="error" />
          <span>{text}</span>
        </p>
      )
    case 'new':
      return (
        <p className="trow trow-new">
          <Glyph glyph={MARKS.lane_opened.glyph} tone="accent" label="new conversation" />
          <span>New conversation {when}</span>
        </p>
      )
    case 'turn_end':
      // A turn that ended well needs no row; one that did not says how.
      return text && text !== 'done' ? (
        <p className="trow faint">
          <Glyph glyph={MARKS.run_interrupted.glyph} tone="broken" label="turn ended early" />
          <span>Turn ended: {text}</span>
        </p>
      ) : null
    case 'cost':
      return text ? (
        <p className="trow faint">
          <Glyph glyph="$" tone="muted" label="cost" />
          <span>Turn cost ${text}</span>
        </p>
      ) : null
    case 'turn_start':
      return null
    default:
      // A kind a newer daemon adds still shows, as a note.
      return (
        <p className="trow faint">
          <Glyph glyph={MARKS.info.glyph} tone="muted" label={e.kind} />
          <span>{text || e.kind}</span>
        </p>
      )
  }
}

function ChatThread({
  thread: make, workspaceId, children,
}: {
  thread: (workspaceId: number) => DeskThread
  workspaceId: number
  children?: ReactNode
}) {
  const [thread] = useState(() => make(workspaceId))
  useEffect(() => {
    thread.start()
    return () => thread.stop()
  }, [thread])
  const d = useThread(thread)
  const live = useSnapshot()
  const now = useNow()
  const [confirming, setConfirming] = useState(false)
  const newButton = useRef<HTMLButtonElement>(null)
  const confirmButton = useRef<HTMLButtonElement>(null)
  const root = useRef<HTMLDivElement>(null)

  const all = useMemo(() => timeline(d.events, live.feed), [d.events, live.feed])
  // The topic filter is a view over the one thread, kept in the hash so it can be linked
  // and survives a reload; the desk and history are never filtered.
  const [topicId, setTopicId] = useState<number | null>(() => readTopicFilter(window.location.hash))
  const [topicList, setTopicList] = useState<TopicView[]>([])
  useEffect(() => {
    const onHash = () => setTopicId(readTopicFilter(window.location.hash))
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])
  const filterTo = useCallback((id: number | null) => {
    window.location.hash = withTopicFilter(window.location.hash, id)
    setTopicId(id)
  }, [])
  // One read of the topics serves the filter and the pill on each turn.
  useEffect(() => {
    let live = true
    const read = () =>
      topicsApi.list(true).then(
        (l) => live && setTopicList(l.topics),
        () => {},
      )
    void read()
    const id = window.setInterval(() => void read(), 15_000)
    return () => {
      live = false
      window.clearInterval(id)
    }
  }, [])
  const topic = useMemo(() => (topicId === null ? null : (topicList.find((t) => t.id === topicId) ?? null)), [topicList, topicId])
  const byTurn = useMemo(() => topicByTurn(topicList), [topicList])
  const items = useMemo(() => {
    if (topicId === null || topic === null) return all
    const members = memberKeys(topic)
    return all.filter((it) => inTopic(it, members))
  }, [all, topic, topicId])
  const open = useMemo(() => new Map(live.decisions.map((x) => [x.id, x])), [live.decisions])
  // Open decisions whose "asked" row is not in the loaded thread still wait on the person.
  const shown = new Set(items.flatMap((it) => (it.type === 'feed' && it.f.event === 'decision_asked' ? [it.f.ref ?? 0] : [])))
  const unplaced = live.decisions.filter((x) => !shown.has(x.id))

  // The questions the desk asked that are still open: each shows Reply here and Skip under
  // the desk message of its turn. Read on the waiting list's cadence and after each action.
  const [questions, setQuestions] = useState<DeskQuestion[]>([])
  const readQuestions = useCallback(() => {
    deskQuestions.list('open').then(
      (qs) => setQuestions(qs),
      () => {},
    )
  }, [])
  useEffect(() => {
    const first = window.setTimeout(readQuestions, 0)
    const id = window.setInterval(() => {
      if (document.visibilityState !== 'hidden') readQuestions()
    }, 5000)
    return () => {
      window.clearTimeout(first)
      window.clearInterval(id)
    }
  }, [readQuestions])
  // A question sits under the last desk message of the turn it was asked in.
  const questionsAt = useMemo(() => {
    const lastOfTurn = new Map<number, number>()
    for (const it of items) if (it.type === 'desk' && it.e.kind === 'assistant' && it.e.turn !== undefined) lastOfTurn.set(it.e.turn, it.e.seq)
    const out = new Map<number, DeskQuestion[]>()
    for (const q of questions) {
      const seq = lastOfTurn.get(q.turn)
      if (seq !== undefined) out.set(seq, [...(out.get(seq) ?? []), q])
    }
    return out
  }, [items, questions])

  // The rail's "in thread" links name rows the thread shows; one that is not loaded has none.
  const backlog = useMemo(() => {
    const shownFeed = new Set(items.flatMap((it) => (it.type === 'feed' ? [it.f.id] : [])))
    // A decision's long markdown stays in its card: the rail names it by its headline.
    return backlogItems({ ...live, shown: shownFeed }).map((b) =>
      b.decision ? { ...b, detail: b.detail.replace(b.decision.question.replace(/\s+/g, ' ').trim(), splitQuestion(b.decision.question).head || clipHead(b.decision.question)) } : b,
    )
  }, [items, live])
  const [flash, setFlash] = useState('')
  const jump = useCallback((anchor: string) => {
    const el = document.getElementById(threadId(anchor))
    if (!el) return
    el.scrollIntoView?.({ block: 'center' })
    setFlash(anchor)
    window.setTimeout(() => setFlash((f) => (f === anchor ? '' : f)), 2500)
  }, [])

  // The header counter's arrows: show the item the thread holds for it, or say it is not
  // there and where to act on it.
  const [stepNote, setStepNote] = useState<WaitingItem | null>(null)
  const stepRef = useRef<(it: WaitingItem) => void>(() => {})
  const showStep = (it: WaitingItem) => {
    const anchor = stepAnchor(it, {
      askedRows: new Map(items.flatMap((r) => (r.type === 'feed' && r.f.event === 'decision_asked' && r.f.id ? [[r.f.ref ?? 0, r.f.id] as [number, number]] : []))),
      unplaced: new Set(unplaced.map((x) => x.id)),
      laneAnchor: (id) => backlog.find((b) => b.laneId === id)?.anchor,
    })
    if (anchor && document.getElementById(threadId(anchor))) {
      setStepNote(null)
      jump(anchor)
    } else {
      setStepNote(it)
    }
  }
  useEffect(() => {
    stepRef.current = showStep
  })
  useEffect(() => {
    const take = () => {
      const it = takeStep()
      if (it) stepRef.current(it)
    }
    take()
    window.addEventListener(STEP_EVENT, take)
    return () => window.removeEventListener(STEP_EVENT, take)
  }, [])

  const follow = useFollowBottom(root, [d.loaded, items.length, d.partial, unplaced.length])
  useEffect(() => {
    if (confirming) confirmButton.current?.focus()
  }, [confirming])

  const conn = CONNECTION[d.connection]
  const offline = d.connection === 'reconnecting'
  const stopped = d.connection === 'signed-out' || d.connection === 'refused'

  return (
    <div ref={root} className="page chat">
      <header className="page-head chat-head">
        <h1>Chat</h1>
        <p className={`chat-conn live-dot ${conn.cls}`} role="status">
          <span aria-hidden="true">{conn.glyph}</span> {conn.text}
          {offline && <Countdown at={d.retryAt} />}
        </p>
        {offline && (
          <button type="button" className="button" onClick={() => thread.wake()}>
            Retry now
          </button>
        )}
        {children}
        <span className="chat-tools">
          {d.busy && (
            <span className="faint">
              {d.queued > 1 ? `Working, ${d.queued - 1} waiting` : 'Working'}…
            </span>
          )}
          {!confirming ? (
            <button ref={newButton} type="button" className="button" onClick={() => setConfirming(true)}>
              New conversation
            </button>
          ) : (
            <span className="chat-confirm" role="group" aria-label="Start a new conversation?">
              <span>Start fresh? The desk forgets this conversation&rsquo;s context.</span>
              <button
                ref={confirmButton}
                type="button"
                className="button button-primary"
                onClick={() => {
                  setConfirming(false)
                  void thread.newConversation()
                  newButton.current?.focus()
                }}
              >
                Start new
              </button>
              <button
                type="button"
                className="button"
                onClick={() => {
                  setConfirming(false)
                  window.setTimeout(() => newButton.current?.focus(), 0)
                }}
              >
                Cancel
              </button>
            </span>
          )}
        </span>
      </header>

      {stopped && !d.problem && (
        <p className="chat-problem tone-bad" role="alert">
          {d.loadError ?? `You are signed out. ${SIGN_IN_HINT}`}
        </p>
      )}
      {offline && d.loaded && (
        <p className="chat-problem tone-bad" role="alert">
          The daemon is not answering, so this conversation may be out of date. It reconnects by itself
          {d.failures >= 3 ? `; if it keeps failing the daemon may have moved: ${SIGN_IN_HINT}` : '.'}
        </p>
      )}
      {d.loadError && !stopped && !d.loaded && (
        <p className="chat-problem tone-bad" role="alert">
          Could not read the conversation: {d.loadError}. Retrying.
        </p>
      )}

      <div className="chat-layout">
        <div className="chat-rail">
          <Backlog
            items={backlog}
            now={now}
            spend={live.spend}
            onJump={jump}
            onStart={(text) => prefillDraft(window.localStorage, draftKey(workspaceId), text)}
          />
          <Topics active={topicId} now={now} onFilter={filterTo} />
        </div>
        <ErrorBoundary region="The conversation">
        <div className="chat-main">
        <TopicBar active={topicId} onFilter={filterTo} />
        {topicId !== null && (
          <p className="topic-filter" role="status">
            Showing: {topic && topic.id === topicId ? topic.title : `topic ${topicId}`}, {items.length} {items.length === 1 ? 'row' : 'rows'}.{' '}
            <button type="button" className="link-button" onClick={() => filterTo(null)}>
              Show all
            </button>
          </p>
        )}
        {d.more && (
          <p className="chat-earlier">
            <button type="button" className="link-button" onClick={() => void thread.loadEarlier()}>
              Load earlier messages
            </button>
          </p>
        )}

        {stepNote && (
          <p className="topic-filter" role="status">
            Not in the loaded thread: {stepNote.title}.{' '}
            {stepNote.action === 'review' && stepNote.next_id ? <a href={href.next(stepNote.next_id)}>Review</a> : null}
            {stepNote.lane_id ? <a href={href.lane(stepNote.lane_id)}>Open lane</a> : null}{' '}
            <button type="button" className="link-button" onClick={() => setStepNote(null)}>
              Dismiss
            </button>
          </p>
        )}
        <div className="thread" role="log" aria-live="polite" aria-relevant="additions" aria-label="Conversation with the front desk" aria-busy={!d.loaded}>
          {d.loaded && items.length === 0 && d.partial === '' && unplaced.length === 0 && (
            <p className="faint thread-empty">Nothing said yet. Ask the front desk what the flock is doing.</p>
          )}
          {items.map((it) =>
            it.type === 'desk' ? (
              <DeskRow
                key={it.key}
                e={it.e}
                now={now}
                topic={it.e.turn === undefined ? undefined : byTurn.get(it.e.turn)}
                activeTopic={topicId}
                onTopic={filterTo}
                questions={questionsAt.get(it.e.seq)}
                flash={flash}
                thread={thread}
                topicFilter={topicId}
                onCleared={readQuestions}
              />
            ) : (
              <FeedRow key={it.key} item={it.f} now={now} flash={flash === `f${it.f.id}`} decision={it.f.event === 'decision_asked' ? open.get(it.f.ref ?? 0) : undefined} />
            ),
          )}
          {unplaced.map((x) => (
            <div key={`d${x.id}`} id={threadId(`d${x.id}`)} className={`trow trow-decision${flash === `d${x.id}` ? ' is-flash' : ''}`}>
              <Glyph glyph={MARKS.decision_asked.glyph} tone="warn" label="decision waiting" />
              <DecisionCard d={x} variant="inline" />
            </div>
          ))}
          {d.partial !== '' && (
            <div className="msg msg-assistant is-partial">
              <span className="msg-who">Front desk</span>
              <Markdown text={d.partial} />
            </div>
          )}
        </div>

        <PendingStrip />
        <Composer thread={thread} workspaceId={workspaceId} d={d} disabled={stopped} following={follow.following} topicId={topicId} onSend={follow.jump} />
        </div>
        </ErrorBoundary>
      </div>
    </div>
  )
}

export { ChatThread, FeedRow, DeskRow }
