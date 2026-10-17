import { useEffect, useId, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { href, type Route } from '../router'
import type { Status, Waiting } from '../api/types'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { SessionBanner } from '../session/SessionBanner'
import { Sidebar } from './Sidebar'
import { StatusCounts, CountList } from './StatusCounts'
import { type CountKind, type PopItem } from './statusItems'
import { useSideWidth } from './useSideWidth'
import { Button, Dialog, Divider, cx } from '../ui'
import { attentionCount, type StatusCounts as Counts } from '../state/attention'
import { pageTitle } from './title'
import { useShellMode } from './useMedia'
import type { NavKey } from './nav'
import { swapAction } from './swap'
import { isUnseen, useSeen } from '../state/seen'

/** The divider and the swap to a page are for windows at least this wide. */
export const DIVIDER_MIN = 768

/**
 * The frame (`.sh-shell`, `--wide` without a side panel): sidebar | main | side panel. The side
 * panel is the `side` slot, or, for a panel the page draws itself (`sideBeside`), the divider is
 * placed at its edge all the same. The divider is the panel's width control: 40 characters stay
 * for the main column, double-click resets, the width is remembered per view, and a drag past
 * 60% of the window calls `onSwap`. Below 768px there is none. Below 760px a mobile bar carries
 * the menu, the route's title, the status counts and the waiting count, and the sidebar is a
 * sheet. The sidebar and the page each sit in their own error boundary, so one failing leaves the other.
 */
export function Shell({
  route,
  waiting,
  workspaces,
  footer,
  banner,
  children,
  boardBroken = 0,
  paletteOpen,
  lanes,
  unseen,
  status,
  side,
  sideBeside = false,
  onSwap,
  swapLabel,
}: {
  route: Route
  waiting: Waiting | null
  workspaces: Status['workspaces']
  footer: ReactNode
  banner?: ReactNode
  children: ReactNode
  boardBroken?: number
  paletteOpen?: () => void
  /** Lane names, so the lane route is titled by its lane. */
  lanes?: readonly { id: number; name: string }[]
  /** Views with something new since the person last looked: a dot on the nav item. */
  unseen?: ReadonlySet<NavKey>
  /** The four counts and what is behind them, for the mobile bar. */
  status?: { counts: Counts; lists: Record<CountKind, PopItem[]> }
  /** The side panel, in the third column. */
  side?: ReactNode
  /** A side panel the page draws itself is showing beside the main column. */
  sideBeside?: boolean
  /** Release of a drag past 60% of the window: swap the layouts. Without one, the open item's own swap (see swap.ts) runs. */
  onSwap?: () => void
  swapLabel?: string
}) {
  const mode = useShellMode()
  // The front desk has something new for the person when a waiting item has not been looked at.
  const seen = useSeen()
  const unseenAll = (() => {
    const fresh = (waiting?.items ?? []).some((it) => isUnseen(seen, it))
    if (!fresh) return unseen
    return new Set<NavKey>([...(unseen ?? []), 'chat'])
  })()
  // The sheet is open for the route it was opened on: navigating closes it.
  const here = JSON.stringify(route)
  const [openFor, setOpenFor] = useState<string | null>(null)
  const [sheet, setSheet] = useState<CountKind | null>(null)
  const menu = useRef<HTMLButtonElement>(null)
  const sidebar = useRef<HTMLElement>(null)
  const sideId = useId()
  const compact = mode === 'sheet'
  const open = compact && openFor === here
  const setOpen = (on: boolean) => setOpenFor(on ? here : null)
  const shown = !compact || open
  const panel = side != null || sideBeside
  const view = route.page
  const w = useSideWidth(view, sidebar)
  const divider = panel && !compact && window.innerWidth >= DIVIDER_MIN
  const bar = window.innerWidth - document.documentElement.clientWidth

  // Opening the sheet moves focus into it; Escape closes it and returns focus to the button.
  useEffect(() => {
    if (!compact || !open) return
    sidebar.current?.querySelector<HTMLElement>('a')?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpenFor(null)
        menu.current?.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [compact, open])

  // A drag of the divider past 60% swaps the layouts: the page's own say, else the open item's.
  const swap = swapAction(route)
  const title = pageTitle(route, lanes)
  const waitingCount = attentionCount(waiting)
  const style = { '--sh-side-w': `${w.width}px` } as CSSProperties

  return (
    <div className={cx('sh-shell', !side && 'sh-shell--wide')} data-component="AppShell" data-mode={mode} style={panel ? style : undefined}>
      <a className="skip-link" href="#main" onClick={(e) => {
        e.preventDefault()
        document.getElementById('main')?.focus()
      }}>
        Skip to the page
      </a>
      <aside ref={sidebar} id={sideId} className={cx('sh-side', compact && 'is-sheet', open && 'is-open')} data-component="Sidebar" hidden={!shown}>
        <ErrorBoundary region="The sidebar">
          <Sidebar route={route} waiting={waiting} workspaces={workspaces} footer={footer} boardBroken={boardBroken} paletteOpen={paletteOpen} unseen={unseenAll} />
        </ErrorBoundary>
      </aside>
      {compact && open && <div className="sh-sheet-scrim" onClick={() => setOpen(false)} aria-hidden="true" />}
      <div className="sh-main-col">
        <SessionBanner />
        {compact && (
          <header className="sh-mobile-bar" data-component="MobileBar">
            <button
              ref={menu}
              type="button"
              className="sh-btn sh-btn--ghost menu-button"
              aria-expanded={open}
              aria-controls={sideId}
              onClick={() => setOpen(!open)}
            >
              Menu
            </button>
            <span className="sh-mobile-title">{title}</span>
            {status && <StatusCounts counts={status.counts} lists={status.lists} sheet onSheet={setSheet} />}
            {waitingCount > 0 && (
              <a className="sh-pill" href={href.inbox()} aria-label={`${waitingCount} waiting on you`}>
                {waitingCount} waiting
              </a>
            )}
          </header>
        )}
        {banner}
        <main id="main" className="main sh-main" tabIndex={-1}>
          <ErrorBoundary region="This page" resetKey={window.location.hash}>
            {children}
          </ErrorBoundary>
        </main>
      </div>
      {side != null && (
        <section className="sh-side-panel" data-component="SidePanel" aria-label="Side panel">
          {side}
        </section>
      )}
      {divider && (
        <Divider
          width={w.width}
          limits={w.limits}
          onResize={w.resize}
          onCommit={w.commit}
          onReset={w.reset}
          onSwap={onSwap ?? swap?.run}
          swapLabel={swapLabel ?? swap?.label}
          right={bar}
        />
      )}
      {status && sheet && (
        <Dialog
          open
          size="sheet"
          title={`${status.counts[sheet]} ${sheet}`}
          hideTitle
          hint={false}
          onClose={() => setSheet(null)}
          actions={
            <>
              <Button
                onClick={() => {
                  setSheet(null)
                  window.location.hash = (sheet === 'running' || sheet === 'broken' ? href.board() : href.inbox()).replace(/^#/, '')
                }}
              >
                Open the list
              </Button>
              <Button variant="ghost" onClick={() => setSheet(null)}>
                Close
              </Button>
            </>
          }
        >
          <CountList kind={sheet} n={status.counts[sheet]} items={status.lists[sheet]} onPick={() => setSheet(null)} />
        </Dialog>
      )}
    </div>
  )
}
