import { useEffect, useRef, useState } from 'react'
import { topics as topicsApi } from '../api/client'
import { readFlag, splitTopics, topicHueClass, writeFlag, type TopicList, type TopicView } from '../model/topic'
import '../styles/topics.css'

const POLL = 15_000
export const IDLE_KEY = 'shepherd.topicbar.idle.open'

export type LoadTopicBar = (all: boolean) => Promise<TopicList>

/**
 * A strip stuck to the top of the thread: "All" and one chip per active topic, then "+N idle" to reveal
 * the quiet ones. The selected topic is always a chip, idle or not. It filters
 * through the same `onFilter` the rail's Topics section uses, so the hash is the one
 * source of truth and the two cannot disagree. Hidden while no topic is active.
 */
export function TopicBar({
  active, onFilter, load = (all) => topicsApi.list(all),
}: {
  /** The topic the thread is filtered to, or null for all. */
  active: number | null
  onFilter: (id: number | null) => void
  load?: LoadTopicBar
}) {
  const [list, setList] = useState<TopicView[]>([])
  const [showIdle, setShowIdle] = useState(() => readFlag(IDLE_KEY))
  const [now, setNow] = useState(() => Date.now())
  const strip = useRef<HTMLDivElement>(null)

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

  // Keep the selected chip in view when the strip overflows; scrolls the strip only, never the page.
  useEffect(() => {
    const box = strip.current
    const chip = box?.querySelector<HTMLElement>('[aria-pressed="true"]')
    if (!box || !chip) return
    const left = chip.offsetLeft - box.offsetLeft
    if (left < box.scrollLeft || left + chip.offsetWidth > box.scrollLeft + box.clientWidth) {
      box.scrollLeft = left - (box.clientWidth - chip.offsetWidth) / 2
    }
  }, [active, chips.length])

  if (shown.length === 0 && idle.length === 0) return null
  return (
    <div className="topic-bar" role="group" aria-label="Filter the thread by topic">
      <div className="topic-bar-strip" ref={strip}>
        <button type="button" className="topic-chip" aria-pressed={active === null} onClick={() => onFilter(null)}>
          All
        </button>
        {chips.map((t) => (
          <button
            key={t.id} type="button" className={`topic-chip ${topicHueClass(t.id)}${t.waits > 0 ? ' needs-you' : ''}`}
            aria-pressed={active === t.id} onClick={() => onFilter(active === t.id ? null : t.id)}
          >
            <span className="topic-chip-title">{t.title}</span>
            {t.waits > 0 && (
              <span className="topic-chip-count">
                {t.waits}
                <span className="visually-hidden"> waiting on you</span>
              </span>
            )}
          </button>
        ))}
        {idle.length > 0 && (
          <button
            type="button" className="topic-chip topic-chip-idle" aria-expanded={showIdle}
            onClick={() => { writeFlag(IDLE_KEY, !showIdle); setShowIdle(!showIdle) }}
          >
            {showIdle ? `Hide ${idle.length} idle` : `+${idle.length} idle`}
          </button>
        )}
      </div>
    </div>
  )
}
