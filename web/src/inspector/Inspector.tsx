import { useEffect, useRef, type ReactNode } from 'react'
import { go, useRoute, withOpen, type Open } from '../router'
import { DecisionPane } from './decisionPane'
import { QuestionPane } from './questionPane'
import { useMode, type Mode } from './useMode'
import { closeInspector, leaveFull, useFullChrome } from './fullChrome'
import { PageBack, PageHeader } from '../ui/PageHeader'
import { LaneInspector } from './lane'
import { MrInspector } from './mr'
import { RunInspector } from './runPane'
import { useStoredArtifact } from '../artifacts/StoredArtifact'
import { ArtifactInspector } from './artifactPane'
import { ContextComposer } from './ContextComposer'
import { isAboutKind } from '../state/deskAbout'
import { useSnapshot } from '../state/context'
import { Button, Kbd, StatusGlyph } from '../ui'
import { SwapThread } from '../chat/SwapSide'
import { neighbour, openKey, waitingKey } from '../model/waitingStep'
import { showWaiting } from '../chat/strip'
import { useKeys } from '../hooks'
import { isSwapped, pageFor } from '../shell/swap'
import type { DeskThread } from '../state/desk'
import '../styles/inspector.css'
import '../shell/swap.css'

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

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
  const chrome = useFullChrome(open)
  switch (open.kind) {
    case 'lane':
      return <LaneInspector open={open} chrome={open.full ? chrome : undefined} />
    case 'decision':
      return <DecisionPane key={open.id} id={open.id} />
    case 'question':
      return <QuestionPane key={open.id} id={open.id} />
    case 'artifact':
      return <ArtifactInspector key={open.id} id={open.id} />
    case 'run':
      return <RunInspector key={open.id} id={open.id} tab={open.tab} full={open.full} chrome={open.full ? chrome : undefined} />
    case 'mr':
      return <MrInspector open={open} chrome={open.full ? chrome : undefined} />
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
  if (open.kind === 'lane' || open.kind === 'mr') return snap.lanes.find((l) => l.id === open.id)?.branch
  if (open.kind === 'decision') return snap.decisions.find((d) => d.id === open.id)?.lane
  if (open.kind === 'run') return snap.runs.find((r) => r.id === open.id)?.lane
  return undefined
}

/**
 * Whether the open view is cramped at the split pane's 576 px: a diff or a file list beside a
 * viewer, a long log, a rendered file. These get "Open full width". The rest (a lane's
 * overview, conversation and runs, a decision, a question) read well in a narrow column.
 */
export function isWide(open: Open): boolean {
  const tab = open.tab
  if (open.kind === 'lane' || open.kind === 'mr') return tab === 'changes' || tab === 'artifacts'
  if (open.kind === 'run') return tab === 'log' || tab === 'changes'
  return open.kind === 'artifact'
}

/** The words for a tab in the breadcrumb. */
const section = (tab: string | undefined) => (tab ? tab.charAt(0).toUpperCase() + tab.slice(1) : undefined)

/** The title chip's words: "run 5", or the merge request as the forge numbers it. */
function titleOf(open: Open, snap: ReturnType<typeof useSnapshot>): string {
  if (open.kind !== 'mr') return `${open.kind} ${open.id}`
  const mr = snap.lanes.find((l) => l.id === open.id)?.mr
  return mr ? `MR !${mr}` : `MR of lane ${open.id}`
}

/** The artifact's own title, or its path, once it is read; "artifact N" until then. */
function ArtifactName({ id, fallback }: { id: number; fallback: string }) {
  const { artifact } = useStoredArtifact(id, 0)
  return <>{artifact?.title || artifact?.path || fallback}</>
}

/** Lane, merge request and run views draw their own page header, with their tabs in it, when they fill the window. */
function ownsHeader(open: Open): boolean {
  return Boolean(open.full) && (open.kind === 'lane' || open.kind === 'mr' || open.kind === 'run')
}

/**
 * The one header: where the item lives (lane, then the tab), what it is as a chip, then
 * Open full width, Open as page and Close. A full-width view of a decision, a file or a question
 * has the shared page header instead: the back link, Open beside and Close, the title and where it lives.
 */
