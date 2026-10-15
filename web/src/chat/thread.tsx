import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { DecisionView, DeskEvent, DeskQuestion, WaitingItem } from '../api/types'
import type { FeedEntry } from '../model/feed'
import { Backlog } from '../components/Backlog'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { TopicBar } from '../components/TopicBar'
import { TopicPill } from '../components/TopicPill'
import { DecisionCard, splitQuestion } from '../components/DecisionCard'
import { EventGlyph, Glyph } from '../components/Glyph'
import { since } from '../format'
import { MARKS } from '../model/marks'
import { inTopic, memberKeys, readTopicFilter, topicByTurn, withTopicFilter, type TopicView } from '../model/topic'
import { deskQuestions, topics as topicsApi } from '../api/client'
import { href } from '../router'
import { useSnapshot } from '../state/context'
import { DeskThread, SIGN_IN_HINT, timeline, type ThreadItem } from '../state/desk'
import { useNow } from '../hooks'
import { useFollowBottom, Countdown, useThread, CONNECTION } from './md'
import { Composer } from './composer'
import { groupThread, rowFeedIds } from './group'
import { QuestionActions } from './questions'
import { questionsByMessage, replyJumpNote, replyRef } from './reply'
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

function ToolLine({ text }: { text: string }) {
  return (
    <p className="trow trow-tool faint">
      <Glyph glyph={MARKS.report.glyph} tone="muted" label="tool call" />
      <span className="mono">{text}</span>
    </p>
  )
}

function WokeNote({ text, when }: { text: string; when: ReactNode }) {
  return (
    <details className="trow trow-system">
      <summary>
        <Glyph glyph={MARKS.info.glyph} tone="muted" label="note" /> Shepherd woke the front desk {when}
      </summary>
      <p className="msg-text">{text}</p>
    </details>
  )
}

/** A turn's tool calls and wake notes, folded: "Show work (N)". */
function WorkFold({ work, now, open }: { work: DeskEvent[]; now: number; open?: boolean }) {
  const tools = work.filter((w) => w.kind === 'tool').length
  const woke = work.length - tools
  const label = [tools > 0 ? `${tools} tool ${tools === 1 ? 'call' : 'calls'}` : '', woke > 0 ? 'woke the front desk' : ''].filter(Boolean).join(', ')
  return (
    <details className="work-fold" open={open}>
      <summary>
        <Glyph glyph={MARKS.report.glyph} tone="muted" label="work" /> Show work ({label})
      </summary>
      {work.map((w) =>
        w.kind === 'tool' ? (
          <ToolLine key={w.seq} text={w.text ?? ''} />
        ) : (
          <WokeNote key={w.seq} text={w.text ?? ''} when={<time dateTime={w.created} title={w.created} className="faint trow-age">{since(w.created, now)}</time>} />
        ),
      )}
    </details>
  )
}

/** Three or more events between messages as one row: their glyphs, expandable. */
function UpdatesFold({
  feed, now, flash, reveal,
}: {
  feed: FeedEntry[]
  now: number
  flash: string
  /** An anchor a jump wants shown; the fold holding it opens. */
  reveal: string
}) {
  const [shown, setShown] = useState(false)
  // A jump to a row inside opens the fold and it stays open; the toggle acts on what shows.
  if (!shown && reveal !== '' && feed.some((f) => `f${f.id}` === reveal)) setShown(true)
  const isOpen = shown
  const id = threadId(`u${feed[0]?.id}`)
  return (
    <div id={id} className="updates-fold">
      <button type="button" className="updates-toggle" aria-expanded={isOpen} onClick={() => setShown(!isOpen)}>
        <span className="updates-glyphs" aria-hidden="true">
          {feed.slice(0, 8).map((f) => (
            <EventGlyph key={f.id} event={f.event} />
          ))}
        </span>
        <span>{feed.length} updates</span>
        <span className="faint trow-age">{since(feed[feed.length - 1]?.created ?? '', now)}</span>
      </button>
      {isOpen && feed.map((f) => <FeedRow key={f.id} item={f} now={now} flash={flash === `f${f.id}`} />)}
    </div>
  )
}

