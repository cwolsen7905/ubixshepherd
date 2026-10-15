import { useEffect, useRef, type ReactNode } from 'react'
import { go, withOpen, type Open } from '../router'
import { useMode, type Mode } from './useMode'
import '../styles/inspector.css'

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

/** Closes the inspector by dropping `open=` from the address, so Back opens it again. */
export function closeInspector() {
  go(withOpen(window.location.hash, undefined))
}

/** The placeholder body: later steps give each kind its own pane. */
function Placeholder({ open }: { open: Open }) {
  return (
    <p className="inspector-body">
      Open: {open.kind} {open.id}
      {open.tab ? `, ${open.tab}` : ''}
    </p>
  )
}

/**
 * Wraps a page and shows the open item beside it (split), over it (overlay) or as a bottom
 * sheet. Focus moves to the heading on open and returns to where it was on close; Esc closes;
 * overlay and sheet trap Tab and make the page inert.
 */
export function InspectorFrame({ open, children, mode: forced }: { open?: Open; children: ReactNode; mode?: Mode }) {
  const measured = useMode()
  const mode = forced ?? measured
  const modal = mode !== 'split'
  const aside = useRef<HTMLElement>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const from = useRef<Element | null>(null)
  const shown = Boolean(open)

  useEffect(() => {
    if (!shown) return
    from.current = document.activeElement
    heading.current?.focus()
    return () => {
      const el = from.current
      from.current = null
      if (el instanceof HTMLElement && el.isConnected) el.focus()
    }
  }, [shown])

  // The shell sits outside the frame, so in a modal mode everything beside the dialog's
  // ancestors goes inert too (the scrim stays live, so a click on it can close).
  useEffect(() => {
    if (!shown || !modal) return
    const made: HTMLElement[] = []
    for (let el = aside.current; el && el !== document.body; el = el.parentElement) {
      for (const sib of Array.from(el.parentElement?.children ?? [])) {
        if (sib === el || !(sib instanceof HTMLElement) || sib.hasAttribute('inert') || sib.classList.contains('inspector-scrim')) continue
        sib.setAttribute('inert', '')
        made.push(sib)
      }
    }
    return () => made.forEach((el) => el.removeAttribute('inert'))
  }, [shown, modal])

  useEffect(() => {
    if (!shown) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented) return
      const box = aside.current
      if (!box) return
      if (e.key === 'Escape') {
        const a = document.activeElement
        const inside = box.contains(a)
        if (!modal && !inside && a !== document.body) return
        e.preventDefault()
        closeInspector()
        return
      }
      if (e.key !== 'Tab' || !modal) return
      const items = Array.from(box.querySelectorAll<HTMLElement>(FOCUSABLE))
      const a = document.activeElement
      if (!box.contains(a) || items.length === 0) {
        e.preventDefault()
        ;(items[0] ?? heading.current)?.focus()
        return
      }
      const first = items[0] as HTMLElement
      const last = items[items.length - 1] as HTMLElement
      if (e.shiftKey && (a === first || a === heading.current)) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && a === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [shown, modal])

  return (
    <div className={`inspector-frame${shown ? ` is-${mode}` : ''}`}>
      <div className="inspector-page" inert={shown && modal ? true : undefined}>
        {children}
      </div>
      {open && (
        <>
          {modal && <div className="inspector-scrim" onClick={closeInspector} aria-hidden="true" />}
          <aside
            ref={aside}
            className={`inspector inspector-${mode}`}
            role={modal ? 'dialog' : undefined}
            aria-modal={modal ? true : undefined}
            aria-labelledby="inspector-title"
          >
            <header className="inspector-head">
              <h2 id="inspector-title" ref={heading} tabIndex={-1}>
                {open.kind} {open.id}
              </h2>
              <button type="button" className="inspector-close" onClick={closeInspector} aria-label="Close inspector">
                ×
              </button>
            </header>
            <Placeholder open={open} />
          </aside>
        </>
      )}
    </div>
  )
}
