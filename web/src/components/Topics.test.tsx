import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TopicView } from '../model/topic'
import { ACTIVE_WINDOW_MS } from '../model/topic'
import { Topics } from './Topics'

const t = (over: Partial<TopicView>): TopicView => ({
  id: 1, title: 'release v0.3.0', status: 'open', source: 'desk', created: '', updated: '', activity: new Date().toISOString(), waits: 0, links: [], ...over,
})

describe('Topics', () => {
  it('lists open topics with what waits, filters on click, clears and reopens', async () => {
    const user = userEvent.setup()
    const list = [
      t({ id: 1, waits: 2, links: [{ kind: 'lane', ref: '4', by: 'desk', label: 'feat/x', state: 'open', time: '' }] }),
      t({ id: 2, title: 'manual', status: 'cleared', reopen: true }),
    ]
    const load = vi.fn().mockResolvedValue({ topics: list })
    const change = vi.fn().mockResolvedValue({})
    const onFilter = vi.fn()
    render(<Topics active={null} now={Date.now()} onFilter={onFilter} load={load} change={change} />)

    const main = await screen.findByRole('button', { name: /release v0\.3\.0/ })
    expect(main).toHaveTextContent('waits on you: 2')
    await user.click(main)
    expect(onFilter).toHaveBeenCalledWith(1)

    await user.click(screen.getByRole('button', { name: 'Details (1)' }))
    expect(screen.getByRole('link', { name: /Lane 4: feat\/x/ })).toHaveAttribute('href', '#/lanes/4')

    await user.click(screen.getByRole('button', { name: 'Clear' }))
    expect(change).toHaveBeenCalledWith(1, { status: 'cleared' })

    await user.click(screen.getByRole('button', { name: 'Cleared (1)' }))
    const cleared = screen.getByText('manual').closest('li') as HTMLElement
    expect(within(cleared).getByText(/reopen\?/)).toBeInTheDocument()
    await user.click(within(cleared).getByRole('button', { name: 'Reopen' }))
    expect(change).toHaveBeenCalledWith(2, { status: 'open' })
  })

  it('says why when the daemon refuses', async () => {
    const user = userEvent.setup()
    const load = vi.fn().mockResolvedValue({ topics: [t({})] })
    const change = vi.fn().mockRejectedValue(new Error('not in a turn the person started'))
    render(<Topics active={null} now={Date.now()} onFilter={() => {}} load={load} change={change} />)
    await user.click(await screen.findByRole('button', { name: 'Clear' }))
    await waitFor(() => expect(screen.getByText(/not in a turn/)).toBeInTheDocument())
  })

  describe('idle topics', () => {
    const old = new Date(Date.now() - ACTIVE_WINDOW_MS * 3).toISOString()
    const list = () => [t({ id: 1, title: 'busy' }), t({ id: 2, title: 'quiet', activity: old }), t({ id: 3, title: 'dusty', activity: old })]
    beforeEach(() => window.localStorage.clear())

    it('collapses idle topics under Idle (N), expands on click and remembers it', async () => {
      const user = userEvent.setup()
      const load = vi.fn().mockResolvedValue({ topics: list() })
      const { unmount } = render(<Topics active={null} now={Date.now()} onFilter={() => {}} load={load} />)
      expect(await screen.findByText('busy')).toBeInTheDocument()
      expect(screen.queryByText('quiet')).toBeNull()
      const toggle = screen.getByRole('button', { name: 'Idle (2)' })
      expect(toggle).toHaveAttribute('aria-expanded', 'false')
      await user.click(toggle)
      expect(screen.getByText('quiet')).toBeInTheDocument()
      expect(screen.getByText('dusty')).toBeInTheDocument()
      unmount()
      render(<Topics active={null} now={Date.now()} onFilter={() => {}} load={load} />)
      expect(await screen.findByText('quiet')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Idle (2)' })).toHaveAttribute('aria-expanded', 'true')
    })

    it('shows the selected idle topic among the active ones', async () => {
      const load = vi.fn().mockResolvedValue({ topics: list() })
      render(<Topics active={2} now={Date.now()} onFilter={() => {}} load={load} />)
      expect(await screen.findByText('quiet')).toBeInTheDocument()
      expect(screen.queryByText('dusty')).toBeNull()
      expect(screen.getByRole('button', { name: 'Idle (1)' })).toBeInTheDocument()
    })

    it('has no Idle group when all topics are active', async () => {
      const load = vi.fn().mockResolvedValue({ topics: [t({})] })
      render(<Topics active={null} now={Date.now()} onFilter={() => {}} load={load} />)
      await screen.findByText('release v0.3.0')
      expect(screen.queryByRole('button', { name: /Idle/ })).toBeNull()
    })
  })
})
