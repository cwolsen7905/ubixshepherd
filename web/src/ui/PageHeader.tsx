import { useEffect, useId, useLayoutEffect, useRef, type KeyboardEvent, type ReactNode, type Ref } from 'react'
import { Button } from './Button'
import { cx } from './cx'
import { dialogShown, isEditable } from './LaneDetailEscape'
import { Kbd } from './Kbd'

/** What a page tells its header about getting out of it. */
export interface PageChrome {
  /** The small underlined way back: a link or a button, "← Board". */
  back: ReactNode
  /** "Open beside the board": the same item in the side panel. Absent where there is no panel. */
  beside?: { label: string; onClick: () => void }
  /** Close: leaves the page. */
  onClose: () => void
  /** Which of the two Esc does, so the key chip sits on the button that answers it. Default `close`. */
  escape?: 'close' | 'beside'
  /** The id of the title's heading, for a frame that labels itself by it and moves focus to it. */
  titleId?: string
}

export interface PageHeaderProps extends PageChrome {
  /** The state glyph, drawn before the title. */
  glyph?: ReactNode
  title: ReactNode
  /** The faint line under the title: repo, branch, age, state. */
  meta?: ReactNode
  /** The state's primary action and a ghost secondary, left. */
  actions?: ReactNode
  /** The destructive action, right: a default outlined button with an ellipsis ("Close lane…"). */
  danger?: ReactNode
  /** The tab strip, a `PageTabs`. */
  tabs?: ReactNode
  /** Something for the top bar, before "Open beside …": where the item sits among the waiting ones, a next key. */
  barExtra?: ReactNode
  /** The title's heading element, for a frame that moves focus to it. */
  titleRef?: Ref<HTMLHeadingElement>
  /** Esc leaves the page here. Left off where the page handles Esc itself. */
  handleEscape?: boolean
  /** Names the page for a screen reader, and tells the header it sits in a column that scrolls itself. */
  label?: string
}

/**
 * The one header of every "Open as page" view (`.sh-page-head`): a top bar (back link at the
 * left, "Open beside …" and Close with the Esc chip at the right), the title row, a faint meta
 * line, the actions row and the tabs. The whole block is sticky: it stays in view while the body
 * scrolls under it, in whichever element scrolls (the window or a column of its own).
 * Esc closes the page unless a dialog is up or the person is typing in a field.
 */
export function PageHeader({ back, beside, onClose, escape = 'close', glyph, title, meta, actions, danger, tabs, barExtra, titleRef, titleId, handleEscape = false, label }: PageHeaderProps) {
  const toBeside = beside?.onClick
  const head = useRef<HTMLElement>(null)
  // Whatever else sticks in the body (a log's search row) sticks under the header, at its measured height.
  useLayoutEffect(() => {
    const el = head.current
    const host = el?.parentElement
    if (!el || !host) return
    const set = () => host.style.setProperty('--sh-page-head-h', `${el.offsetHeight}px`)
    set()
    if (typeof ResizeObserver === 'undefined') return
    const watch = new ResizeObserver(set)
    watch.observe(el)
    return () => {
      watch.disconnect()
      host.style.removeProperty('--sh-page-head-h')
    }
  }, [])
  useEffect(() => {
    if (!handleEscape) return
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return
      if (isEditable(e.target) || dialogShown()) return
      if (escape === 'beside' && toBeside) toBeside()
      else onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [handleEscape, escape, onClose, toBeside])
  const chip = (
    <>
      {' '}
      <Kbd keys="Esc" />
    </>
  )
  return (
    <header ref={head} className="sh-page-head" data-component="PageHeader" aria-label={label}>
      <div className="sh-page-head-bar">
        {back}
        <span className="sh-page-head-bar-actions">
          {barExtra}
          {beside && (
            <Button variant="ghost" className="sh-page-head-beside" onClick={beside.onClick}>
              {beside.label}
              {escape === 'beside' && chip}
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Close
            {escape === 'close' && chip}
          </Button>
        </span>
      </div>
      <div className="sh-page-head-title">
        {glyph}
        <h1 id={titleId} ref={titleRef} tabIndex={titleRef || titleId ? -1 : undefined}>{title}</h1>
      </div>
      {meta && <div className="sh-page-head-meta">{meta}</div>}
      {(actions || danger) && (
        <div className="sh-page-head-actions">
          {actions}
          {danger && <span className="sh-page-head-danger">{danger}</span>}
        </div>
      )}
      {tabs}
    </header>
  )
}

/** The small underlined back link of a header, for a page that has an address to go back to. */
export function PageBack({ children, href, onClick }: { children: ReactNode; href?: string; onClick?: () => void }) {
  if (href !== undefined) {
    return (
      <a
        className="sh-page-back"
        href={href}
        onClick={(e) => {
          if (!onClick || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
          e.preventDefault()
          onClick()
        }}
      >
        {children}
      </a>
    )
  }
  return (
    <button type="button" className="sh-page-back" onClick={onClick}>
      {children}
    </button>
  )
}

export type PageTab = { id: string; label: ReactNode }

/** The ids a tab and its panel carry, so the page can name the panel the strip controls. */
export function pageTabIds(base: string, id: string) {
  return { tab: `${base}-tab-${id}`, panel: `${base}-panel-${id}` }
}

/**
 * The tab strip of a page header (`.sh-tabs`). Counts in a label are plain numbers. The caller
 * renders the panel (role tabpanel, the ids from `pageTabIds`), since the strip sits in the sticky
 * header and the panel below it. Arrow keys, Home and End move between tabs.
 */
export function PageTabs({ tabs, value, onChange, label, base }: { tabs: PageTab[]; value: string; onChange: (id: string) => void; label: string; base: string }) {
  const refs = useRef(new Map<string, HTMLButtonElement>())
  const current = tabs.find((t) => t.id === value) ?? tabs[0]
  function onKey(e: KeyboardEvent, i: number) {
    const last = tabs.length - 1
    const to = e.key === 'ArrowRight' ? (i === last ? 0 : i + 1) : e.key === 'ArrowLeft' ? (i === 0 ? last : i - 1) : e.key === 'Home' ? 0 : e.key === 'End' ? last : -1
    if (to < 0) return
    e.preventDefault()
    const next = tabs[to]
    if (!next) return
    onChange(next.id)
    refs.current.get(next.id)?.focus()
  }
  return (
    <div role="tablist" aria-label={label} className="sh-tabs ui-tabs-list sh-page-head-tabs" data-component="PageTabs">
      {tabs.map((t, i) => (
        <button
          key={t.id}
          ref={(el) => {
            if (el) refs.current.set(t.id, el)
            else refs.current.delete(t.id)
          }}
          type="button"
          role="tab"
          id={pageTabIds(base, t.id).tab}
          aria-selected={t.id === current?.id}
          aria-controls={pageTabIds(base, t.id).panel}
          tabIndex={t.id === current?.id ? 0 : -1}
          className={cx('sh-tab', t.id === current?.id && 'is-on', 'ui-tab')}
          onClick={() => onChange(t.id)}
          onKeyDown={(e) => onKey(e, i)}
        >
          {t.label}
        </button>
      ))}
    </div>
  )
}

/** A stable id base for one page's tabs and panel. */
export function usePageTabBase(): string {
  return useId()
}
