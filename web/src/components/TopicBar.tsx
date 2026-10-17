import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { topics as topicsApi } from '../api/client'
import { readFlag, splitTopics, topicHueClass, writeFlag, type TopicList, type TopicView } from '../model/topic'
import { Topics } from './Topics'
import '../styles/topics.css'

const POLL = 15_000
export const IDLE_KEY = 'shepherd.topicbar.idle.open'

/** One identity for the default loader, so an effect keyed on it is not restarted by a render. */
const defaultLoad: LoadTopicBar = (all) => topicsApi.list(all)

export type LoadTopicBar = (all: boolean) => Promise<TopicList>

/** The longest a chip label gets; the full title is the tooltip. */
export const CHIP_MAX = 28
/** Room kept for the "+N" button when some chips do not fit, and the gap between chips, in px. */
const MORE_W = 56
const GAP = 8

/** Text cut at a whole word to at most `max` characters, "…" after a cut. */
export function cutAtWord(text: string, max: number): string {
  const one = text.replace(/\s+/g, ' ').trim()
  if (one.length <= max) return one
  const head = one.slice(0, max - 1)
  const at = one[max - 1] === ' ' ? -1 : head.lastIndexOf(' ')
  return (at > 0 ? head.slice(0, at) : head).replace(/[\s,;:.-]+$/, '') + '…'
}

/** A topic title as a chip label. */
export const chipLabel = (title: string, max = CHIP_MAX) => cutAtWord(title, max)

/**
 * How many of the chips, with these widths, fit in `avail` px on one line. Everything when it
 * all fits; otherwise as many as fit with `reserve` px left for the "+N" button. An unknown width (0) shows all.
 */
export function fitCount(widths: number[], avail: number, reserve = MORE_W + GAP): number {
  if (avail <= 0) return widths.length
  const used = (n: number) => widths.slice(0, n).reduce((a, w) => a + w, 0) + GAP * Math.max(0, n - 1)
  if (used(widths.length) <= avail) return widths.length
  let n = 0
  while (n < widths.length && used(n + 1) <= avail - reserve) n++
  return n
}

/**
 * A strip in the conversation head, after the title: "All" and one chip per active topic, then "+N idle" to reveal
 * the quiet ones. The selected topic is always a chip, idle or not. It filters
 * through the same `onFilter` the rail's Topics section uses, so the hash is the one
 * source of truth and the two cannot disagree. With `manage` the full list opens in a
 * panel under the chips: rename, clear, reopen, merge and add live there.
 */
