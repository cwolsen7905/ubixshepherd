import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Member } from './api'
import { MemberList } from './MemberList'

afterEach(cleanup)

const now = () => Date.parse('2026-11-22T12:00:00Z')
const mem = (over: Partial<Member> = {}): Member => ({
  name: 'work', enrolled: true, online: true, since: '2026-11-22T10:00:00Z', version: '0.9.0', api_level: 1, runs_going: 1, waiting: 2, daemon_id: 'abc-123', ...over,
})
const ops = (rows: Member[], over = {}) => ({
  list: async () => rows, add: vi.fn(), revoke: vi.fn(), remove: vi.fn(), confirm: vi.fn(), ...over,
})

describe('MemberList', () => {
  it('shows each member with whether it is connected, in words, and its counts', async () => {
    render(<MemberList ops={ops([mem(), mem({ name: 'home', online: false, since: '2026-11-20T12:00:00Z', runs_going: 0, waiting: 0 })])} now={now} />)
    expect(await screen.findByText('work')).toBeTruthy()
    expect(screen.getByText(/online since 2h ago/)).toBeTruthy()
    expect(screen.getByText(/1 running, 2 waiting/)).toBeTruthy()
    expect(screen.getByText(/offline, last seen 2d ago/)).toBeTruthy()
  })

  it('says so when no member exists and when one waits to enrol or has a changed id', async () => {
    const { unmount } = render(<MemberList ops={ops([])} now={now} />)
    expect(await screen.findByText(/No member yet/)).toBeTruthy()
    unmount()
    const confirm = vi.fn(async () => undefined)
    render(<MemberList ops={ops([mem({ name: 'new', enrolled: false, online: false, since: undefined, code_expires: '2026-11-22T12:05:00Z' }), mem({ name: 'swapped', online: false, pending_id: 'zzz' })], { confirm })} now={now} />)
    expect(await screen.findByText(/waiting to enrol/)).toBeTruthy()
    expect(screen.getByText(/refused: its daemon id changed/)).toBeTruthy()
    await userEvent.setup().click(screen.getByRole('button', { name: 'Confirm new id' }))
    await waitFor(() => expect(confirm).toHaveBeenCalledWith('swapped'))
  })

  it('adds a member and shows its code once, until hidden', async () => {
    const add = vi.fn(async (name: string) => ({ name, code: 'one-two-three-four-five-six-seven-eight', expires: '2026-11-22T12:05:00Z' }))
    render(<MemberList ops={ops([], { add })} now={now} />)
    const u = userEvent.setup()
    await u.type(await screen.findByLabelText('Member name'), 'home')
    await u.click(screen.getByRole('button', { name: 'Add member' }))
    expect(await screen.findByText('one-two-three-four-five-six-seven-eight')).toBeTruthy()
    expect(add).toHaveBeenCalledWith('home')
    await u.click(screen.getByRole('button', { name: 'Hide the code' }))
    expect(screen.queryByText('one-two-three-four-five-six-seven-eight')).toBeNull()
  })

  it('revokes only after a confirm', async () => {
    const revoke = vi.fn(async () => undefined)
    render(<MemberList ops={ops([mem()], { revoke })} now={now} />)
    const u = userEvent.setup()
    await u.click(await screen.findByRole('button', { name: 'Revoke' }))
    const dlg = await screen.findByRole('alertdialog')
    expect(revoke).not.toHaveBeenCalled()
    await u.click(within(dlg).getByRole('button', { name: 'Revoke' }))
    await waitFor(() => expect(revoke).toHaveBeenCalledWith('work'))
  })
})
