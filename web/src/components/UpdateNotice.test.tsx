import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { UpdateStatus, UpgradeStatus } from '../api/types'
import { UpdateNotice } from './UpdateNotice'

const status = (o: Partial<UpdateStatus> = {}): UpdateStatus => ({
  enabled: true, channel: 'beta', current: 'v0.4.0', current_known: true, latest: 'v0.5.0-beta.2',
  url: 'https://example.test/r/2', available: true, dismissed: false, show: true,
  ...o,
})

describe('UpdateNotice', () => {
  it('names the version, links the release and dismisses it', async () => {
    const user = userEvent.setup()
    const load = vi.fn(() => Promise.resolve(status()))
    const dismiss = vi.fn((v: string) => Promise.resolve(status({ dismissed: true, show: false, latest: v })))
    render(<UpdateNotice load={load} dismiss={dismiss} />)

    expect(await screen.findByText('v0.5.0-beta.2')).toBeInTheDocument()
    expect(screen.getByText(/this is v0.4.0/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'See the release' })).toHaveAttribute('href', 'https://example.test/r/2')

    await user.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(dismiss).toHaveBeenCalledWith('v0.5.0-beta.2')
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())
  })

  it('says so for a build whose version is unknown', async () => {
    const load = () => Promise.resolve(status({ current: 'dev', current_known: false, available: false }))
    render(<UpdateNotice load={load} dismiss={vi.fn()} />)
    expect(await screen.findByText(/This build is dev, so it is not compared/)).toBeInTheDocument()
  })

  it('shows nothing when the daemon says not to, or cannot be read', async () => {
    const { rerender } = render(<UpdateNotice load={() => Promise.resolve(status({ show: false, dismissed: true }))} dismiss={vi.fn()} />)
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())
    rerender(<UpdateNotice load={() => Promise.reject(new Error('down'))} dismiss={vi.fn()} />)
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument())
  })

  it('never follows a link that is not https', async () => {
    const load = () => Promise.resolve(status({ url: 'javascript:alert(1)' }))
    render(<UpdateNotice load={load} dismiss={vi.fn()} />)
    await screen.findByText('v0.5.0-beta.2')
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('keeps the notice and says why when the dismissal fails', async () => {
    const user = userEvent.setup()
    render(<UpdateNotice load={() => Promise.resolve(status())} dismiss={() => Promise.reject(new Error('409 not the version now offered'))} />)
    await user.click(await screen.findByRole('button', { name: 'Dismiss' }))
    expect(await screen.findByText(/Could not dismiss: 409/)).toBeInTheDocument()
    expect(screen.getByText('v0.5.0-beta.2')).toBeInTheDocument()
  })

  describe('upgrade', () => {
    const upgrade = (o: Partial<UpgradeStatus> = {}): UpgradeStatus => ({
      state: 'idle', method: 'binary', can_upgrade: true, active_runs: 0, auto: false, ...o,
    })

    it('upgrades from the notice and shows the pending restart', async () => {
      const user = userEvent.setup()
      // The daemon's answer follows the upgrade, as the notice's faster polling will read it.
      let now = status({ upgrade: upgrade() })
      const load = vi.fn(() => Promise.resolve(now))
      const start = vi.fn((...args: [string, boolean]) => {
        void args
        now = status({ upgrade: upgrade({ state: 'installed', target: 'v0.5.0-beta.2', active_runs: 2 }) })
        return Promise.resolve(now)
      })
      render(<UpdateNotice load={load} dismiss={vi.fn()} upgrade={start} />)
      await user.click(await screen.findByRole('button', { name: 'Upgrade' }))
      expect(start).toHaveBeenCalledWith('v0.5.0-beta.2', false)
      expect(await screen.findByText(/restarts when no agent is running \(2 running now\)/)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Upgrade' })).not.toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: /Restart now \(interrupts 2 running agents\)/ }))
      expect(start).toHaveBeenLastCalledWith('v0.5.0-beta.2', true)
    })

    it('shows the command instead when the daemon will not replace this install', async () => {
      const hint = 'installed with Homebrew: run `brew upgrade shepherd`'
      const load = () => Promise.resolve(status({ upgrade: upgrade({ method: 'brew', can_upgrade: false, hint }) }))
      render(<UpdateNotice load={load} dismiss={vi.fn()} upgrade={vi.fn()} />)
      expect(await screen.findByText(/brew upgrade shepherd/)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Upgrade' })).not.toBeInTheDocument()
    })

    it('says why a failed upgrade failed and lets the person retry', async () => {
      const load = () =>
        Promise.resolve(status({ upgrade: upgrade({ state: 'rolled_back', error: 'it did not stay up', failed_version: 'v0.5.0-beta.2' }) }))
      render(<UpdateNotice load={load} dismiss={vi.fn()} upgrade={vi.fn()} />)
      expect(await screen.findByText(/The upgrade failed: it did not stay up/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Upgrade' })).toBeInTheDocument()
    })

    it('shows the error and keeps the button when the request is refused', async () => {
      const user = userEvent.setup()
      const load = () => Promise.resolve(status({ upgrade: upgrade() }))
      render(<UpdateNotice load={load} dismiss={vi.fn()} upgrade={() => Promise.reject(new Error('409 not newer'))} />)
      await user.click(await screen.findByRole('button', { name: 'Upgrade' }))
      expect(await screen.findByText(/409 not newer/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Upgrade' })).toBeEnabled()
    })

    it('offers no upgrade to a daemon that has none', async () => {
      render(<UpdateNotice load={() => Promise.resolve(status())} dismiss={vi.fn()} upgrade={vi.fn()} />)
      await screen.findByText('v0.5.0-beta.2')
      expect(screen.queryByRole('button', { name: 'Upgrade' })).not.toBeInTheDocument()
    })
  })
})
