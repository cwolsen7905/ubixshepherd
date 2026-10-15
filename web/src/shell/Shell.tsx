import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import type { Route } from '../router'
import type { Status, Waiting } from '../api/types'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { Sidebar } from './Sidebar'
import { currentItem } from './nav'
import { useShellMode } from './useMedia'

/**
 * The frame: a left sidebar, a slim page head and the page. The sidebar is full width, icons
 * only below 1280px, and a sheet (closed until the menu button opens it) below 760px. The
 * sidebar and the page each sit in their own error boundary, so one failing leaves the other.
 */
export function Shell({
  route,
  waiting,
  workspaces,
  footer,
  headActions,
  banner,
  children,
  boardBroken = 0,
  paletteOpen,
}: {
  route: Route
  waiting: Waiting | null
  workspaces: Status['workspaces']
  footer: ReactNode
  headActions: ReactNode
  banner?: ReactNode
  children: ReactNode
  boardBroken?: number
  paletteOpen?: () => void
}) {
  const mode = useShellMode()
  // The sheet is open for the route it was opened on: navigating closes it.
  const here = JSON.stringify(route)
  const [openFor, setOpenFor] = useState<string | null>(null)
  const menu = useRef<HTMLButtonElement>(null)
  const side = useRef<HTMLElement>(null)
  const sideId = useId()
  const sheet = mode === 'sheet'
  const open = sheet && openFor === here
  const setOpen = (on: boolean) => setOpenFor(on ? here : null)
  const shown = !sheet || open

  // Opening the sheet moves focus into it; Escape closes it and returns focus to the button.
  useEffect(() => {
    if (!sheet || !open) return
    side.current?.querySelector<HTMLElement>('a')?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpenFor(null)
        menu.current?.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [sheet, open])

  const title = currentItem(route)?.label ?? ''

  return (
    <div className="app shell" data-mode={mode}>
      <a className="skip-link" href="#main" onClick={(e) => {
        e.preventDefault()
        document.getElementById('main')?.focus()
      }}>
        Skip to the page
      </a>
      <aside ref={side} id={sideId} className={`sidebar${sheet ? ' is-sheet' : ''}${open ? ' is-open' : ''}`} hidden={!shown}>
        <ErrorBoundary region="The sidebar">
          <Sidebar route={route} waiting={waiting} workspaces={workspaces} footer={footer} boardBroken={boardBroken} paletteOpen={paletteOpen} />
        </ErrorBoundary>
      </aside>
      {sheet && open && <div className="sheet-scrim" onClick={() => setOpen(false)} aria-hidden="true" />}
      <div className="shell-body">
        <header className="page-bar">
          {sheet && (
            <button
              ref={menu}
              type="button"
              className="menu-button"
              aria-expanded={open}
              aria-controls={sideId}
              onClick={() => setOpen(!open)}
            >
              Menu
            </button>
          )}
          <span className="page-bar-title">{title}</span>
          <div className="page-bar-actions">
            <ErrorBoundary region="The waiting list">{headActions}</ErrorBoundary>
          </div>
        </header>
        {banner}
        <main id="main" className="main" tabIndex={-1}>
          <ErrorBoundary region="This page" resetKey={window.location.hash}>
            {children}
          </ErrorBoundary>
        </main>
      </div>
    </div>
  )
}
