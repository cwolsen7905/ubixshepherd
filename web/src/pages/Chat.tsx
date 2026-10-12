import { Fragment, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode, type RefObject } from 'react'
import type { DecisionView, DeskEvent } from '../api/types'
import { Backlog } from '../components/Backlog'
import { TopicBar } from '../components/TopicBar'
import { Topics } from '../components/Topics'
import { TopicPill } from '../components/TopicPill'
import { DecisionCard, splitQuestion } from '../components/DecisionCard'
import { EventGlyph, Glyph } from '../components/Glyph'
import { since } from '../format'
import { useNow } from '../hooks'
import { DRAFT_EVENT, clearDraft, draftKey, prefillDraft, readDraft, writeDraft } from '../model/chatDraft'
import { backlogItems, threadId } from '../model/backlog'
import { MARKS } from '../model/marks'
import { inTopic, memberKeys, readTopicFilter, topicByTurn, withTopicFilter, type TopicView } from '../model/topic'
import { topics as topicsApi } from '../api/client'
import { href } from '../router'
import { useSnapshot } from '../state/context'
import { DeskThread, SIGN_IN_HINT, timeline, type Connection, type DeskSnapshot } from '../state/desk'
import '../styles/chat.css'

// What a model or agent wrote is text, never markup: the page builds elements, so nothing
// of it reaches innerHTML, and a link survives only as an http or https anchor.

/** The URL as an anchor target when it is http or https; null for anything else. */
export function safeHref(raw: string): string | null {
  try {
    const u = new URL(raw)
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.href : null
  } catch {
    return null
  }
}