export function TopicBar({
  active, onFilter, manage = false, load = defaultLoad,
}: {
  /** The topic the thread is filtered to, or null for all. */
  active: number | null
  onFilter: (id: number | null) => void
  /** Shows the full topic list under the chips (rename, clear, reopen, merge, add); the host opens it. */
  manage?: boolean
  load?: LoadTopicBar
}) {
  const [list, setList] = useState<TopicView[]>([])
  const [showIdle, setShowIdle] = useState(() => readFlag(IDLE_KEY))
  const [now, setNow] = useState(() => Date.now())
  const strip = useRef<HTMLDivElement>(null)
  const row = useRef<HTMLDivElement>(null)
  const [fit, setFit] = useState<number | null>(null)
  const [menu, setMenu] = useState(false)

  useEffect(() => {
    let live = true
    const read = () =>
      load(true).then(
        (l) => {
          if (!live) return
          setNow(Date.now())
          setList(l.topics)
        },
        () => {},
      )
    void read()
    const id = window.setInterval(() => void read(), POLL)
    return () => {
      live = false
      window.clearInterval(id)
    }
  }, [load])

  const { active: shown, idle } = splitTopics(list, now, active)
  const chips = showIdle ? [...shown, ...idle] : shown

  // The order is stable. A selected topic that would overflow takes the last visible slot, or is the
  // only chip shown when none fits; it is never dropped.
  const ordered = chips
  const orderKey = ordered.map((t) => `${t.id}:${t.title}:${t.waits}`).join('|')

  // Measure every chip (the ones past the fit stay in the DOM, invisible) and see how many fit one line.
  useLayoutEffect(() => {
    const measure = () => {
      const box = row.current
      const el = strip.current
      if (!box || !el) return
      const idleW = box.querySelector<HTMLElement>('.topic-chip-idle')?.offsetWidth ?? 0
      const all = el.querySelector<HTMLElement>('.topic-chip-all')?.offsetWidth ?? 0
      const widths = Array.from(el.querySelectorAll<HTMLElement>('.topic-chip-t'), (c) => c.offsetWidth)
      const avail = box.clientWidth - (idleW ? idleW + GAP : 0) - all - GAP
      const n = fitCount(widths, box.clientWidth === 0 ? 0 : avail)
      setFit((f) => (f === n ? f : n))
    }
    measure()
    if (typeof ResizeObserver === 'undefined' || !row.current) return
    const ro = new ResizeObserver(measure)
    ro.observe(row.current)
    return () => ro.disconnect()
  }, [orderKey, showIdle, idle.length])

  const shownN = fit === null ? ordered.length : Math.min(fit, ordered.length)
  const visible = new Set(Array.from({ length: shownN }, (_, i) => i))
  const pick = ordered.findIndex((t) => t.id === active)
  if (pick >= shownN) {
    if (shownN > 0) visible.delete(shownN - 1)
    visible.add(pick)
  }
  const over = ordered.filter((_, i) => !visible.has(i))
  const menuOpen = menu && over.length > 0
  useEffect(() => {
    if (!menu) return
    const off = (e: Event) => {
      if (e instanceof KeyboardEvent ? e.key === 'Escape' : !row.current?.contains(e.target as Node)) setMenu(false)
    }
    document.addEventListener('keydown', off)
    document.addEventListener('mousedown', off)
    return () => {
      document.removeEventListener('keydown', off)
      document.removeEventListener('mousedown', off)
    }
  }, [menu])

  const none = shown.length === 0 && idle.length === 0
  return (
    <div className="topic-bar" role="group" aria-label="Filter the thread by topic">
      <div className="topic-bar-row" ref={row}>
        {!none && (
          <div className="topic-bar-strip" ref={strip}>
            <button type="button" className="topic-chip topic-chip-all" aria-pressed={active === null} onClick={() => onFilter(null)}>
              All
            </button>
            {ordered.map((t, i) => (
              <button
                key={t.id} type="button"
                className={`topic-chip topic-chip-t ${topicHueClass(t.id)}${t.waits > 0 ? ' needs-you' : ''}${visible.has(i) ? '' : ' is-over'}`}
                aria-pressed={active === t.id} title={t.title} tabIndex={visible.has(i) ? undefined : -1} aria-hidden={visible.has(i) ? undefined : true}
                onClick={() => onFilter(active === t.id ? null : t.id)}
              >
                <span className="topic-chip-title">{chipLabel(t.title)}</span>
                {t.waits > 0 && (
                  <span className="topic-chip-count">
                    {t.waits}
                    <span className="visually-hidden"> waiting on you</span>
                  </span>
                )}
              </button>
            ))}
          </div>
        )}
        {over.length > 0 && (
          <span className="topic-more">
            <button type="button" className="topic-chip topic-chip-more" aria-expanded={menuOpen} aria-haspopup="true" title={`${over.length} more topics`} onClick={() => setMenu(!menu)}>
              +{over.length}
            </button>
            {menuOpen && (
              <div className="topic-more-menu" role="group" aria-label="More topics">
                {over.map((t) => (
                  <button key={t.id} type="button" className="topic-more-item" title={t.title} onClick={() => { setMenu(false); onFilter(t.id) }}>
                    {chipLabel(t.title)}
                    {t.waits > 0 && <span className="topic-chip-count">{t.waits}<span className="visually-hidden"> waiting on you</span></span>}
                  </button>
                ))}
              </div>
            )}
          </span>
        )}
        {idle.length > 0 && (
          <button
            type="button" className="topic-chip topic-chip-idle" aria-expanded={showIdle}
            onClick={() => { writeFlag(IDLE_KEY, !showIdle); setShowIdle(!showIdle) }}
          >
            {showIdle ? `Hide ${idle.length} idle` : `+${idle.length} idle`}
          </button>
        )}
      </div>
      {manage && (
        <div className="topic-menu">
          <Topics active={active} now={now} onFilter={onFilter} load={load} />
        </div>
      )}
    </div>
  )
}
