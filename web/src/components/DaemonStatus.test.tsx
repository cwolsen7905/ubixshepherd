import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { StatusDetail } from '../api/types'
import { DaemonStatus, type Loaded } from './DaemonStatus'

const NOW = Date.parse('2026-10-10T12:00:00Z')

const detail = (o: Partial<StatusDetail> = {}): StatusDetail => ({
  commit: 'abcdef0123456789', built_at: '2026-10-09T08:00:00Z', os: 'linux', arch: 'amd64', host: 'build-box',
  now: '2026-10-10T12:00:00Z', config_loaded: '2026-10-10T11:30:00Z',
  running: 2, open_lanes: 5, decisions: 1, quota_holds: [{ agent: 'claude', until: '2026-10-10T14:05:00Z' }],
  spend_usd: 3.5, budget_usd: 20, forge: { last_poll: '2026-10-10T11:59:00Z' },
  ...o,
})

const loaded = (d: StatusDetail = detail(), session: Loaded['session'] = { role: 'web', allow_settings: true, expires: '2026-10-11T00:00:00Z' }): Loaded => ({
  status: { version: 'v0.9.0', pid: 1, started: '2026-10-10T09:00:00Z', store: 'sqlite', config: '/home/me/.shepherd/config.yaml', workspaces: [], detail: d },
  session,
})

function setup(props: Partial<React.ComponentProps<typeof DaemonStatus>> = {}) {
  const load = vi.fn(() => Promise.resolve(loaded()))
  render(<DaemonStatus error={null} downSince={null} ready lastEvent="2026-10-10T11:58:30Z" load={load} now={NOW} {...props} />)
  return { load, dot: screen.getByRole('button', { name: /daemon/i }), pop: () => screen.getByRole('tooltip', { hidden: true }) }
}

describe('DaemonStatus', () => {
  it('shows the daemon, config, activity, spend, watcher and session when focused', async () => {
    const user = userEvent.setup()
    const { load, dot, pop } = setup()
    expect(pop()).not.toBeVisible()
    expect(load).not.toHaveBeenCalled()

    await user.tab()
    expect(dot).toHaveFocus()
    expect(pop()).toBeVisible()
    expect(dot).toHaveAttribute('aria-describedby', pop().id)
    expect(await screen.findByText('v0.9.0')).toBeInTheDocument()
    const text = pop().textContent ?? ''
    for (const want of [
      'abcdef01', 'linux/amd64', 'build-box', '3h', '/home/me/.shepherd/config.yaml', '30m ago',
      '2 running', '5 open', '1 waiting', 'claude until', '$3.50', 'of $20.00', '1m ago',
      'connected', '1m ago', 'may save',
    ]) {
      expect(text).toContain(want)
    }
  })

  it('opens on hover and closes on leave, and on Escape', async () => {
    const user = userEvent.setup()
    const { dot, pop } = setup()
    await user.hover(dot)
    expect(pop()).toBeVisible()
    await user.unhover(dot)
    expect(pop()).not.toBeVisible()

    await user.hover(dot)
    expect(pop()).toBeVisible()
    await user.keyboard('{Escape}')
    expect(pop()).not.toBeVisible()
  })

  it('stays open after a tap until tapped again', async () => {
    const user = userEvent.setup()
    const { dot, pop } = setup()
    await user.click(dot)
    await user.unhover(dot)
    expect(pop()).toBeVisible()
    await user.click(dot)
    expect(pop()).not.toBeVisible()
  })

  it('says a failed reload and unreported fields plainly', async () => {
    const user = userEvent.setup()
    const d = detail({ reload_error: 'config.yaml: line 3: bad', reload_failed_at: '2026-10-10T11:45:00Z', quota_holds: undefined, forge: undefined })
    const { pop } = setup({ load: () => Promise.resolve(loaded(d, null)) })
    await user.tab()
    expect(await screen.findByText(/failed.*line 3: bad/)).toBeInTheDocument()
    expect(pop().textContent).toContain('not reported')
    expect(pop().textContent).not.toContain('may save')
  })

  it('says since when it is offline, and that it is retrying', async () => {
    const user = userEvent.setup()
    const down = NOW - 3 * 60_000
    const { load, pop } = setup({ error: 'the daemon could not be reached', downSince: down })
    expect(screen.getByRole('button', { name: /unreachable/i })).toBeInTheDocument()
    await user.tab()
    expect(pop()).toBeVisible()
    expect(pop().textContent).toMatch(/Offline since .*\(3m ago\)\. Retrying\./)
    expect(pop().textContent).toContain('not connected')
    // the daemon's detail comes from the daemon, which is not answering
    expect(pop().textContent).not.toContain('v0.9.0')
    await act(async () => {})
    expect(load).toHaveBeenCalled()
  })
})
