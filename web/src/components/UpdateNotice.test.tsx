import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { UpdateStatus } from '../api/types'
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
})