function DeskRow({
  e, now, topic, activeTopic = null, onTopic, questions = [], flash = '', thread, topicFilter = null, onCleared, work = [], onReplyRef,
}: {
  e: DeskEvent
  /** Tool calls and wake notes of this reply's turn, folded above it. */
  work?: DeskEvent[]
  now: number
  /** Desk questions asked in this message's turn, of any state: each is an anchor, the open ones carry actions. */
  questions?: DeskQuestion[]
  /** Scrolls to the row of the question a reply names; false when the thread holds none. */
  onReplyRef?: (questionId: number) => void
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
    case 'user': {
      const ref = replyRef(text)
      return (
        <div className="msg msg-user">
          <span className="msg-who">You {when}{pill}</span>
          {ref !== null && onReplyRef && (
            <button type="button" className="link-button msg-replyref" onClick={() => onReplyRef(ref)}>
              Replying to your question {ref}
            </button>
          )}
          <p className="msg-text">{text}</p>
        </div>
      )
    }
    case 'assistant': {
      const asked = questions[0]
      const waiting = questions.some((q) => q.state === 'open')
      return (
        <div
          id={asked ? threadId(`q${asked.id}`) : undefined}
          className={`msg msg-assistant${waiting ? ' msg-asked' : ''}${asked && flash === `q${asked.id}` ? ' is-flash' : ''}`}
        >
          {work.length > 0 && <WorkFold work={work} now={now} />}
          <span className="msg-who">Front desk {when}{pill}</span>
          <Markdown text={text} />
          {questions.slice(1).map((q) => (
            <span key={`a${q.id}`} id={threadId(`q${q.id}`)} />
          ))}
          {thread && questions.filter((q) => q.state === 'open').map((q) => (
            <QuestionActions key={q.id} q={q} thread={thread} topicId={topicFilter} onCleared={() => onCleared?.()} />
          ))}
        </div>
      )
    }
    case 'system':
      return <WokeNote text={text} when={when} />
    case 'tool':
      return <ToolLine text={text} />
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
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null)

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
  const rows = useMemo(() => groupThread(items), [items])
  const open = useMemo(() => new Map(live.decisions.map((x) => [x.id, x])), [live.decisions])
  // Open decisions whose "asked" row is not in the loaded thread still wait on the person.
  const shown = new Set(items.flatMap((it) => (it.type === 'feed' && it.f.event === 'decision_asked' ? [it.f.ref ?? 0] : [])))
  const unplaced = live.decisions.filter((x) => !shown.has(x.id))

  // The questions the desk asked that are still open: each shows Reply here and Skip under
  // the desk message of its turn. Read on the waiting list's cadence and after each action.
  const [openQs, setOpenQs] = useState<DeskQuestion[]>([])
  // Answered ones are read only for a reply in the thread that names one not yet known, so
  // the poll stays the size of the waiting list however long the history grows.
  const [answered, setAnswered] = useState<DeskQuestion[]>([])
  const asked = useRef(new Set<number>())
  const questions = useMemo(() => [...openQs, ...answered.filter((a) => !openQs.some((o) => o.id === a.id))], [openQs, answered])
  const readQuestions = useCallback(() => {
    deskQuestions.list('open').then(
      (qs) => setOpenQs(qs),
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
  useEffect(() => {
    const known = new Set(questions.map((q) => q.id))
    const missing = items.flatMap((it) => {
      const ref = it.type === 'desk' && it.e.kind === 'user' ? replyRef(it.e.text ?? '') : null
      return ref !== null && !known.has(ref) && !asked.current.has(ref) ? [ref] : []
    })
    if (missing.length === 0 || openQs.length === 0 && !d.loaded) return
    missing.forEach((id) => asked.current.add(id))
    deskQuestions.list('answered').then(
      (qs) => setAnswered(qs),
      () => missing.forEach((id) => asked.current.delete(id)),
    )
  }, [items, questions, openQs, d.loaded])
  // A question sits under the last desk message of the turn it was asked in.
  const questionsAt = useMemo(() => {
    return questionsByMessage(items.flatMap((it) => (it.type === 'desk' ? [it.e] : [])), questions)
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
  // A jump to a row folded into an "N updates" row asks that fold to open, then scrolls.
  const [reveal, setReveal] = useState('')
  const inFold = useCallback(
    (anchor: string) => rows.some((r) => r.type === 'updates' && rowFeedIds(r).some((id) => `f${id}` === anchor)),
    [rows],
  )
  const land = useCallback((anchor: string) => {
    const el = document.getElementById(threadId(anchor))
    if (!el) return false
    el.scrollIntoView?.({ block: 'center' })
    setFlash(anchor)
    window.setTimeout(() => setFlash((f) => (f === anchor ? '' : f)), 2500)
    return true
  }, [])
  // A reply names its question: scroll to the message that asked it, or say it is not loaded.
  const [replyNote, setReplyNote] = useState('')
  const jumpToQuestion = useCallback(
    (id: number) => {
      setReplyNote(replyJumpNote(id, land(`q${id}`)))
    },
    [land],
  )
  const jump = useCallback(
    (anchor: string) => {
      if (!land(anchor) && inFold(anchor)) setReveal(anchor)
    },
    [land, inFold],
  )
  useEffect(() => {
    if (reveal === '') return
    const at = window.setTimeout(() => land(reveal), 0)
    const done = window.setTimeout(() => setReveal(''), 2500)
    return () => {
      window.clearTimeout(at)
      window.clearTimeout(done)
    }
  }, [reveal, land])

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
    if (anchor && (document.getElementById(threadId(anchor)) || inFold(anchor))) {
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

  const follow = useFollowBottom(scroller, [d.loaded, items.length, d.partial, unplaced.length])
  useEffect(() => {
    if (confirming) confirmButton.current?.focus()
  }, [confirming])

  const conn = CONNECTION[d.connection]
  const offline = d.connection === 'reconnecting'
  const stopped = d.connection === 'signed-out' || d.connection === 'refused'

  return (
    <div className="page chat">
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

        {replyNote !== '' && (
          <p className="topic-filter" role="status">
            {replyNote}{' '}
            <button type="button" className="link-button" onClick={() => setReplyNote('')}>
              Dismiss
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
        <div ref={setScroller} className="thread" role="log" aria-live="polite" aria-relevant="additions" aria-label="Conversation with the front desk" aria-busy={!d.loaded} tabIndex={0}>
          <div className="thread-body">
          {d.loaded && items.length === 0 && d.partial === '' && unplaced.length === 0 && (
            <p className="faint thread-empty">Nothing said yet. Ask the front desk what the flock is doing.</p>
          )}
          {rows.map((r) => {
            if (r.type === 'updates') return <UpdatesFold key={r.key} feed={r.feed} now={now} flash={flash} reveal={reveal} />
            if (r.type === 'work') return <WorkFold key={r.key} work={r.work} now={now} />
            const it: ThreadItem = r.type === 'reply' ? { type: 'desk', key: r.key, e: r.e } : r.it
            const work = r.type === 'reply' ? r.work : undefined
            return it.type === 'desk' ? (
              <DeskRow
                key={it.key}
                work={work}
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
                onReplyRef={jumpToQuestion}
              />
            ) : (
              <FeedRow key={it.key} item={it.f} now={now} flash={flash === `f${it.f.id}`} decision={it.f.event === 'decision_asked' ? open.get(it.f.ref ?? 0) : undefined} />
            )
          })}
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
