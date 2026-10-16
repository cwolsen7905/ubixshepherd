import { useEffect, useRef, type ReactNode } from 'react'
import { go, href, withOpen, type Open } from '../router'
import { DecisionPane } from './decisionPane'
import { QuestionPane } from './questionPane'
import { useMode, type Mode } from './useMode'
import { LaneInspector } from './lane'
import { ArtifactInspector } from './artifactPane'
import { ContextComposer } from './ContextComposer'
import { isAboutKind } from '../state/deskAbout'
import { useSnapshot } from '../state/context'
import { Kbd } from '../ui'
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

/** What a kind shows: each step adds its `case`; the rest fall back to the placeholder. */
function InspectorBody({ open }: { open: Open }) {
  switch (open.kind) {
    case 'lane':
      return <LaneInspector open={open} />
    case 'decision':
      return <DecisionPane key={open.id} id={open.id} />
    case 'question':
      return <QuestionPane key={open.id} id={open.id} />
    case 'artifact':
      return <ArtifactInspector key={open.id} id={open.id} />
    default:
      return <Placeholder open={open} />
  }
}

/** The context composer, labelled with what the person knows the entity by (a lane's branch). */
function InspectorFoot({ open }: { open: Open }) {
  const snap = useSnapshot()
  if (!isAboutKind(open.kind)) return null
  const label = open.kind === 'lane' ? snap.lanes.find((l) => l.id === open.id)?.branch : undefined
  return <ContextComposer about={{ kind: open.kind, ids: [open.id], ...(label ? { label } : {}) }} />
}

/** The lane an open item belongs to, as the breadcrumb names it. Unknown items show no parent. */
function parentLane(open: Open, snap: ReturnType<typeof useSnapshot>): string | undefined {
  if (open.kind === 'lane') return snap.lanes.find((l) => l.id === open.id)?.branch
  if (open.kind === 'decision') return snap.decisions.find((d) => d.id === open.id)?.lane
  if (open.kind === 'run') return snap.runs.find((r) => r.id === open.id)?.lane
  return undefined
}

/** The page an item has of its own, for "Open as page". */
function pageFor(open: Open): string | undefined {
  if (open.kind === 'lane') return href.lane(open.id)
  if (open.kind === 'run') return href.log(open.id)
  return undefined
}

/** The breadcrumb row: where the item lives, what it is, then Open as page and Close. */
function InspectorHead({ open, heading }: { open: Open; heading: React.RefObject<HTMLHeadingElement | null> }) {
  const snap = useSnapshot()
  const parent = parentLane(open, snap)
  const page = pageFor(open)
  return (
    <header className="inspector-head">
      {parent && (
        <>
          <span className="inspector-crumb">{parent}</span>
          <span aria-hidden="true">›</span>
        </>
      )}
      <h2 id="inspector-title" ref={heading} tabIndex={-1}>
        {open.kind} {open.id}
      </h2>
      <span className="inspector-head-actions">
        {page && (
          <a className="ui-btn ui-btn-ghost" href={page}>
            Open as page
          </a>
        )}
        <button type="button" className="ui-btn ui-btn-ghost" onClick={closeInspector} aria-label="Close inspector">
          Close <Kbd keys="Esc" />
        </button>
      </span>
    </header>
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

  // The shell sits outside the frame, so in a modal mode everything beside the dialog's
  // ancestors goes inert too (the scrim stays live, so a click on it can close). This effect
  // comes before the focus one: cleanups run in order, so the shell is live again by the time
  // focus returns to an element in it.
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
    from.current = document.activeElement
    heading.current?.focus()
    return () => {
      const el = from.current
      from.current = null
      if (el instanceof HTMLElement && el.isConnected) el.focus()
    }
  }, [shown])

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
            <InspectorHead open={open} heading={heading} />
            <InspectorBody open={open} />
            <InspectorFoot open={open} />
          </aside>
        </>
      )}
    </div>
  )
}
