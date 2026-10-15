import { useEffect, useMemo, useRef, useState } from 'react'
import { boardItems, counts, dismissedCount, countsText, GROUP_NAMES, GROUPS } from './model/board'
import { usd } from './format'
import { faviconHref } from './favicon'
import { useKeys } from './hooks'
import { go, href, useRoute } from './router'
import { useSnapshot } from './state/context'
import { awaitingApproval } from './state/live'
import { BoardPage } from './pages/Board'
import { ChatPage } from './pages/Chat'
import { DecisionsPage } from './pages/Decisions'
import { NextPage } from './pages/Next'
import { LanePage } from './pages/Lane'
import { LogPage } from './pages/Log'
import { ProjectPage } from './pages/Project'
import { ProjectsPage } from './pages/Projects'
import { RunsPage } from './pages/Runs'
import { SettingsPage } from './pages/Settings'
import { TimelinePage } from './pages/Timeline'
import { UsagePage } from './pages/Usage'
import { DaemonStatus } from './components/DaemonStatus'
import { ErrorBoundary } from './components/ErrorBoundary'
import { Help } from './components/Help'
import { WaitingOnYou } from './components/WaitingOnYou'
import { UpdateNotice } from './components/UpdateNotice'
import { GROUP_MARKS } from './model/groups'
import { readShowDismissed, writeShowDismissed } from './model/board'

