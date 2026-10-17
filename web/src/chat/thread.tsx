import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { DeskQuestion, WaitingItem } from '../api/types'
import type { FeedEntry } from '../model/feed'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { since } from '../format'
import { EventGlyph } from '../components/Glyph'
import { inTopic, memberKeys, readTopicFilter, topicByTurn, withTopicFilter, type TopicView } from '../model/topic'
import { deskQuestions, topics as topicsApi } from '../api/client'
import { href, parse } from '../router'
import { useSnapshot } from '../state/context'
import { DeskThread, SIGN_IN_HINT, timeline, type ThreadItem } from '../state/desk'
import { useNow } from '../hooks'
import { useFollowBottom, Countdown, useThread, STREAM_LINE } from './md'
import { Button } from '../ui'
import { cx } from '../ui/cx'
import { Thread, ThreadRow, WorkFold, type ThreadEntry } from '../ui/Thread'
import { Composer } from './composer'
import { groupThread, rowFeedIds } from './group'
import { closedQuestions, questionsByMessage, replyJumpNote, replyRef } from './reply'
import { PendingStrip } from './strip'
import { deskEntry, feedEntry, workSteps } from './rows'
import { TopicChips } from './TopicChips'
import { backlogItems, threadId } from '../model/backlog'
import { takeStep, STEP_EVENT, stepAnchor } from '../model/waitingStep'
import { isUnseen, useSeen } from '../state/seen'
import { Announcer, AwayBlock, useSeenWatch } from './seen'
import { DecisionCard } from '../components/DecisionCard'
import { NotifyToggle } from './notify'
import './convo.css'

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
  const id = threadId(`u${feed[0]?.id}`)
  return (
    <div id={id} className="sh-updates">
      <button type="button" className="sh-updates-toggle" aria-expanded={shown} onClick={() => setShown(!shown)}>
        <span className="sh-updates-glyphs" aria-hidden="true">
          {feed.slice(0, 8).map((f) => (
            <EventGlyph key={f.id} event={f.event} />
          ))}
        </span>
        <span>{feed.length} updates</span>
        <span className="sh-event-time">{since(feed[feed.length - 1]?.created ?? '', now)}</span>
      </button>
      {shown && (
        <div className="sh-updates-body">
          {feed.map((f) => (
            <ThreadRow key={f.id} it={feedEntry({ f, now, flash: flash === `f${f.id}` })} />
          ))}
        </div>
      )}
    </div>
  )
}

