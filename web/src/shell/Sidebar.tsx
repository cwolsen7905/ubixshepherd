import { useState, type ReactNode } from 'react'
import { href, type Route } from '../router'
import type { Status, Waiting } from '../api/types'
import { KeepGoing } from '../components/KeepGoing'
import { Count, Icon, Kbd, cx, type BadgeTone } from '../ui'
import { COUNT_LABEL, NAV, navCounts, type NavItem, type NavKey } from './nav'
import { RailFoot } from './RailFoot'

/** Only Inbox is a call to act; Next up is a plain count and Board is broken work (the design's three tones). */
const COUNT_TONE: Partial<Record<NavKey, BadgeTone>> = { next: 'neutral', board: 'bad' }

/** The workspace the daemon serves is the brand: its name, or a count when there are several. */
function workspaceName(workspaces: Status['workspaces']): string {
  const [first] = workspaces
  if (!first) return 'uBixShepherd'
  return workspaces.length === 1 ? first.name : `${workspaces.length} workspaces`
}

/** The workspace mark and name (`.sh-ws`), a link to the board. */
export function Workspace({ workspaces, rail = false }: { workspaces: Status['workspaces']; rail?: boolean }) {
  return (
    <a
      className="sh-ws"
      data-component="WorkspaceMark"
      href={href.board()}
      aria-label={workspaces.length ? `uBixShepherd, ${workspaceName(workspaces)}, go to the board` : 'uBixShepherd, go to the board'}
      title={workspaces.map((w) => w.name).join(', ') || (rail ? 'uBixShepherd' : undefined)}
    >
      <span className="sh-mark" aria-hidden="true">
        <i />
        <i />
        <i />
        <i />
      </span>
      {!rail && <span>{workspaceName(workspaces)}</span>}
    </a>
  )
}

/** What a count stands for in the rail's tooltip: "Inbox, 3 waiting", "Board, 1 broken". */
const RAIL_NOUN: Partial<Record<NavKey, string>> = { inbox: 'waiting', next: 'waiting', board: 'broken' }

/** The collapsed item's tooltip and accessible name: its label, and its count where it has one. */
export function railName(it: Pick<NavItem, 'key' | 'label'>, n: number): string {
  const noun = RAIL_NOUN[it.key]
  return noun ? `${it.label}, ${n} ${noun}` : it.label
}

/** Detect platform for the search shortcut hint: Cmd on Apple platforms, Ctrl elsewhere. */
function isMacOS(): boolean {
  return /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)
}

/**
 * The left sidebar (`.sh-side`): the workspace as the brand, the search trigger that opens the
 * palette, the views in three groups with their counts and unseen dots, then the footer in the
 * design's order: Keep going, then what the caller puts in `footer` (spend, restarting, the
 * Online line with the keys button, the four status counts, the update notice).
 * Counts come from the attention list; the board count shows broken lanes, in red with a cross.
 * With `rail` it collapses to icons: each view is its icon with its name and count in the tooltip,
 * a lantern dot on Inbox while something waits, a red cross on Board while a lane is broken, and
 * the footer folds into Keep going and one status glyph (RailFoot).
 */
export function Sidebar({
  route,
  waiting,
  workspaces,
  footer,
  boardBroken,
  paletteOpen,
  unseen,
  rail = false,
  onToggleRail,
  status,
}: {
  route: Route
  waiting: Waiting | null
  workspaces: Status['workspaces']
  footer: ReactNode
  boardBroken: number
  paletteOpen?: () => void
  /** Views with something new since the person last looked. */
  unseen?: ReadonlySet<NavKey>
  /** Collapsed to an icon rail. */
  rail?: boolean
  /** The chevron beside the workspace mark; without it there is no chevron. */
  onToggleRail?: () => void
  /** Lanes working and failing, for the rail's one status glyph. */
  status?: { running: number; broken: number }
}) {
  const [isMac] = useState(isMacOS)
  const counts = navCounts(waiting)
  const keys = isMac ? '⌘K' : 'Ctrl K'
  return (
    <>
      <div className="sh-side-head">
        <Workspace workspaces={workspaces} rail={rail} />
        {onToggleRail && (
          <button
            type="button"
            className="sh-rail-toggle"
            data-component="RailToggle"
            aria-expanded={!rail}
            aria-label={rail ? 'Expand the sidebar' : 'Collapse the sidebar to icons'}
            title={rail ? 'Expand the sidebar (\\)' : 'Collapse the sidebar (\\)'}
            onClick={onToggleRail}
          >
            <Icon name={rail ? 'chevron-right' : 'chevron-left'} />
          </button>
        )}
      </div>
      {rail ? (
        <button type="button" className="sh-rail-btn sh-rail-find" data-component="SearchTrigger" onClick={() => paletteOpen?.()} aria-label="Search or act" title={`Search or act (${keys})`}>
          <Icon name="search" />
        </button>
      ) : (
        <button type="button" className="sh-find" data-component="SearchTrigger" onClick={() => paletteOpen?.()} aria-label="Search or act">
          <span>Search or act</span>
          <Kbd keys={keys} />
        </button>
      )}
      <nav className="sh-side-nav" aria-label="Views">
        {NAV.map((g, i) => (
          <div key={g.name} role="group" aria-label={g.name} className="sh-nav-group">
            {i > 0 && <div className="sh-nav-sep" aria-hidden="true" />}
            {g.items.map((it) => {
              const n = (it.key === 'board' ? boardBroken : counts[it.key]) ?? 0
              const here = it.current(route)
              const fresh = unseen?.has(it.key)
              if (rail) {
                const name = railName(it, n)
                return (
                  <a
                    key={it.key}
                    className={cx('sh-nav-item', 'is-rail', here && 'is-here')}
                    data-component="NavItem"
                    data-nav={it.key}
                    href={it.to()}
                    aria-current={here ? 'page' : undefined}
                    aria-label={name}
                    title={name}
                  >
                    <Icon name={it.icon} />
                    {it.key === 'inbox' && n > 0 && <i className="sh-rail-dot is-waiting" data-dot="waiting" aria-hidden="true" />}
                    {it.key === 'board' && n > 0 && (
                      <i className="sh-rail-broken" data-dot="broken" aria-hidden="true">
                        ✗
                      </i>
                    )}
                    {fresh && <i className="sh-rail-dot is-unseen" data-dot="unseen" aria-hidden="true" />}
                  </a>
                )
              }
              return (
                <a
                  key={it.key}
                  className={cx('sh-nav-item', here && 'is-here')}
                  data-component="NavItem"
                  data-nav={it.key}
                  href={it.to()}
                  aria-current={here ? 'page' : undefined}
                  title={it.label}
                >
                  <span className="sh-nav-label">
                    <Icon name={it.icon} />
                    {it.label}
                    {fresh && <i className="sh-unseen" title="New since you last looked" />}
                  </span>
                  <Count count={n} tone={COUNT_TONE[it.key] ?? it.countTone ?? 'attention'} label={COUNT_LABEL[it.key]} />
                </a>
              )
            })}
          </div>
        ))}
      </nav>
      {rail ? (
        <RailFoot running={status?.running ?? 0} broken={status?.broken ?? 0}>
          {footer}
        </RailFoot>
      ) : (
        <div className="sh-side-foot">
          <KeepGoing />
          {footer}
        </div>
      )}
    </>
  )
}
