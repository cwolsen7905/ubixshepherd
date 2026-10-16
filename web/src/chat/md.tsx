import { useCallback, useEffect, useRef, useSyncExternalStore, useState } from 'react'
import { useNow } from '../hooks'
import type { Connection } from '../state/desk'
import { DeskThread, type DeskSnapshot } from '../state/desk'
import { publishChat } from '../shell/chatLink'
import { blocks, Markdown as ConversationMarkdown } from '../conversation/Markdown'

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

/** A small markdown subset: paragraphs, headings, fenced code, lists, tables, blockquotes, horizontal rules and task lists. */
export { blocks }

/** The one renderer (code blocks get a copy button), with the chat page's class names. */
export function Markdown({ text }: { text: string }) {
  return <ConversationMarkdown text={text} classes="md" />
}

/** The line above the composer while the chat stream is not live; nothing says so when it is. */
export const STREAM_LINE: Record<Exclude<Connection, 'live'>, { text: string; down: boolean }> = {
  connecting: { text: 'Chat connecting…', down: false },
  reconnecting: { text: 'Chat reconnecting…', down: false },
  'signed-out': { text: 'Signed out', down: true },
  refused: { text: 'Refused', down: true },
}

/** ", retrying in 4s" until `at`; the one part of the page that ticks every second. */
export function Countdown({ at }: { at: number | null }) {
  const now = useNow(1000)
  if (at === null) return <>, reconnecting…</>
  const s = Math.max(0, Math.ceil((at - now) / 1000))
  return <>{s > 0 ? `, retrying in ${s}s` : ', retrying…'}</>
}

/** Reads the thread, and publishes its connection for the rail's status dot while it is open. */
export function useThread(thread: DeskThread): DeskSnapshot {
  const snap = useSyncExternalStore(thread.subscribe, thread.getSnapshot)
  const { connection, retryAt } = snap
  useEffect(() => {
    publishChat({ connection, retryAt })
  }, [connection, retryAt])
  useEffect(() => () => publishChat(null), [])
  return snap
}

/** How close to the bottom, in px, still counts as being at it. */
export const NEAR_BOTTOM = 24

/**
 * Keeps the thread's own scroll region at the bottom while the reader is there. Content
 * grows without a render of ours (markdown, a decision card, fonts, a streaming reply),
 * so a ResizeObserver on `el` and its children re-pins; `deps` re-pins on our own
 * changes. The reader leaves by scrolling up (scrollTop falls, or the wheel goes up) and
 * returns by reaching the bottom. Our own scrolling and content growth never move
 * scrollTop up, so neither unpins.
 */
export function useFollowBottom(el: HTMLElement | null, deps: unknown[]) {
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
    if (!el) return
    el.scrollTo?.({ top: el.scrollHeight })
    lastY.current = Math.max(0, el.scrollHeight - el.clientHeight)
  }, [el])
  /** Back to the bottom, following again: the reader's own send, or "Jump to latest". */
  const jump = useCallback(() => {
    pin(true)
    toBottom()
  }, [pin, toBottom])

  useEffect(() => {
    if (!el) return
    const onScroll = () => {
      const y = el.scrollTop
      if (el.clientHeight + y >= el.scrollHeight - NEAR_BOTTOM) pin(true)
      else if (y < lastY.current) pin(false)
      lastY.current = y
    }
    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < 0) pin(false)
    }
    const onResize = () => {
      if (pinned.current) toBottom()
    }
    lastY.current = el.scrollTop
    el.addEventListener('scroll', onScroll, { passive: true })
    el.addEventListener('wheel', onWheel, { passive: true })
    window.addEventListener('resize', onResize)
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(onResize)
    ro?.observe(el)
    for (const c of Array.from(el.children)) ro?.observe(c)
    return () => {
      el.removeEventListener('scroll', onScroll)
      el.removeEventListener('wheel', onWheel)
      window.removeEventListener('resize', onResize)
      ro?.disconnect()
    }
  }, [el, pin, toBottom])

  useEffect(() => {
    if (pinned.current) toBottom()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toBottom, ...deps])

  return { following, jump }
}

export function clipHead(q: string) {
  const one = q.replace(/\s+/g, ' ').trim()
  return one.length > 120 ? one.slice(0, 119) + '…' : one
}
