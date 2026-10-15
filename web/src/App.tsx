import { useMemo, useRef, useState } from 'react'
import { boardItems, counts as boardCounts, dismissedCount } from './model/board'
import { useKeys } from './hooks'
import { go, href, useRoute } from './router'
import { useSnapshot } from './state/context'
import { BoardPage } from './pages/Board'
import { ChatPage } from './pages/Chat'
import { InboxPage } from './pages/Inbox'
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
import { InspectorFrame } from './inspector/Inspector'
import { CommandPalette, type PaletteHandle } from './palette/CommandPalette'
import { StatusCounts } from './components/StatusCounts'
import { statusCounts } from './state/attention'
import { UpdateNotice } from './components/UpdateNotice'
import { Shell } from './shell/Shell'
import { useAttentionChrome } from './shell/useAttentionChrome'
import { SpendMeter } from './components/BudgetMeter'
import { readShowDismissed, writeShowDismissed } from './model/board'

export function App() {
  const route = useRoute()
  const snap = useSnapshot()
  const [showDismissed, setShowDismissed] = useState(() => readShowDismissed(window.localStorage))
  const items = useMemo(() => boardItems({ ...snap, showDismissed }), [snap, showDismissed])
  const hidden = useMemo(() => dismissedCount(snap.lanes, snap.runs), [snap.lanes, snap.runs])
  const [help, setHelp] = useState(false)
  const pending = useRef('')
  const paletteRef = useRef<PaletteHandle>(null)
  const boardBroken = boardCounts(items).broken

  useAttentionChrome(snap.waiting)

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
      c: () => pending.current === 'g' && go(href.chat()),
      n: () => pending.current === 'g' && go(href.next()),
    },
    [],
  )

  const spend = snap.spend

  const footer = (
    <>
      {spend && <SpendMeter spend={spend} />}
      <div className="foot-row">
        <ErrorBoundary region="The daemon status">
          <DaemonStatus error={snap.error} downSince={snap.downSince} ready={snap.ready} lastEvent={snap.feed.at(-1)?.created} />
        </ErrorBoundary>
        <button type="button" className="help-button" onClick={() => setHelp(true)} aria-label="Keyboard shortcuts">
          <kbd>?</kbd>
        </button>
      </div>
      <ErrorBoundary region="The update notice">
        <UpdateNotice />
      </ErrorBoundary>
    </>
  )

  return (
    <>
      <Shell
        route={route}
        waiting={snap.waiting}
        workspaces={snap.status?.workspaces ?? []}
        footer={footer}
        boardBroken={boardBroken}
        paletteOpen={() => paletteRef.current?.open()}
        headActions={
          <StatusCounts counts={statusCounts({ ...snap, showDismissed })} />
        }
        banner={
          snap.error && (
            <div className="banner tone-bad" role="alert">
              The daemon did not answer: {snap.error}. Showing what was last loaded; retrying.
            </div>
          )
        }
      >
        <InspectorFrame open={route.open}>
        {route.page === 'board' && (
          <BoardPage
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
          />
        )}
        {route.page === 'projects' && <ProjectsPage />}
        {route.page === 'project' && <ProjectPage key={route.name} name={route.name} />}
        {route.page === 'unassigned' && <ProjectPage key="unassigned" name={null} />}
        {route.page === 'runs' && <RunsPage range={route.range} agent={route.agent} lane={route.lane} />}
        {route.page === 'timeline' && <TimelinePage range={route.range} agent={route.agent} />}
        {route.page === 'usage' && <UsagePage range={route.range} from={route.from} to={route.to} />}
        {route.page === 'inbox' && <InboxPage kind={route.kind} />}
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
        </InspectorFrame>
      </Shell>
      {help && <Help onClose={() => setHelp(false)} />}
      <CommandPalette ref={paletteRef} />
    </>
  )
}
