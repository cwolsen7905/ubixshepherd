import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { StatusDetail } from '../api/types'
import { RailSpend } from '../shell/RailSpend'
import { DaemonDetail, DaemonStatus, type Loaded } from './DaemonStatus'

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
  render(<DaemonStatus error={null} downSince={null} ready load={load} now={NOW} {...props} />)
  return { load, dot: screen.getByRole('button', { name: /daemon/i }), pop: () => screen.getByRole('dialog', { hidden: true }) }
}

describe('DaemonStatus', () => {
  it('shows four short lines and a link to the Status page when focused', async () => {
    const user = userEvent.setup()
    const { load, dot, pop } = setup({ chat: { connection: 'live', retryAt: null } })
    expect(pop()).not.toBeVisible()
    expect(load).not.toHaveBeenCalled()

    await user.tab()
    expect(dot).toHaveFocus()
    expect(pop()).toBeVisible()
    expect(dot).toHaveAttribute('aria-controls', pop().id)
    expect(await screen.findByText('v0.9.0')).toBeInTheDocument()
    const text = pop().textContent ?? ''
    for (const want of ['Daemon', 'up', '3h', 'Chat', 'live', 'polled 1m ago', 'v0.9.0']) expect(text).toContain(want)
    expect(pop().querySelectorAll('dl > div')).toHaveLength(4)
    expect(screen.getByRole('button')).toHaveTextContent('Daemon live')
    // the detail lives on the Status page now
    for (const gone of ['linux/amd64', '/home/me/.shepherd/config.yaml', '$3.50', 'may save']) expect(text).not.toContain(gone)
    expect(screen.getByRole('link', { name: 'Details →' })).toHaveAttribute('href', '#/status')
  })

  it('is green only when both links are up, amber while chat catches up, red when signed out or down', () => {
    const { rerender } = render(<DaemonStatus error={null} downSince={null} ready chat={{ connection: 'live', retryAt: null }} load={vi.fn(() => Promise.resolve(loaded()))} now={NOW} />)
    const dot = () => screen.getByRole('button')
    expect(dot()).toHaveClass('is-up')
    const again = (p: Partial<React.ComponentProps<typeof DaemonStatus>>) =>
      rerender(<DaemonStatus error={null} downSince={null} ready load={vi.fn(() => Promise.resolve(loaded()))} now={NOW} {...p} />)
    again({ chat: null })
    expect(dot()).toHaveClass('is-up')
    again({ chat: { connection: 'reconnecting', retryAt: null } })
    expect(dot()).toHaveClass('is-warn')
    expect(dot()).toHaveAccessibleName('Chat reconnecting')
    again({ chat: { connection: 'connecting', retryAt: null } })
    expect(dot()).toHaveClass('is-warn')
    again({ chat: { connection: 'signed-out', retryAt: null } })
    expect(dot()).toHaveClass('is-down')
    again({ chat: { connection: 'refused', retryAt: null } })
    expect(dot()).toHaveClass('is-down')
    again({ error: 'unreachable', downSince: NOW, chat: { connection: 'live', retryAt: null } })
    expect(dot()).toHaveClass('is-down')
    again({ ready: false, chat: null })
    expect(dot()).not.toHaveClass('is-up')
  })

  it('puts every status in the popover, leaving the spend bar to the rail', async () => {
    const user = userEvent.setup()
    const spend = { day: '2026-10-10', usd: 3.5, budget: 20, credit_usd: 0, by_source: null }
    const { dot, pop } = setup({ spend, counts: { running: 2, waiting: 1, broken: 0, routing: 3 } })
    expect(screen.queryByRole('meter')).toBeNull()
    await user.click(dot)
    await screen.findByText('v0.9.0')
    expect(pop().textContent).toContain('$3.50 of $20.00')
    expect(pop().textContent).toContain('2 running, 1 waiting, 0 broken, 3 to route')
  })

  it('says how long ago the forge was polled, without opening the popover', async () => {
    const { load, dot } = setup({ watch: true })
    await waitFor(() => expect(dot).toHaveTextContent('Daemon live, forge 1m ago'))
    expect(load).toHaveBeenCalledTimes(1)
    expect(dot).toHaveAccessibleName('Daemon live, forge 1m ago')
  })

  it('says the forge failed when its poll errors', async () => {
    const load = vi.fn(() => Promise.resolve(loaded(detail({ forge: { error: 'glab: 401' } }))))
    render(<DaemonStatus error={null} downSince={null} ready load={load} now={NOW} watch />)
    await waitFor(() => expect(screen.getByRole('button', { name: /daemon/i })).toHaveTextContent('Daemon live, forge error'))
  })

  it('draws the rail spend bar with its value and words', () => {
    render(<RailSpend spend={{ day: '2026-10-10', usd: 3.5, budget: 20, credit_usd: 0, by_source: null }} />)
    expect(screen.getByRole('meter', { name: 'Spent today' })).toHaveAttribute('aria-valuenow', '3.5')
    expect(document.querySelector('.rail-spend')).toHaveAttribute('title', '$3.50 of $20.00 today')
  })

  it('says the detail is unreadable when a refresh fails, not the old snapshot', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
      const load = vi.fn().mockResolvedValueOnce(loaded()).mockRejectedValue(new Error('boom'))
      const { dot, pop } = setup({ load })
      await user.click(dot)
      expect(await screen.findByText('v0.9.0')).toBeInTheDocument()
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5100)
      })
      expect(pop().textContent).toContain('unreadable')
      expect(pop().textContent).not.toContain('v0.9.0')
    } finally {
      vi.useRealTimers()
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

  it('says the daemon is down and for how long', async () => {
    const user = userEvent.setup()
    const { load, pop } = setup({ error: 'the daemon could not be reached', downSince: NOW - 3 * 60_000 })
    await user.tab()
    expect(pop()).toBeVisible()
    expect(pop().textContent).toMatch(/down for 3m/)
    expect(pop().textContent).not.toContain('v0.9.0')
    expect(pop().textContent).toContain('unknown')
    await act(async () => {})
    expect(load).toHaveBeenCalled()
  })
})