const INLINE = /(`[^`\n]+`|\*\*[^*\n]+\*\*|\[[^\]\n]+\]\([^)\s]+\))/

function inline(text: string): ReactNode[] {
  return text.split(INLINE).map((part, i) => {
    if (part.length > 2 && part.startsWith('`') && part.endsWith('`')) return <code key={i}>{part.slice(1, -1)}</code>
    if (part.length > 4 && part.startsWith('**') && part.endsWith('**')) return <strong key={i}>{part.slice(2, -2)}</strong>
    const m = /^\[([^\]]+)\]\(([^)\s]+)\)$/.exec(part)
    if (m) {
      const url = safeHref(m[2] ?? '')
      if (url) {
        return (
          <a key={i} href={url} target="_blank" rel="noopener noreferrer">
            {m[1]}
          </a>
        )
      }
      // Not a web link: show what it said, with its target as plain text.
      return <Fragment key={i}>{`${m[1] ?? ''} (${m[2] ?? ''})`}</Fragment>
    }
    return <Fragment key={i}>{part}</Fragment>
  })
}

type Block =
  | { t: 'p'; text: string }
  | { t: 'h'; text: string }
  | { t: 'code'; text: string }
  | { t: 'list'; ordered: boolean; items: string[] }

/** A small markdown subset: paragraphs, headings, fenced code, bullet and numbered lists. */
export function blocks(src: string): Block[] {
  const out: Block[] = []
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  let para: string[] = []
  const flush = () => {
    if (para.length) out.push({ t: 'p', text: para.join('\n') })
    para = []
  }
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? ''
    if (/^\s*```/.test(line)) {
      flush()
      const code: string[] = []
      for (i++; i < lines.length && !/^\s*```/.test(lines[i] ?? ''); i++) code.push(lines[i] ?? '')
      out.push({ t: 'code', text: code.join('\n') })
      continue
    }
    const h = /^#{1,6}\s+(.*)$/.exec(line)
    const li = /^\s*(?:([-*])|\d+[.)])\s+(.*)$/.exec(line)
    if (h) {
      flush()
      out.push({ t: 'h', text: h[1] ?? '' })
    } else if (li) {
      flush()
      const ordered = !li[1]
      const last = out.at(-1)
      if (last?.t === 'list' && last.ordered === ordered) last.items.push(li[2] ?? '')
      else out.push({ t: 'list', ordered, items: [li[2] ?? ''] })
    } else if (line.trim() === '') {
      flush()
    } else {
      para.push(line)
    }
  }
  flush()
  return out
}

export function Markdown({ text }: { text: string }) {
  return (
    <div className="md">
      {blocks(text).map((b, i) => {
        switch (b.t) {
          case 'code':
            return (
              <pre key={i}>
                <code>{b.text}</code>
              </pre>
            )
          case 'h':
            return (
              <p key={i} className="md-h">
                {inline(b.text)}
              </p>
            )
          case 'list': {
            const items = b.items.map((it, j) => <li key={j}>{inline(it)}</li>)
            return b.ordered ? <ol key={i}>{items}</ol> : <ul key={i}>{items}</ul>
          }
          default:
            return <p key={i}>{inline(b.text)}</p>
        }
      })}
    </div>
  )
}

const CONNECTION: Record<Connection, { text: string; cls: string; glyph: string }> = {
  connecting: { text: 'Connecting…', cls: '', glyph: '○' },
  live: { text: 'Live', cls: 'is-up', glyph: '●' },
  reconnecting: { text: 'Offline', cls: 'is-down', glyph: '○' },
  'signed-out': { text: 'Signed out', cls: 'is-down', glyph: '○' },
  refused: { text: 'Refused', cls: 'is-down', glyph: '○' },
}

/** ", retrying in 4s" until `at`; the one part of the page that ticks every second. */
function Countdown({ at }: { at: number | null }) {
  const now = useNow(1000)
  if (at === null) return <>, reconnecting…</>
  const s = Math.max(0, Math.ceil((at - now) / 1000))
  return <>{s > 0 ? `, retrying in ${s}s` : ', retrying…'}</>
}

/** How close to the bottom, in px, still counts as being at it. */
const NEAR_BOTTOM = 24

/**
 * Keeps the page at the bottom of the conversation while the reader is there. Content
 * grows without a render of ours (markdown, a decision card, fonts, a streaming reply),
 * so a ResizeObserver on `target` re-pins; `deps` re-pins on our own changes. The reader
 * leaves by scrolling up (scrollY falls, or the wheel goes up) and returns by reaching the
 * bottom. Our own scrolling and content growth never move scrollY up, so neither unpins.
 */
export function useFollowBottom(target: RefObject<HTMLElement | null>, deps: unknown[]) {
  const pinned = useRef(true)
  const lastY = useRef(0)
  const [following, setFollowing] = useState(true)

  const pin = useCallback((v: boolean) => {
    if (pinned.current !== v) {
      pinned.current = v
      setFollowing(v)
    }
  }, [])
  const toBottom = useCallback(() => {
    const el = document.documentElement
    window.scrollTo({ top: el.scrollHeight })
    lastY.current = Math.max(0, el.scrollHeight - window.innerHeight)
  }, [])
  /** Back to the bottom, following again: the reader's own send, or "Jump to latest". */
  const jump = useCallback(() => {
    pin(true)
    toBottom()
  }, [pin, toBottom])

  useEffect(() => {
    const onScroll = () => {
      const y = window.scrollY
      if (window.innerHeight + y >= document.documentElement.scrollHeight - NEAR_BOTTOM) pin(true)
      else if (y < lastY.current) pin(false)
      lastY.current = y
    }
    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < 0) pin(false)
    }
    const onResize = () => {
      if (pinned.current) toBottom()
    }
    lastY.current = window.scrollY
    window.addEventListener('scroll', onScroll, { passive: true })
    window.addEventListener('wheel', onWheel, { passive: true })
    window.addEventListener('resize', onResize)
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(onResize)
    if (target.current) ro?.observe(target.current)
    return () => {
      window.removeEventListener('scroll', onScroll)
      window.removeEventListener('wheel', onWheel)
      window.removeEventListener('resize', onResize)
      ro?.disconnect()
    }
  }, [target, pin, toBottom])

  useEffect(() => {
    if (pinned.current) toBottom()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toBottom, ...deps])

  return { following, jump }
}

function useThread(thread: DeskThread): DeskSnapshot {
  return useSyncExternalStore(thread.subscribe, thread.getSnapshot)
}

/** The chat page. `make` builds the thread for a workspace; tests give it fakes. */
export function ChatPage({ make = (id: number) => new DeskThread(id) }: { make?: (workspaceId: number) => DeskThread }) {
  const snap = useSnapshot()
  const workspaces = snap.status?.workspaces ?? []
  const [pick, setPick] = useState(0)
  // The daemon takes a workspace_id only when it has more than one.
  const workspaceId = workspaces.length > 1 ? (workspaces.find((w) => w.id === pick) ?? workspaces[0])?.id ?? 0 : 0
  if (!snap.status && !snap.error) return <p className="empty">Connecting to the daemon…</p>
  return (
    <ChatThread key={workspaceId} thread={make} workspaceId={workspaceId}>
      {workspaces.length > 1 && (
        <label className="chat-ws">
          <span className="faint">Workspace</span>
          <select value={workspaceId} onChange={(e) => setPick(Number(e.target.value))}>
            {workspaces.map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        </label>
      )}
    </ChatThread>
  )
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

        <div className="thread" role="log" aria-live="polite" aria-relevant="additions" aria-label="Conversation with the front desk" aria-busy={!d.loaded}>
          {d.loaded && items.length === 0 && d.partial === '' && unplaced.length === 0 && (
            <p className="faint thread-empty">Nothing said yet. Ask the front desk what the flock is doing.</p>
          )}
          {items.map((it) =>
            it.type === 'desk' ? (
              <DeskRow key={it.key} e={it.e} now={now} topic={it.e.turn === undefined ? undefined : byTurn.get(it.e.turn)} activeTopic={topicId} onTopic={filterTo} />
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

        <Composer thread={thread} workspaceId={workspaceId} d={d} disabled={stopped} following={follow.following} topicId={topicId} onSend={follow.jump} />
        </div>
      </div>
    </div>
  )
}

function Composer({
  thread, workspaceId, d, disabled, following, topicId, onSend,
}: {
  thread: DeskThread
  workspaceId: number
  d: DeskSnapshot
  disabled: boolean
  following: boolean
  /** The topic being viewed: the turn is filed under it. */
  topicId: number | null
  onSend: () => void
}) {
  const key = draftKey(workspaceId)
  // The words are kept as they are typed, so leaving the page and coming back loses nothing.
  const [text, setText] = useState(() => readDraft(window.localStorage, key))
  const edit = (v: string) => {
    setText(v)
    writeDraft(window.localStorage, key, v)
  }
  const box = useRef<HTMLTextAreaElement>(null)
  const sending = d.sending
  // Words put in the draft from elsewhere (the rail's Start) show at once.
  useEffect(() => {
    const onDraft = (e: Event) => {
      if ((e as CustomEvent<string>).detail !== key) return
      setText(readDraft(window.localStorage, key))
      box.current?.focus()
    }
    window.addEventListener(DRAFT_EVENT, onDraft)
    return () => window.removeEventListener(DRAFT_EVENT, onDraft)
  }, [key])

  const send = async () => {
    if (sending || disabled || !text.trim()) return
    // The text stays until the daemon has taken it, so a refusal loses nothing.
    onSend()
    if (await thread.send(text, topicId)) {
      setText('')
      clearDraft(window.localStorage, key)
    }
    box.current?.focus()
  }

  return (
    <form
      className="composer"
      onSubmit={(e) => {
        e.preventDefault()
        void send()
      }}
    >
      {!following && (
        <button type="button" className="button chat-jump" onClick={onSend}>
          Jump to latest
        </button>
      )}
      {d.problem && (
        <p className="chat-problem tone-bad" role="alert">
          {d.problem.message}
        </p>
      )}
      {d.notice && !d.problem && (
        <p className="chat-notice faint" role="status">
          {d.notice}
        </p>
      )}
      <label htmlFor="chat-input" className="visually-hidden">
        Message to the front desk
      </label>
      <textarea
        id="chat-input"
        ref={box}
        rows={2}
        value={text}
        readOnly={sending}
        disabled={disabled}
        placeholder="Message the front desk. Enter sends, Shift+Enter adds a line."
        onChange={(e) => edit(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault()
            void send()
          }
        }}
      />
      <div className="composer-bar">
        {d.busy && (
          <button type="button" className="button" onClick={() => void thread.interrupt()}>
            Interrupt
          </button>
        )}
        <button type="submit" className="button button-primary" disabled={sending || disabled || !text.trim()}>
          {sending ? 'Sending…' : 'Send'}
        </button>
      </div>
    </form>
  )
}

function DeskRow({
  e, now, topic, activeTopic = null, onTopic,
}: {
  e: DeskEvent
  now: number
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
    case 'assistant':
      return (
        <div className="msg msg-assistant">
          <span className="msg-who">Front desk {when}{pill}</span>
          <Markdown text={text} />
        </div>
      )
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
          <span>{item.text}</span>{' '}
          {RUN_KINDS.has(item.event) && item.ref ? <a href={href.log(item.ref)}>log</a> : null}{' '}
          <time dateTime={item.created} title={item.created} className="faint trow-age">{age}</time>
        </p>
        {decision && <DecisionCard d={decision} variant="inline" />}
      </div>
    </div>
  )
}

/** The first words of a question that has no summary line, for a one-line place. */
function clipHead(q: string) {
  const one = q.replace(/\s+/g, ' ').trim()
  return one.length > 120 ? one.slice(0, 119) + '…' : one
}
