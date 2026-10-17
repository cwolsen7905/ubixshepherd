import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { topics as topicsApi } from '../api/client'
import { chipLabel, fitCount, type LoadTopicBar } from '../components/TopicBar'
import { Topics } from '../components/Topics'
import { splitTopics, type TopicView } from '../model/topic'
import { Chip } from '../ui'
import { useShellMode } from '../shell/useMedia'
import { cx } from '../ui/cx'
import './convo.css'

const POLL = 15_000
const GAP = 8
/** Room kept for the "+n more" chip, in px. */
const MORE_W = 96

const defaultLoad: LoadTopicBar = (all) => topicsApi.list(all)

/**
 * The topic chips of the Conversation's head: "All" and a chip per active topic, each with the
 * lantern count of what waits on the person and a lichen dot while something in it is unseen.
 * The ones that do not fit, and the idle ones, wait behind "+n more", a popover. A click filters
 * the thread through `onFilter`, the same filter the hash carries. With `manage` the full list
 * (rename, clear, reopen, merge, add) opens under the chips.
 */
export function TopicChips({
  active, onFilter, manage = false, unseen, load = defaultLoad,
}: {
  active: number | null
  onFilter: (id: number | null) => void
  manage?: boolean
  /** Topics with something unseen that waits on the person. */
  unseen?: ReadonlySet<number>
  load?: LoadTopicBar
}) {
  const [list, setList] = useState<TopicView[]>([])
  const [now, setNow] = useState(() => Date.now())
  const row = useRef<HTMLDivElement>(null)
  const [fit, setFit] = useState<number | null>(null)
  const [menu, setMenu] = useState(false)
  // On a phone the chips scroll on one row instead of folding behind "+n more".
  const scroll = useShellMode() === 'sheet'

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

  const { active: busy, idle } = splitTopics(list, now, active)
  const ordered = busy
  const orderKey = ordered.map((t) => `${t.id}:${t.title}:${t.waits}`).join('|')

  // Every chip is measured (the ones past the fit stay in the DOM, hidden) to see how many fit one line.
  useLayoutEffect(() => {
    const box = row.current
    if (!box) return
    const measure = () => {
      const all = box.querySelector<HTMLElement>('[data-all]')?.offsetWidth ?? 0
      const widths = Array.from(box.querySelectorAll<HTMLElement>('[data-topic]'), (c) => c.offsetWidth)
      const n = fitCount(widths, box.clientWidth === 0 ? 0 : box.clientWidth - all - GAP, MORE_W + GAP)
      setFit((f) => (f === n ? f : n))
    }
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(measure)
    ro.observe(box)
    return () => ro.disconnect()
  }, [orderKey])

  const shownN = fit === null || scroll ? ordered.length : Math.min(fit, ordered.length)
  const visible = new Set(Array.from({ length: shownN }, (_, i) => i))
  // The selected topic is never dropped: it takes the last visible slot.
  const pick = ordered.findIndex((t) => t.id === active)
  if (pick >= shownN) {
    if (shownN > 0) visible.delete(shownN - 1)
    visible.add(pick)
  }
  const selectedIdle = idle.find((t) => t.id === active)
  const over = [...ordered.filter((_, i) => !visible.has(i)), ...idle.filter((t) => t.id !== active)]
  const overWaits = over.reduce((n, t) => n + t.waits, 0)
  const overUnseen = over.some((t) => unseen?.has(t.id))
  const open = menu && over.length > 0
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

  const chip = (t: TopicView, hidden = false) => (
    <span key={t.id} data-topic className={cx('sh-chiprow-item', hidden && 'is-over')} aria-hidden={hidden ? true : undefined}>
      <Chip
        selected={active === t.id}
        count={t.waits}
        onClick={() => onFilter(active === t.id ? null : t.id)}
      >
        <span title={t.title} tabIndex={hidden ? -1 : undefined}>{chipLabel(t.title)}</span>
        {unseen?.has(t.id) && <span className="sh-unseen" role="img" aria-label="unseen" />}
      </Chip>
    </span>
  )

  return (
    <div className="sh-topics" role="group" aria-label="Filter the thread by topic" data-component="TopicChips">
      <div className={cx('sh-topics-row', scroll && 'sh-chiprow--scroll')} ref={row}>
        <span data-all>
          <Chip kind="filter" selected={active === null} onClick={() => onFilter(null)}>
            All
          </Chip>
        </span>
        {ordered.map((t, i) => chip(t, !visible.has(i)))}
        {selectedIdle && chip(selectedIdle)}
        {over.length > 0 && (
          <span className="sh-topics-more">
            <button type="button" className="sh-chip" aria-expanded={open} aria-haspopup="true" title={`${over.length} more topics`} onClick={() => setMenu(!menu)}>
              +{over.length} more
              {overWaits > 0 && <span className="sh-chip-n">{overWaits}<span className="visually-hidden"> waiting on you</span></span>}
              {overUnseen && <span className="sh-unseen" role="img" aria-label="unseen" />}
            </button>
            {open && (
              <div className="sh-tmore-pop" role="group" aria-label="More topics">
                {over.map((t) => (
                  <button
                    key={t.id}
                    type="button"
                    className="sh-tmore-item"
                    title={t.title}
                    onClick={() => {
                      setMenu(false)
                      onFilter(t.id)
                    }}
                  >
                    <span>{chipLabel(t.title, 40)}</span>
                    <span className="sh-tmore-end">
                      {unseen?.has(t.id) && <span className="sh-unseen" role="img" aria-label="unseen" />}
                      {t.waits > 0 && <span className="sh-chip-n">{t.waits}<span className="visually-hidden"> waiting on you</span></span>}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </span>
        )}
      </div>
      {manage && (
        <div className="sh-topics-manage">
          <Topics active={active} now={now} onFilter={onFilter} load={load} />
        </div>
      )}
    </div>
  )
}
