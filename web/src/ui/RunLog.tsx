import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { RunState } from '../api/types'
import { useKeys } from '../hooks'
import { parseLog, type Block } from '../model/log'

const LOG_CHUNK = 64 << 10
const LOG_POLL = { visible: 1500, hidden: 10_000 }

function useRunLog(id: number) {
  const [text, setText] = useState('')
  const [done, setDone] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let stopped = false
    let timer = 0
    let offset = 0
    const read = async () => {
      try {
        for (;;) {
          const chunk = await api.runLog(id, offset)
          if (stopped) return
          const read = chunk.offset - offset
          offset = chunk.offset
          if (chunk.data) setText((t) => t + chunk.data)
          if (chunk.done) {
            setDone(true)
            return
          }
          if (read < LOG_CHUNK) break
        }
        setError(null)
      } catch (e) {
        if (!stopped) setError(e instanceof Error ? e.message : String(e))
      }
      if (!stopped) timer = window.setTimeout(read, document.visibilityState === 'hidden' ? LOG_POLL.hidden : LOG_POLL.visible)
    }
    void read()
    return () => {
      stopped = true
      window.clearTimeout(timer)
    }
  }, [id])
  return { text, done, error }
}

type Variant = 'finished' | 'live' | 'transcript'

/**
 * A run's log, shared between the run page and the lane inspector. `live` keeps following if the
 * run is running; `transcript` hides the search and follow controls.
 */
export function RunLog({ id, state, variant = 'finished', active = true, onDone }: {
  id: number
  state?: RunState
  variant?: Variant
  active?: boolean
  onDone?: () => void
}) {
  const { text, done, error } = useRunLog(id)
  const [follow, setFollow] = useState(true)
  const [query, setQuery] = useState('')
  const [hit, setHit] = useState(0)
  const [openAll, setOpenAll] = useState(false)
  const search = useRef<HTMLInputElement>(null)
  const body = useRef<HTMLDivElement>(null)
  const end = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (done) onDone?.()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [done])

  const blocks = useMemo(() => parseLog(text), [text])
  const q = query.trim().toLowerCase()
  const hits = useMemo(() => (q ? countHits(text.toLowerCase(), q) : 0), [text, q])

  const running = state === 'running' && !done
  useEffect(() => {
    if (active && follow && running && !q) end.current?.scrollIntoView?.({ block: 'end' })
  }, [text, follow, running, q, active])
  useEffect(() => {
    const onScroll = () => {
      const bottom = end.current?.getBoundingClientRect().bottom ?? 0
      if (bottom > window.innerHeight + 40) setFollow(false)
    }
    window.addEventListener('wheel', onScroll, { passive: true })
    window.addEventListener('touchmove', onScroll, { passive: true })
    return () => {
      window.removeEventListener('wheel', onScroll)
      window.removeEventListener('touchmove', onScroll)
    }
  }, [])

  const jump = useCallback((i: number) => {
    const marks = body.current?.querySelectorAll('mark')
    if (!marks || marks.length === 0) return
    const n = ((i % marks.length) + marks.length) % marks.length
    marks.forEach((m, j) => m.classList.toggle('is-current', j === n))
    marks[n]?.scrollIntoView({ block: 'center' })
    setHit(n)
    setFollow(false)
  }, [])
  useEffect(() => {
    if (q) jump(0)
  }, [q, jump])

  useKeys(
    active && variant !== 'transcript'
      ? {
          '/': (e) => {
            e.preventDefault()
            search.current?.focus()
          },
          f: () => setFollow((f) => !f),
          t: () => setOpenAll((o) => !o),
          n: () => jump(hit + 1),
          N: () => jump(hit - 1),
        }
      : {},
    [hit, jump, active, variant],
  )

  return (
    <div className="sh-run-log" data-component="RunLog" data-variant={variant}>
      {variant !== 'transcript' && (
        <div className="log-tools" role="toolbar" aria-label="Log">
          <label className="log-search">
            <span className="visually-hidden">Search the log</span>
            <input
              ref={search}
              type="search"
              placeholder="Search  /"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  jump(hit + (e.shiftKey ? -1 : 1))
                } else if (e.key === 'Escape') {
                  setQuery('')
                  e.currentTarget.blur()
                }
              }}
            />
          </label>
          {q && <span className="faint" aria-live="polite">{hits === 0 ? 'no match' : `${Math.min(hit + 1, hits)} of ${hits}`}</span>}
          <span className="log-tools-gap" />
          <button type="button" className="link-button" aria-pressed={openAll} onClick={() => setOpenAll((o) => !o)}>
            {openAll ? 'Collapse tool calls' : 'Expand tool calls'} <kbd>t</kbd>
          </button>
          <button
            type="button"
            className="link-button"
            aria-pressed={follow}
            disabled={!running}
            onClick={() => setFollow((f) => !f)}
            title={running ? undefined : 'The run has ended'}
          >
            {follow && running ? 'Following' : 'Follow'} <kbd>f</kbd>
          </button>
        </div>
      )}

      <div ref={body} className="log-body">
        {error && <p className="tone-bad">Could not read the log: {error}. Retrying.</p>}
        {!error && text === '' && <p className="faint">{done ? 'The log is empty.' : 'Waiting for output…'}</p>}
        {blocks.map((b, i) => (
          <LogBlock key={i} block={b} q={q} openAll={openAll} />
        ))}
        {running && (
          <p className="log-live faint" aria-live="off">
            <span className="glyph" aria-hidden="true">▸</span> running
          </p>
        )}
        {done && text !== '' && <p className="log-end faint">End of log.</p>}
      </div>
      <div ref={end} />
    </div>
  )
}

function LogBlock({ block, q, openAll }: { block: Block; q: string; openAll: boolean }) {
  if (block.kind === 'header') {
    return <pre className="log-header">{mark(block.lines.join('\n'), q)}</pre>
  }
  if (block.kind === 'text') {
    return <pre className="log-text">{mark(block.lines.join('\n'), q)}</pre>
  }
  const matches = q !== '' && block.calls.some((c) => (c.tool + ' ' + c.input).toLowerCase().includes(q))
  const names = [...new Set(block.calls.map((c) => c.tool))]
  return (
    <details className="log-tools-block" open={openAll || matches}>
      <summary>
        <span className="glyph" aria-hidden="true">→</span>
        {block.calls.length === 1 ? (
          <span>
            <strong>{block.calls[0]?.tool}</strong> <span className="faint">{block.calls[0]?.summary}</span>
          </span>
        ) : (
          <span>
            {block.calls.length} tool calls <span className="faint">{names.slice(0, 5).join(', ')}{names.length > 5 ? ', …' : ''}</span>
          </span>
        )}
      </summary>
      <ol>
        {block.calls.map((c, i) => (
          <li key={i}>
            <strong>{mark(c.tool, q)}</strong> <span className="log-input">{mark(c.input, q)}</span>
          </li>
        ))}
      </ol>
    </details>
  )
}

function mark(text: string, q: string): ReactNode {
  if (!q) return text
  const lower = text.toLowerCase()
  const out: ReactNode[] = []
  let at = 0
  for (let i = lower.indexOf(q); i >= 0; i = lower.indexOf(q, i + q.length)) {
    out.push(text.slice(at, i), <mark key={i}>{text.slice(i, i + q.length)}</mark>)
    at = i + q.length
  }
  out.push(text.slice(at))
  return out
}

function countHits(text: string, q: string): number {
  let n = 0
  for (let i = text.indexOf(q); i >= 0; i = text.indexOf(q, i + q.length)) n++
  return n
}