function InspectorHead({ open, heading }: { open: Open; heading: React.RefObject<HTMLHeadingElement | null> }) {
  const snap = useSnapshot()
  const chrome = useFullChrome(open)
  const parent = parentLane(open, snap)
  const page = pageFor(open)
  const tab = section(open.tab)
  const title = open.kind === 'artifact' ? <ArtifactName id={open.id} fallback={titleOf(open, snap)} /> : titleOf(open, snap)
  if (open.full) {
    return (
      <PageHeader
        {...chrome}
        titleRef={heading}
        title={title}
        meta={
          (parent || tab) && (
            <>
              {parent && <span>{parent}</span>}
              {tab && <span>{tab}</span>}
            </>
          )
        }
        actions={
          page && (
            <a className="sh-btn sh-btn--ghost" href={page}>
              Open as page
            </a>
          )
        }
      />
    )
  }
  return (
    <header className="inspector-head">
      {parent && (
        <>
          <span className="inspector-crumb">{parent}</span>
          <span aria-hidden="true">›</span>
        </>
      )}
      {tab && (
        <>
          <span className="inspector-crumb">{tab}</span>
          <span aria-hidden="true">›</span>
        </>
      )}
      <h2 id="inspector-title" ref={heading} tabIndex={-1} className="inspector-chip">
        {title}
      </h2>
      <span className="inspector-head-actions">
        {isWide(open) && (
          <button type="button" className="ui-btn ui-btn-ghost inspector-full-btn" onClick={() => go(withOpen(window.location.hash, { ...open, full: true }))}>
            Open full width
          </button>
        )}
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
 * The row over the main column while an item fills it: the way back to the thread, where the item
 * sits among the ones waiting, `]` for the next and Esc. Below the split width there is no thread
 * beside the page, so the bar answers `]` itself.
 */
function SwapBar({ open, keys }: { open: Open; keys: boolean }) {
  const snap = useSnapshot()
  const items = snap.waiting?.items ?? []
  const at = openKey(items, open)
  const pos = at === null ? -1 : items.findIndex((it) => waitingKey(it) === at)
  const next = () => {
    const it = neighbour(items, at, 'down')
    if (it) showWaiting(it)
  }
  useKeys(keys ? { ']': next } : {}, [keys, items, at])
  const d = snap.decisions.find((x) => x.id === open.id)
  return (
    <PageHeader
      back={<PageBack onClick={closeInspector}>← Conversation</PageBack>}
      onClose={closeInspector}
      glyph={<StatusGlyph glyph="?" tone="warn" label="decision" />}
      title={`Decision ${open.id}`}
      meta={
        d && (
          <>
            <span>{d.lane}</span>
            {d.agent && <span>{d.agent}</span>}
            {pos >= 0 && <span>{pos + 1} of {items.length} waiting</span>}
          </>
        )
      }
      barExtra={
        items.length > 1 && (
          <Button variant="ghost" className="sh-swap-next" onClick={next}>
            <Kbd keys="]" />
            next
          </Button>
        )
      }
    />
  )
}

/**
 * Wraps a page and shows the open item beside it (split), over it (overlay) or as a bottom
 * sheet. Focus moves to the heading on open and returns to where it was on close; Esc closes;
 * overlay and sheet trap Tab and make the page inert.
 */
export function InspectorFrame({ open, children, mode: forced, thread }: { open?: Open; children: ReactNode; mode?: Mode; thread?: (workspaceId: number) => DeskThread }) {
  const measured = useMode()
  const mode = forced ?? measured
  const route = useRoute()
  // A decision opened in the Conversation fills the main column and the thread goes to the side.
  const swapped = open !== undefined && isSwapped({ ...route, open })
  const modal = mode !== 'split' && !swapped
  const aside = useRef<HTMLElement>(null)
  const frame = useRef<HTMLDivElement>(null)
  const swapMain = useRef<HTMLDivElement>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const from = useRef<Element | null>(null)
  const shown = Boolean(open)
  const full = Boolean(open?.full)

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
    // The swapped layout has no inspector head: focus lands on the decision's column.
    if (swapped) swapMain.current?.focus({ preventScroll: true })
    else heading.current?.focus()
    return () => {
      const el = from.current
      from.current = null
      if (el instanceof HTMLElement && el.isConnected) el.focus()
    }
  }, [shown, swapped])

  // Open full width and Back swap one header button for the other, which drops focus with the
  // old one: put it on the button that took its place. Not on first open (the heading has it).
  const wasFull = useRef(full)
  useEffect(() => {
    if (wasFull.current === full) return
    wasFull.current = full
    aside.current?.querySelector<HTMLElement>(full ? '.sh-page-back' : '.inspector-full-btn')?.focus()
  }, [full])

  useEffect(() => {
    if (!shown) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented) return
      const box = swapped ? frame.current : aside.current
      if (!box) return
      if (e.key === 'Escape') {
        const a = document.activeElement
        const inside = box.contains(a)
        if (!modal && !inside && a !== document.body) return
        // Words being typed in a field of the swapped layout are not swapped away by Esc.
        if (swapped && (a instanceof HTMLTextAreaElement || a instanceof HTMLInputElement) && a.value !== '') return
        e.preventDefault()
        if (open?.full) leaveFull(open)
        else closeInspector()
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
  }, [shown, modal, open, swapped])

  if (open && swapped) {
    return (
      <div ref={frame} className={`inspector-frame is-swap is-${mode}`}>
        <div className="inspector-page sh-swap-main" ref={swapMain} tabIndex={-1} aria-label="Decision">
          <SwapBar open={open} keys={mode !== 'split'} />
          <div className="sh-swap-scroll">
            <div className="sh-swap-col">
              <InspectorBody open={open} />
            </div>
          </div>
        </div>
        {mode === 'split' && (
          <aside className="inspector inspector-split sh-swap-side" aria-label="The conversation about this decision">
            <SwapThread decisionId={open.id} make={thread} />
          </aside>
        )}
      </div>
    )
  }

  return (
    <div className={`inspector-frame${shown ? ` is-${mode}` : ''}${full ? ' is-full' : ''}`}>
      <div className="inspector-page" inert={shown && modal ? true : undefined} hidden={full}>
        {children}
      </div>
      {open && (
        <>
          {modal && !full && <div className="inspector-scrim" onClick={closeInspector} aria-hidden="true" />}
          <aside
            ref={aside}
            className={`inspector inspector-${mode}${full ? ' inspector-full' : ''}`}
            role={modal ? 'dialog' : undefined}
            aria-modal={modal ? true : undefined}
            aria-labelledby="inspector-title"
          >
            {!ownsHeader(open) && <InspectorHead open={open} heading={heading} />}
            <InspectorBody open={open} />
            <InspectorFoot open={open} />
          </aside>
        </>
      )}
    </div>
  )
}
