import { act, cleanup, render, screen } from '@testing-library/react'
import type { StatusDetail } from '../api/types'
import type { Loaded } from '../components/DaemonStatus'
import { publishChat } from '../shell/chatLink'
import { Live, type Api, type Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { feedItem, status } from '../test/fixtures'
import { StatusPage } from './Status'

const NOW = Date.parse('2026-10-10T12:00:00Z')
const still: Clock = { now: () => NOW, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

const detail: StatusDetail = {
  commit: 'abcdef0123456789', built_at: '2026-10-09T08:00:00Z', os: 'linux', arch: 'amd64', host: 'build-box',
  now: '2026-10-10T12:00:00Z', config_loaded: '2026-10-10T11:30:00Z', running: 2, open_lanes: 5, decisions: 0,
  quota_holds: [], spend_usd: 3.5, budget_usd: 20, forge: { last_poll: '2026-10-10T11:59:00Z' },
}
const loaded = (): Loaded => ({
  status: { version: 'v0.9.0', pid: 1, started: '2026-10-10T09:00:00Z', store: 'sqlite', config: '/c.yaml', workspaces: [], detail },
  session: null,
})

function view(live: Live) {
  return render(
    <LiveProvider live={live}>
      <StatusPage load={() => Promise.resolve(loaded())} now={NOW} />
    </LiveProvider>,
  )
}

function liveWith(over: Partial<Api> = {}) {
  const api = {
    status: async () => status(),
    lanes: async () => [], runs: async () => [], decisions: async () => [], requests: async () => [],
    spend: async () => null,
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async () => ({ items: [feedItem({ id: 1, created: '2026-10-10T11:58:30Z' })], events: ['run_passed'], last: 1 }),
    ...over,
  } as unknown as Api
  return new Live(api, still)
}

describe('StatusPage', () => {
  afterEach(() => {
    cleanup()
    publishChat(null)
  })

  it('shows the daemon detail at full width', async () => {
    view(liveWith())
    expect(screen.getByRole('heading', { name: 'Status' })).toBeInTheDocument()
    expect(await screen.findByText('v0.9.0')).toBeInTheDocument()
    expect(document.body.textContent).toContain('linux/amd64')
    expect(document.body.textContent).toContain('/c.yaml')
    expect(document.body.textContent).toContain('Chat stream')
  })

  it('shows the chat stream state as the open chat publishes it', async () => {
    view(liveWith())
    expect(document.body.textContent).toContain('not open')
    act(() => publishChat({ connection: 'reconnecting', retryAt: null }))
    expect(document.body.textContent).toContain('reconnecting')
    act(() => publishChat({ connection: 'signed-out', retryAt: null }))
    expect(document.body.textContent).toContain('signed out')
  })
})
