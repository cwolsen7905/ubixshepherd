import { Fragment, useCallback, useEffect, useRef, useSyncExternalStore, useState, type ReactNode, type RefObject } from 'react'
import { useNow } from '../hooks'
import type { Connection } from '../state/desk'
import { DeskThread, type DeskSnapshot } from '../state/desk'

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

export const CONNECTION: Record<Connection, { text: string; cls: string; glyph: string }> = {
  connecting: { text: 'Connecting…', cls: '', glyph: '○' },
  live: { text: 'Live', cls: 'is-up', glyph: '●' },
  reconnecting: { text: 'Offline', cls: 'is-down', glyph: '○' },
  'signed-out': { text: 'Signed out', cls: 'is-down', glyph: '○' },
  refused: { text: 'Refused', cls: 'is-down', glyph: '○' },
}

/** ", retrying in 4s" until `at`; the one part of the page that ticks every second. */
export function Countdown({ at }: { at: number | null }) {
  const now = useNow(1000)
  if (at === null) return <>, reconnecting…</>
  const s = Math.max(0, Math.ceil((at - now) / 1000))
  return <>{s > 0 ? `, retrying in ${s}s` : ', retrying…'}</>
}

export function useThread(thread: DeskThread): DeskSnapshot {
  return useSyncExternalStore(thread.subscribe, thread.getSnapshot)
}

/** How close to the bottom, in px, still counts as being at it. */
export const NEAR_BOTTOM = 24

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

export function clipHead(q: string) {
  const one = q.replace(/\s+/g, ' ').trim()
  return one.length > 120 ? one.slice(0, 119) + '…' : one
}
