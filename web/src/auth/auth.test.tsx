import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import type { Device } from './api'
import { DeviceList } from './DeviceList'
import { LoginGate } from './LoginGate'

afterEach(cleanup)

const dev = (over: Partial<Device> = {}): Device => ({
  id: 'd1', name: 'laptop', created: '2026-11-01T00:00:00Z', last_used: '2026-11-04T00:00:00Z', expires: '2026-12-04T00:00:00Z', max_expires: '2027-01-30T00:00:00Z', ...over,
})
const now = () => Date.parse('2026-11-05T00:00:00Z')

describe('DeviceList', () => {
  it('lists devices by name and times, never a token', async () => {
    render(<DeviceList ops={{ list: async () => [dev()], revoke: vi.fn(), signOutEverywhere: vi.fn() }} now={now} />)
    expect(await screen.findByText('laptop')).toBeTruthy()
    expect(document.body.textContent).toContain('2027-01-30')
    expect(document.body.textContent).not.toMatch(/shd_|hash/)
  })

  it('revokes one device after a confirm, then reloads', async () => {
    let rows = [dev(), dev({ id: 'd2', name: 'build box' })]
    const revoke = vi.fn(async (id: string) => { rows = rows.filter((r) => r.id !== id) })
    render(<DeviceList ops={{ list: async () => rows, revoke, signOutEverywhere: vi.fn() }} now={now} />)
    const u = userEvent.setup()
    const [first] = await screen.findAllByRole("button", { name: "Revoke" })
    if (!first) throw new Error("no revoke button")
    await u.click(first)
    const dlg = await screen.findByRole('alertdialog')
    expect(revoke).not.toHaveBeenCalled()
    await u.click(within(dlg).getByRole('button', { name: 'Revoke' }))
    await waitFor(() => expect(revoke).toHaveBeenCalledWith('d1'))
    await waitFor(() => expect(screen.queryByText('laptop')).toBeNull())
    expect(screen.getByText('build box')).toBeTruthy()
  })

  it('signs out everywhere only after confirming', async () => {
    const out = vi.fn(async () => ({}))
    const done = vi.fn()
    render(<DeviceList ops={{ list: async () => [], revoke: vi.fn(), signOutEverywhere: out }} now={now} onSignedOut={done} />)
    const u = userEvent.setup()
    await u.click(await screen.findByRole('button', { name: 'Sign out everywhere' }))
    expect(out).not.toHaveBeenCalled()
    await u.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Sign out everywhere' }))
    await waitFor(() => expect(done).toHaveBeenCalled())
    expect(out).toHaveBeenCalledOnce()
  })
})

describe('LoginGate', () => {
  it('shows the app with a session, the sign-in page on 401, and the app when offline', async () => {
    const { rerender } = render(<LoginGate read={async () => ({})}><p>app</p></LoginGate>)
    expect(await screen.findByText('app')).toBeTruthy()
    rerender(<LoginGate read={async () => { throw new ApiError(401, 'no session') }}><p>app2</p></LoginGate>)
    expect(await screen.findByRole('heading', { name: /Sign in/ })).toBeTruthy()
    cleanup()
    render(<LoginGate read={async () => { throw new Error('offline') }}><p>app3</p></LoginGate>)
    expect(await screen.findByText('app3')).toBeTruthy()
  })
})