function ChatThread({
  thread: make, workspaceId, children, side = false, topic: fixedTopic, keys = true,
}: {
  thread: (workspaceId: number) => DeskThread
  workspaceId: number
  children?: ReactNode
  /** The column beside a main-body page: no topic chips, no "Since you were away", as wide as its panel. */
  side?: boolean
  /** A topic the thread is held to, whatever the address says (null: all of it). */
  topic?: number | null
  /** The pending bar answers `]` and `[`; off where the page beside has its own. */
  keys?: boolean
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
  const [menu, setMenu] = useState(false)
  const [topics, setTopics] = useState(false)
  const moreRoot = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (!menu) return
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      setMenu(false)
      moreRoot.current?.querySelector('button')?.focus()
    }
    const away = (e: MouseEvent) => {
      if (moreRoot.current && !moreRoot.current.contains(e.target as Node)) setMenu(false)
    }
    document.addEventListener('keydown', key)
    document.addEventListener('mousedown', away)
    return () => {
      document.removeEventListener('keydown', key)
      document.removeEventListener('mousedown', away)
    }
  }, [menu])
  const wasConfirming = useRef(false)
  const confirmRoot = useRef<HTMLSpanElement>(null)
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null)

  const seen = useSeen()
  const waitingItems = live.waiting?.items ?? null
  const [hash, setHash] = useState(window.location.hash)
  useEffect(() => {
    const on = () => setHash(window.location.hash)
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  const { away, dismiss: dismissAway } = useSeenWatch(waitingItems, hash)
  const unseenDecisions = useMemo(() => new Set((waitingItems ?? []).flatMap((w) => (w.decision_id && isUnseen(seen, w) ? [w.decision_id] : []))), [waitingItems, seen])
  const unseenTurns = useMemo(() => new Set((waitingItems ?? []).flatMap((w) => (w.question_id && w.turn !== undefined && isUnseen(seen, w) ? [w.turn] : []))), [waitingItems, seen])
  const unseenQuestions = useMemo(() => new Set((waitingItems ?? []).flatMap((w) => (w.question_id && isUnseen(seen, w) ? [w.question_id] : []))), [waitingItems, seen])

  const all = useMemo(() => timeline(d.events, live.feed), [d.events, live.feed])
  // The topic filter is a view over the one thread, kept in the hash so it can be linked
  // and survives a reload; the desk and history are never filtered.
  const [hashTopic, setTopicId] = useState<number | null>(() => readTopicFilter(window.location.hash))
  const topicId = fixedTopic !== undefined ? fixedTopic : hashTopic
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
    closedQuestions(deskQuestions.list).then(
      (qs) => setAnswered(qs),
      () => missing.forEach((id) => asked.current.delete(id)),
    )
  }, [items, questions, openQs, d.loaded])
  // A question sits under the last desk message of the turn it was asked in.
  const questionsAt = useMemo(() => {
    return questionsByMessage(items.flatMap((it) => (it.type === 'desk' ? [it.e] : [])), questions)
  }, [items, questions])

  // Where each lane last shows in the loaded thread: the header counter's arrows use it to land an
  // MR or ship item; a lane with no loaded row has none.
  const laneAnchors = useMemo(() => {
    const shownFeed = new Set(items.flatMap((it) => (it.type === 'feed' ? [it.f.id] : [])))
    const out = new Map<number, string>()
    // The first item for a lane wins, as the lookup it replaces did.
    for (const b of backlogItems({ ...live, shown: shownFeed })) {
      if (b.laneId !== undefined && b.anchor && !out.has(b.laneId)) out.set(b.laneId, b.anchor)
    }
    return out
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
      laneAnchor: (id) => laneAnchors.get(id),
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
    if (confirming) confirmRoot.current?.querySelector('button')?.focus()
    // The More button remounts when the confirmation closes; focus it once it is there.
    else if (wasConfirming.current) moreRoot.current?.querySelector('button')?.focus()
    wasConfirming.current = confirming
  }, [confirming])

  const line = d.connection === 'live' ? null : STREAM_LINE[d.connection]
  const offline = d.connection === 'reconnecting'
  const stopped = d.connection === 'signed-out' || d.connection === 'refused'

  // The topics holding something that waits on the person and has not been opened.
  const unseenTopics = new Set(
    topicList.flatMap((t) => (t.links.some((l) => (l.kind === 'decision' && unseenDecisions.has(Number(l.ref))) || (l.kind === 'turn' && unseenTurns.has(Number(l.ref)))) ? [t.id] : [])),
  )
  const openDecision = parse(hash).open?.kind === 'decision' ? parse(hash).open?.id : undefined

  // The rows, as the shared thread draws them. A row's time is kept to place "n new".
  const entries: ThreadEntry[] = []
  const times: number[] = []
  const add = (e: ThreadEntry | null, created: string | undefined) => {
    if (!e) return
    entries.push(e)
    times.push(created ? Date.parse(created) : 0)
  }
  for (const r of rows) {
    if (r.type === 'updates') {
      add({ kind: 'node', key: r.key, children: <UpdatesFold feed={r.feed} now={now} flash={flash} reveal={reveal} /> }, r.feed[0]?.created)
      continue
    }
    if (r.type === 'work') {
      add({ kind: 'node', key: r.key, children: <WorkFold steps={workSteps(r.work, now)} /> }, r.work[0]?.created)
      continue
    }
    const it: ThreadItem = r.type === 'reply' ? { type: 'desk', key: r.key, e: r.e } : r.it
    if (it.type === 'desk') {
      add(
        deskEntry({
          e: it.e, key: it.key, now, work: r.type === 'reply' ? r.work : undefined,
          topic: it.e.turn === undefined ? undefined : byTurn.get(it.e.turn),
          activeTopic: topicId, onTopic: side ? undefined : filterTo,
          questions: questionsAt.get(it.e.seq), flash, thread, topicFilter: topicId, onCleared: readQuestions, onReplyRef: jumpToQuestion,
          unseen: (questionsAt.get(it.e.seq) ?? []).some((q) => unseenQuestions.has(q.id)),
        }),
        it.e.created,
      )
    } else {
      const asked = it.f.event === 'decision_asked' ? open.get(it.f.ref ?? 0) : undefined
      add(
        feedEntry({
          f: it.f, now, flash: flash === `f${it.f.id}`, decision: asked, selected: asked !== undefined && asked.id === openDecision,
          unseen: it.f.event === 'decision_asked' && unseenDecisions.has(it.f.ref ?? 0),
        }),
        it.f.created,
      )
    }
  }
  for (const x of unplaced) {
    add(
      { kind: 'decision', key: `d${x.id}`, anchor: `d${x.id}`, flash: flash === `d${x.id}`, children: <DecisionCard d={x} variant="inline" unseen={unseenDecisions.has(x.id)} selected={x.id === openDecision} /> },
      undefined,
    )
  }
  if (d.partial !== '') add({ kind: 'desk', key: 'partial', text: d.partial, partial: true }, undefined)
  // Scrolled up, the reader has a way back to the newest message, which stays in view as the thread scrolls.
  if (!follow.following) {
    add({ kind: 'node', key: 'jump', children: <Button className="sh-conv-jump" onClick={follow.jump}>Jump to latest</Button> }, undefined)
  }
  // "n new": everything from the first row newer than the person's last look.
  if (away !== null && !side) {
    const at = times.findIndex((t) => t > away)
    if (at >= 0) entries.splice(at, 0, { kind: 'new', key: 'new', label: `${entries.length - at} new` })
  }

  const before = (
    <>
      {topicId !== null && !side && (
        <p className="sh-conv-note" role="status">
          Showing: {topic && topic.id === topicId ? topic.title : `topic ${topicId}`}, {items.length} {items.length === 1 ? 'row' : 'rows'}.{' '}
          <button type="button" className="sh-link" onClick={() => filterTo(null)}>
            Show all
          </button>
        </p>
      )}
      {d.more && (
        <p className="sh-conv-note">
          <button type="button" className="sh-link" onClick={() => void thread.loadEarlier()}>
            Load earlier messages
          </button>
        </p>
      )}
      {replyNote !== '' && (
        <p className="sh-conv-note" role="status">
          {replyNote}{' '}
          <button type="button" className="sh-link" onClick={() => setReplyNote('')}>
            Dismiss
          </button>
        </p>
      )}
      {stepNote && (
        <p className="sh-conv-note" role="status">
          Not in the loaded thread: {stepNote.title}.{' '}
          {stepNote.action === 'review' && stepNote.next_id ? <a href={href.next(stepNote.next_id)}>Review</a> : null}
          {stepNote.lane_id ? <a href={href.lane(stepNote.lane_id)}>Open lane</a> : null}{' '}
          <button type="button" className="sh-link" onClick={() => setStepNote(null)}>
            Dismiss
          </button>
        </p>
      )}
      {!side && <AwayBlock since={away} items={away === null ? [] : (waitingItems ?? [])} feed={live.feed} onDismiss={dismissAway} now={now} />}
    </>
  )

  return (
    <div className={cx('page chat sh-conv', side && 'sh-conv--side')} data-component="Conversation" data-layout={side ? 'side' : 'main'}>
      <header className="sh-head sh-conv-head">
        {side ? <h2 className="sh-conv-title">Conversation</h2> : <h1>Conversation</h1>}
        {side ? (
          topicId !== null && <span className="sh-conv-topic" title={topic?.title}>{topic?.title ?? `topic ${topicId}`}</span>
        ) : (
          <ErrorBoundary region="The topics">
            <TopicChips active={topicId} onFilter={filterTo} manage={topics} unseen={unseenTopics} />
          </ErrorBoundary>
        )}
        {children}
        <span className="sh-head-right" role="status">
          {offline ? 'Reconnecting…' : d.busy ? (d.queued > 1 ? `Working, ${d.queued - 1} waiting…` : 'Working…') : 'Front desk is idle'}
        </span>
        <span className="sh-conv-tools">
          {offline && (
            <Button size="small" onClick={() => thread.wake()}>
              Retry now
            </Button>
          )}
          {confirming ? (
            <span className="sh-conv-confirm" ref={confirmRoot} role="group" aria-label="Start a new conversation?">
              <span>Start fresh? The desk forgets this conversation&rsquo;s context.</span>
              <Button
                size="small"
                variant="primary"
                onClick={() => {
                  setConfirming(false)
                  void thread.newConversation()
                }}
              >
                Start new
              </Button>
              <Button
                size="small"
                onClick={() => {
                  setConfirming(false)
                }}
              >
                Cancel
              </Button>
            </span>
          ) : (
            <span className="sh-conv-more" ref={moreRoot}>
              <Button size="small" variant="ghost" aria-label="More" aria-haspopup="true" aria-expanded={menu} onClick={() => setMenu(!menu)}>
                &hellip;
              </Button>
              {menu && (
                <div className="sh-conv-menu" role="group" aria-label="Conversation options">
                  <NotifyToggle />
                  {!side && (
                    <Button size="small" aria-pressed={topics} onClick={() => { setMenu(false); setTopics(!topics) }}>
                      {topics ? 'Hide topics' : 'All topics'}
                    </Button>
                  )}
                  <Button size="small" onClick={() => { setMenu(false); setConfirming(true) }}>
                    New conversation
                  </Button>
                </div>
              )}
            </span>
          )}
        </span>
      </header>

      {stopped && !d.problem && (
        <p className="sh-conv-problem" role="alert">
          {d.loadError ?? `You are signed out. ${SIGN_IN_HINT}`}
        </p>
      )}
      {offline && d.loaded && (
        <p className="sh-conv-problem" role="alert">
          The daemon is not answering, so this conversation may be out of date. It reconnects by itself
          {d.failures >= 3 ? `; if it keeps failing the daemon may have moved: ${SIGN_IN_HINT}` : '.'}
        </p>
      )}
      {d.loadError && !stopped && !d.loaded && (
        <p className="sh-conv-problem" role="alert">
          Could not read the conversation: {d.loadError}. Retrying.
        </p>
      )}

      <ErrorBoundary region="The conversation">
        <div className="sh-conv-body">
          <Thread
            items={entries}
            scrollRef={setScroller}
            layout={side ? 'side' : 'main'}
            busy={!d.loaded}
            before={before}
            empty={d.loaded && d.partial === '' ? <p className="sh-conv-empty">Nothing said yet. Ask the front desk what the flock is doing.</p> : null}
          />
          <Announcer items={waitingItems} />
          <PendingStrip keys={keys} />
          {/* Always mounted, so a change of its text is announced; empty while the stream is live. */}
          <p className={cx('sh-conv-stream', line?.down && 'is-down')} role="status">
            {line && (
              <>
                {line.text}
                {offline && <Countdown at={d.retryAt} />}
              </>
            )}
          </p>
          <Composer thread={thread} workspaceId={workspaceId} d={d} disabled={stopped} topicId={topicId} onSend={follow.jump} />
        </div>
      </ErrorBoundary>
    </div>
  )
}

export { ChatThread }
