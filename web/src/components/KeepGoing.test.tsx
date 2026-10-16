import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ApiError } from '../api/client'
import { KeepGoing, detail, type KeepGoingStatus } from './KeepGoing'

const st = (o: Partial<KeepGoingStatus> = {}): KeepGoingStatus => ({
  on: false, spent_usd: 0, cap_usd: 5, runs_now: 0, max_runs: 3, ...o,
})

describe('KeepGoing', () => {
  it('shows an off switch and posts nothing until clicked', async () => {
    const set = vi.fn()
    render(<KeepGoing load={() => Promise.resolve(st())} set={set} />)
    const sw = await screen.findByRole('switch', { name: 'Keep going' })
    expect(sw).toHaveAttribute('aria-checked', 'false')
    expect(set).not.toHaveBeenCalled()
  })

  it('turns on by a click and shows spend against the cap and runs', async () => {
    const user = userEvent.setup()
    const set = vi.fn((on: boolean) => Promise.resolve(st({ on, spent_usd: 1.2, runs_now: 2 })))
    render(<KeepGoing load={() => Promise.resolve(st())} set={set} />)
    await user.click(await screen.findByRole('switch'))
    expect(set).toHaveBeenCalledWith(true)
    await waitFor(() => expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true'))
    expect(screen.getByText('$1.20 of $5.00, 2 of 3 runs')).toBeInTheDocument()
  })

  it('shows a progress bar when on with a cap', async () => {
    render(<KeepGoing load={() => Promise.resolve(st({ on: true, spent_usd: 1.5, runs_now: 1 }))} set={vi.fn()} />)
    const bar = await screen.findByRole('progressbar', { name: 'Keep going spend' })
    expect(bar).toHaveAttribute('aria-valuemin', '0')
    expect(bar).toHaveAttribute('aria-valuemax', '5')
    expect(bar).toHaveAttribute('aria-valuenow', '1.5')
    expect(bar).toHaveAttribute('aria-valuetext', '$1.50 of $5.00, 1 of 3 runs')
    expect(bar).toHaveAttribute('style', '--kg-fill-pct: 30%;')
  })

  it('hides the progress bar when off', async () => {
    render(<KeepGoing load={() => Promise.resolve(st({ cap_usd: 5 }))} set={vi.fn()} />)
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })

  it('hides the progress bar when no cap', async () => {
    render(<KeepGoing load={() => Promise.resolve(st({ on: true, cap_usd: 0, spent_usd: 1.5, max_runs: 0 }))} set={vi.fn()} />)
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })

  it('clamps bar and aria when spend exceeds cap', async () => {
    render(<KeepGoing load={() => Promise.resolve(st({ on: true, spent_usd: 7, runs_now: 3 }))} set={vi.fn()} />)
    const bar = await screen.findByRole('progressbar', { name: 'Keep going spend' })
    expect(bar).toHaveAttribute('aria-valuenow', '5')
    expect(bar).toHaveAttribute('style', '--kg-fill-pct: 100%;')
    expect(bar).toHaveClass('tone-warn')
  })

  it('applies warn tone when at 80% of cap', async () => {
    render(<KeepGoing load={() => Promise.resolve(st({ on: true, spent_usd: 4, runs_now: 2 }))} set={vi.fn()} />)
    const bar = await screen.findByRole('progressbar', { name: 'Keep going spend' })
    expect(bar).toHaveClass('tone-warn')
    expect(screen.getByText('$4.00 of $5.00, 2 of 3 runs')).toHaveClass('tone-warn')
  })

  it('turns off by a click', async () => {
    const user = userEvent.setup()
    const set = vi.fn((on: boolean) => Promise.resolve(st({ on })))
    render(<KeepGoing load={() => Promise.resolve(st({ on: true }))} set={set} />)
    await user.click(await screen.findByRole('switch'))
    expect(set).toHaveBeenCalledWith(false)
    await waitFor(() => expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false'))
  })

  it('says why a change was refused and leaves the switch off', async () => {
    const user = userEvent.setup()
    const set = vi.fn(() => Promise.reject(new ApiError(403, 'only the person turns it on')))
    render(<KeepGoing load={() => Promise.resolve(st())} set={set} />)
    await user.click(await screen.findByRole('switch'))
    expect(await screen.findByRole('alert')).toHaveTextContent('only the person turns it on')
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
  })

  it('shows nothing when the status cannot be read', async () => {
    const load = vi.fn(() => Promise.reject(new Error('404')))
    render(<KeepGoing load={load} set={vi.fn()} />)
    await waitFor(() => expect(load).toHaveBeenCalled())
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
  })
})

describe('detail', () => {
  const now = Date.parse('2026-10-14T12:00:00Z')
  it('warns from 80% of the cap', () => {
    expect(detail(st({ on: true, spent_usd: 3.9 }), now)?.warn).toBe(false)
    expect(detail(st({ on: true, spent_usd: 4 }), now)?.warn).toBe(true)
  })
  it('names a recent stop, not an old one', () => {
    const off = (iso: string) => st({ reason_off: 'idle', off_at: iso })
    expect(detail(off('2026-10-14T11:30:00Z'), now)?.text).toBe('off: idle too long')
    expect(detail(off('2026-10-14T09:00:00Z'), now)).toBeNull()
    expect(detail(st({ reason_off: 'person', off_at: '2026-10-14T11:59:00Z' }), now)).toBeNull()
  })
  it('has no spend part without a cap', () => {
    expect(detail(st({ on: true, cap_usd: 0, max_runs: 0 }), now)).toBeNull()
  })
})