describe('DaemonDetail', () => {
  const show = (o: Partial<React.ComponentProps<typeof DaemonDetail>> = {}) =>
    render(<DaemonDetail down={false} error={null} downSince={null} lastEvent="2026-10-10T11:58:30Z" chat={{ connection: 'live', retryAt: null }} data={loaded()} failed={null} now={NOW} {...o} />)

  it('holds everything the popover used to, and the chat stream', () => {
    const { container } = show()
    const text = container.textContent ?? ''
    for (const want of [
      'abcdef01', 'linux/amd64', 'build-box', '3h', '/home/me/.shepherd/config.yaml', '30m ago',
      '2 running', '5 open', '1 waiting', 'claude until', '$3.50', 'of $20.00', '1m ago',
      'Event stream', 'connected', 'Chat stream', 'live', 'may save',
    ]) {
      expect(text).toContain(want)
    }
  })

  it('says a failed reload and unreported fields plainly', () => {
    const d = detail({ reload_error: 'config.yaml: line 3: bad', reload_failed_at: '2026-10-10T11:45:00Z', quota_holds: undefined, forge: undefined })
    const { container } = show({ data: loaded(d, null) })
    expect(container.textContent).toMatch(/failed.*line 3: bad/)
    expect(container.textContent).toContain('not reported')
    expect(container.textContent).not.toContain('may save')
  })

  it('says since when it is offline and that the chat stream is not open', () => {
    const { container } = show({ down: true, error: 'no answer', downSince: NOW - 3 * 60_000, data: null, chat: null })
    expect(container.textContent).toMatch(/Offline since .*\(3m ago\)\. Retrying\./)
    expect(container.textContent).toContain('not connected')
    expect(container.textContent).toContain('not open')
  })
})
