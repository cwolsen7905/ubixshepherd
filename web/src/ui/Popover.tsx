import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import './ui.css'

/** A small floating panel from a trigger button. Esc or a click outside closes it; Esc returns focus. */
export function Popover({ label, children }: { label: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const id = useId()
  const root = useRef<HTMLSpanElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!open) return
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      setOpen(false)
      trigger.current?.focus()
    }
    const away = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('keydown', key)
    document.addEventListener('mousedown', away)
    return () => {
      document.removeEventListener('keydown', key)
      document.removeEventListener('mousedown', away)
    }
  }, [open])

  return (
    <span ref={root} className="ui-popover-anchor">
      <button ref={trigger} type="button" className="ui-btn" aria-expanded={open} aria-controls={id} onClick={() => setOpen((o) => !o)}>
        {label}
      </button>
      {open && (
        <div id={id} className="ui-popover" role="group">
          {children}
        </div>
      )}
    </span>
  )
}