export function App() {
  const route = useRoute()
  const snap = useSnapshot()
  const [showDismissed, setShowDismissed] = useState(() => readShowDismissed(window.localStorage))
  const items = useMemo(() => boardItems({ ...snap, showDismissed }), [snap, showDismissed])
  const hidden = useMemo(() => dismissedCount(snap.lanes, snap.runs), [snap.lanes, snap.runs])
  const n = counts(items)
  const [help, setHelp] = useState(false)
  const pending = useRef('')

  // The counts go to the window title, so a background tab shows that something waits.
  useEffect(() => {
    const c = countsText(n)
    document.title = c ? `${c} · Shepherd` : 'Shepherd'
  }, [n])

  // And the icon takes a lantern dot while something waits on the person.
  const waiting = n.needs > 0
  useEffect(() => {
    document.querySelector<HTMLLinkElement>('link[rel="icon"]')?.setAttribute('href', faviconHref(waiting))
  }, [waiting])

  useKeys(
    {
      '?': () => setHelp((h) => !h),
      Escape: () => setHelp(false),
      g: () => {
        pending.current = 'g'
        window.setTimeout(() => (pending.current = ''), 1200)
      },
      b: () => pending.current === 'g' && go(href.board()),
      d: () => pending.current === 'g' && go(href.decisions()),
      t: () => pending.current === 'g' && go(href.timeline()),
      n: () => pending.current === 'g' && go(href.next()),
    },
    [],
  )

  const decisions = snap.decisions.length + snap.requests.length
  const approvals = awaitingApproval(snap.next).length
  const spend = snap.spend

  return (
    <div className="app">
      <ErrorBoundary region="The top bar">
      <header className="bar">
        <a className="wordmark" href={href.board()}>
          <svg className="wordmark-mark" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
            <rect width="32" height="32" fill="#0f62fe" />
            <rect x="6" y="6" width="9" height="9" fill="#ffffff" />
            <rect x="17" y="6" width="9" height="9" fill="#ffffff" opacity="0.55" />
            <rect x="6" y="17" width="9" height="9" fill="#ffffff" opacity="0.55" />
            <rect x="17" y="17" width="9" height="9" fill="#ffffff" />
          </svg>
          <span>
            uBix<b>Shepherd</b>
          </span>
        </a>
        <nav className="nav" aria-label="Views">
          <a href={href.board()} aria-current={route.page === 'board' ? 'page' : undefined}>
            Board
          </a>
          <a
            href={href.projects()}
            aria-current={route.page === 'projects' || route.page === 'project' || route.page === 'unassigned' ? 'page' : undefined}
          >
            Projects
          </a>
          <a href={href.decisions()} aria-current={route.page === 'decisions' ? 'page' : undefined}>
            Decisions
            {decisions > 0 && <span className="nav-count tone-warn">{decisions}</span>}
          </a>
          <a href={href.next()} aria-current={route.page === 'next' ? 'page' : undefined}>
            Next up
            {approvals > 0 && (
              <span className="nav-count tone-warn" title={`${approvals} Next-up item${approvals === 1 ? '' : 's'} wait${approvals === 1 ? 's' : ''} on your approval`}>
                {approvals}
              </span>
            )}
          </a>
          <a href={href.runs()} aria-current={route.page === 'runs' ? 'page' : undefined}>
            Runs
          </a>
          <a href={href.timeline()} aria-current={route.page === 'timeline' ? 'page' : undefined}>
            Timeline
          </a>
          <a href={href.usage()} aria-current={route.page === 'usage' ? 'page' : undefined}>
            Usage
          </a>
          <a href={href.chat()} aria-current={route.page === 'chat' ? 'page' : undefined}>
            Chat
          </a>
          <a href={href.settings()} aria-current={route.page === 'settings' ? 'page' : undefined}>
            Settings
          </a>
        </nav>
        <p className="bar-counts" aria-live="polite">
          {GROUPS.filter((g) => n[g] > 0).map((g) => (
            <span key={g} className={`bar-count tone-${g === 'needs' ? 'warn' : g === 'broken' ? 'broken' : 'muted'}`}>
              <span aria-hidden="true">{GROUP_MARKS[g].glyph}</span> {n[g]} {GROUP_NAMES[g]}
            </span>
          ))}
        </p>
        <WaitingOnYou />
        <p className="bar-meta">
          {spend && (
            <span title={`Spent today (${spend.day})`}>
              {usd(spend.usd)}
              {spend.budget > 0 && <span className="faint"> of {usd(spend.budget)}</span>} today
            </span>
          )}
          <DaemonStatus
            error={snap.error}
            downSince={snap.downSince}
            ready={snap.ready}
            lastEvent={snap.feed.at(-1)?.created}
          />
          <button type="button" className="help-button" onClick={() => setHelp(true)} aria-label="Keyboard shortcuts">
            <kbd>?</kbd>
          </button>
        </p>
      </header>
      </ErrorBoundary>

      {snap.error && (
        <div className="banner tone-bad" role="alert">
          The daemon did not answer: {snap.error}. Showing what was last loaded; retrying.
        </div>
      )}

      <ErrorBoundary region="The update notice">
        <UpdateNotice />
      </ErrorBoundary>

      <main className="main">
        <ErrorBoundary region="This page" resetKey={window.location.hash}>
        {route.page === 'board' && <BoardPage
            items={items}
            range={route.range}
            dismissed={{
              count: hidden,
              show: showDismissed,
              onShow: (on) => {
                setShowDismissed(on)
                writeShowDismissed(window.localStorage, on)
              },
            }}
          />}
        {route.page === 'projects' && <ProjectsPage />}
        {route.page === 'project' && <ProjectPage key={route.name} name={route.name} />}
        {route.page === 'unassigned' && <ProjectPage key="unassigned" name={null} />}
        {route.page === 'runs' && <RunsPage range={route.range} agent={route.agent} lane={route.lane} />}
        {route.page === 'timeline' && <TimelinePage range={route.range} agent={route.agent} />}
        {route.page === 'usage' && <UsagePage range={route.range} from={route.from} to={route.to} />}
        {route.page === 'decisions' && <DecisionsPage focus={route.focus} />}
        {route.page === 'next' && <NextPage />}
        {route.page === 'chat' && <ChatPage />}
        {route.page === 'settings' && <SettingsPage scope={route.scope} />}
        {route.page === 'lane' && <LanePage key={route.id} id={route.id} />}
        {route.page === 'log' && <LogPage key={route.run} run={route.run} />}
        {route.page === 'missing' && (
          <p className="empty">
            Nothing lives at <code>{route.path}</code>. <a href={href.board()}>Go to the board</a>.
          </p>
        )}
        </ErrorBoundary>
      </main>

      {help && <Help onClose={() => setHelp(false)} />}
    </div>
  )
}
