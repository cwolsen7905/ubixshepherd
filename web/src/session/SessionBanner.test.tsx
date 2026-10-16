import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { SessionBanner } from './SessionBanner'

const read = vi.fn()
const extend = vi.fn()
const active = vi.fn()
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  webSession: { read: () => read(), extend: () => extend(), active: () => active() },
}))

const NOW = Date.parse('2026-10-15T12:00:00Z')
const at = (min: number) => new Date(NOW + min * 60_000).toISOString()

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  vi.setSystemTime(NOW)
  read.mockReset()
  extend.mockReset()
  active.mockReset()
})
afterEach(() => vi.useRealTimers())

describe('SessionBanner', () => {
  it('reports the person’s input, but not on a timer', async () => {
    read.mockResolvedValue({ expires: at(500), max_expires: at(5000) })
    active.mockResolvedValue({ expires: at(720), max_expires: at(5000) })
    render(<SessionBanner />)
    await act(async () => { vi.advanceTimersByTime(120_000) })
    expect(active).not.toHaveBeenCalled()
    await act(async () => { window.dispatchEvent(new Event('keydown')) })
    expect(active).toHaveBeenCalledTimes(1)
    await act(async () => { window.dispatchEvent(new Event('keydown')) })
    expect(active).toHaveBeenCalledTimes(1)
  })

  it('stays hidden while there is more than 30 minutes', async () => {
    read.mockResolvedValue({ expires: at(31), max_expires: at(5000) })
    const { container } = render(<SessionBanner />)
    await act(async () => {})
    expect(container).toBeEmptyDOMElement()
  })

  it('shows the countdown inside 30 minutes and extends on click', async () => {
    read.mockResolvedValue({ expires: at(20), max_expires: at(5000) })
    extend.mockResolvedValue({ expires: at(720), max_expires: at(5000) })
    render(<SessionBanner />)
    expect(await screen.findByText(/Session ends in 20 min/)).toBeInTheDocument()
    await userEvent.setup({ advanceTimers: vi.advanceTimersByTime }).click(screen.getByRole('button', { name: 'Extend' }))
    expect(extend).toHaveBeenCalledTimes(1)
    await act(async () => {})
    expect(screen.queryByRole('button', { name: 'Extend' })).toBeNull()
  })

  it('offers no extend at the cap', async () => {
    read.mockResolvedValue({ expires: at(10), max_expires: at(10) })
    render(<SessionBanner />)
    expect(await screen.findByText(/longest stretch/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Extend' })).toBeNull()
  })

  it('says the session ended, and how to start another', async () => {
    read.mockRejectedValue(new ApiError(401, 'not signed in'))
    render(<SessionBanner />)
    expect(await screen.findByRole('alert')).toHaveTextContent(/Session ended.*shepherd web/)
  })

  it('turns to ended when the clock runs out', async () => {
    read.mockResolvedValue({ expires: at(1), max_expires: at(5000) })
    render(<SessionBanner />)
    await screen.findByText(/Session ends in 1 min/)
    await act(async () => { vi.advanceTimersByTime(90_000) })
    expect(screen.getByRole('alert')).toHaveTextContent(/Session ended/)
  })
})
