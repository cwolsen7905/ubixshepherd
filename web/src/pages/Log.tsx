import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { RunView } from '../api/types'
import { EventGlyph } from '../components/Glyph'
import { duration, runCost } from '../format'
import { useKeys, useNow } from '../hooks'
import { parseLog, type Block } from '../model/log'
import { href } from '../router'
import { useLive } from '../state/context'
import { RUN_EVENT } from './Lane'

/** The most the daemon returns per read, in bytes. */
const LOG_CHUNK = 64 << 10

/** How often a running run's log is read while following it. */
export const LOG_POLL = { visible: 1500, hidden: 10_000 }

/**
 * Reads a run's log from the offset API (the page is keyed by run, so state starts empty): chunks at once until the end, then, while the
 * run is running, again every so often from where it stopped.
 */
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
          // A full chunk means more is waiting; read it now rather than at the next poll.
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

export function LogPage({ run: id }: { run: number }) {
  const live = useLive()
  const now = useNow(5000)
  const [run, setRun] = useState<RunView | null>(null)
  const { text, done, error } = useRunLog(id)
  const [follow, setFollow] = useState(true)
  const [query, setQuery] = useState('')
  const [hit, setHit] = useState(0)
  const [openAll, setOpenAll] = useState(false)
  const search = useRef<HTMLInputElement>(null)
  const body = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let current = true
    api.run(id).then((r) => current && setRun(r), () => {})
    return () => {
      current = false
    }
  }, [id, done])

  useEffect(() => {
    if (run?.ended) live.markSeen(`run:${run.id}`)
  }, [live, run?.id, run?.ended])

  const blocks = useMemo(() => parseLog(text), [text])
  const q = query.trim().toLowerCase()
  const hits = useMemo(() => (q ? countHits(text.toLowerCase(), q) : 0), [text, q])

  // Following keeps the end of the log in view; scrolling up stops it.
  const running = run?.state === 'running' && !done
  useEffect(() => {
    if (follow && running && !q) window.scrollTo({ top: document.documentElement.scrollHeight })
  }, [text, follow, running, q])
  useEffect(() => {
    const onScroll = () => {
      const atEnd = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 40
      if (!atEnd) setFollow(false)
    }
    window.addEventListener('wheel', onScroll, { passive: true })
    window.addEventListener('touchmove', onScroll, { passive: true })
    return () => {
      window.removeEventListener('wheel', onScroll)
      window.removeEventListener('touchmove', onScroll)
    }
  }, [])

  const jump = useCallback(
    (i: number) => {
      const marks = body.current?.querySelectorAll('mark')
      if (!marks || marks.length === 0) return
      const n = ((i % marks.length) + marks.length) % marks.length
      marks.forEach((m, j) => m.classList.toggle('is-current', j === n))
      marks[n]?.scrollIntoView({ block: 'center' })
      setHit(n)
      setFollow(false)
    },
    [],
  )
  useEffect(() => {
    if (q) jump(0)
  }, [q, jump])

  useKeys(
    {
      '/': (e) => {
        e.preventDefault()
        search.current?.focus()
      },
      f: () => setFollow((f) => !f),
      t: () => setOpenAll((o) => !o),
      n: () => jump(hit + 1),
      N: () => jump(hit - 1),
    },
    [hit, jump],
  )

  return (
    <div className="page log">
      <header className="page-head log-head">
        <h1>
          {run && <EventGlyph event={RUN_EVENT[run.state]} />}
          Run {id}
        </h1>
        {run && (
          <p className="page-sub lane-sub">
            <a href={href.lane(run.lane_id)}>{run.lane}</a>
            <span>{run.agent}{run.model ? ` (${run.model})` : ''}</span>
            <span>{run.state}</span>
            <span>{duration(run.started, run.ended, now)}</span>
            {runCost(run) && <span>{runCost(run)}</span>}
            {run.parent ? <a href={href.log(run.parent)}>continues run {run.parent}</a> : null}
          </p>
        )}
      </header>

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

/** The text with each case-insensitive match of q wrapped in <mark>. */
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
