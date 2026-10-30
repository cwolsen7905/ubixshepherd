import { useEffect, useId, useRef, useState, useSyncExternalStore } from 'react'
import { readTheme, setTheme, subscribeTheme, THEMES } from '../theme/theme'
import { usePageMenuItems } from './pageMenu'
import './MoreMenu.css'

/**
 * The one "…" menu at the top right. It always holds the theme (Dark, Light or System, remembered
 * for this browser); the page shown adds its own items under a divider (see `usePageMenu`).
 * Enter opens it, j and k or the arrows move, Enter picks, Esc closes and returns focus.
 */
export function MoreMenu() {
  const theme = useSyncExternalStore(subscribeTheme, readTheme)
  const pages = usePageMenuItems()
  const [open, setOpen] = useState(false)
  const id = useId()
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', away)
    return () => document.removeEventListener('mousedown', away)
  }, [open])

  const close = () => {
    setOpen(false)
    trigger.current?.focus()
  }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      return close()
    }
    const step = e.key === 'j' || e.key === 'ArrowDown' ? 1 : e.key === 'k' || e.key === 'ArrowUp' ? -1 : 0
    if (step === 0 || !panel.current) return
    const rows = [...panel.current.querySelectorAll<HTMLElement>('button')]
    if (rows.length === 0) return
    e.preventDefault()
    const at = rows.indexOf(document.activeElement as HTMLElement)
    rows[(at + step + rows.length) % rows.length]?.focus()
  }

  return (
    <div ref={root} className="sh-more-menu" data-component="MoreMenu" onKeyDown={onKey}>
      <button
        ref={trigger}
        type="button"
        className="sh-btn ui-btn"
        aria-label="More"
        aria-haspopup="true"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(!open)}
      >
        <span aria-hidden="true">⋯</span>
      </button>
      {open && (
        <div id={id} ref={panel} className="sh-more-pop" role="group" aria-label="Menu">
          <div role="group" aria-label="Theme" className="sh-more-options sh-more-theme">
            {THEMES.map(([value, name]) => (
              <button key={value} type="button" aria-pressed={theme === value} className="sh-btn sh-btn--ghost" onClick={() => setTheme(value)}>
                {name}
              </button>
            ))}
          </div>
          {pages.map(({ title, items }, i) => (
            <div key={i} role="group" aria-label={title} className="sh-more-options sh-more-page">
              {items.map((it) =>
                it.node !== undefined ? (
                  <div key={it.key} className="sh-more-node">{it.node}</div>
                ) : (
                  <button
                    key={it.key}
                    type="button"
                    className="sh-btn sh-btn--ghost"
                    aria-pressed={it.pressed}
                    onClick={() => {
                      setOpen(false)
                      it.onSelect?.()
                    }}
                  >
                    {it.label}
                  </button>
                ),
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
