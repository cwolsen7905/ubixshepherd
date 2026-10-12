import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TopicView } from '../model/topic'
import { ACTIVE_WINDOW_MS } from '../model/topic'
import { TopicBar } from './TopicBar'

const t = (over: Partial<TopicView>): TopicView => ({
  id: 1, title: 'release v0.3.0', status: 'open', source: 'desk', created: '', updated: '', activity: '', waits: 0, links: [], ...over,
})
const loader = (topics: TopicView[]) => vi.fn().mockResolvedValue({ topics })

describe('TopicBar', () => {
  it('shows All selected with no filter, and a chip per active topic with its waits', async () => {
    render(<TopicBar active={null} onFilter={() => {}} load={loader([t({ waits: 2 }), t({ id: 2, title: 'docs' })])} />)
    expect(await screen.findByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
    const chip = screen.getByRole('button', { name: /release v0\.3\.0/ })
    expect(chip).toHaveAttribute('aria-pressed', 'false')
    expect(chip).toHaveTextContent('2')
    expect(screen.getByRole('button', { name: 'docs' })).toBeInTheDocument()
  })

  it('a chip sets the filter and All clears it', async () => {
    const user = userEvent.setup()
    const onFilter = vi.fn()
    const { rerender } = render(<TopicBar active={null} onFilter={onFilter} load={loader([t({}), t({ id: 2, title: 'docs' })])} />)
    await user.click(await screen.findByRole('button', { name: 'docs' }))
    expect(onFilter).toHaveBeenLastCalledWith(2)
    rerender(<TopicBar active={2} onFilter={onFilter} load={loader([t({}), t({ id: 2, title: 'docs' })])} />)
    expect(screen.getByRole('button', { name: 'docs' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'false')
    await user.click(screen.getByRole('button', { name: 'All' }))
    expect(onFilter).toHaveBeenLastCalledWith(null)
  })

  it('leaves cleared topics out', async () => {
    render(<TopicBar active={null} onFilter={() => {}} load={loader([t({}), t({ id: 2, title: 'manual', status: 'cleared' })])} />)
    await screen.findByRole('button', { name: /release/ })
    expect(screen.queryByRole('button', { name: /manual/ })).toBeNull()
  })

  it('is hidden when no topic is active', async () => {
    const load = loader([t({ status: 'cleared' })])
    render(<TopicBar active={null} onFilter={() => {}} load={load} />)
    await vi.waitFor(() => expect(load).toHaveBeenCalled())
    expect(screen.queryByRole('group')).toBeNull()
  })

  describe('idle topics', () => {
    const old = new Date(Date.now() - ACTIVE_WINDOW_MS * 3).toISOString()
    const fresh = new Date().toISOString()
    const list = () => [
      t({ id: 1, title: 'busy', activity: fresh }),
      t({ id: 2, title: 'quiet', activity: old }),
      t({ id: 3, title: 'dusty', activity: old }),
    ]
    beforeEach(() => window.localStorage.clear())

    it('hides idle chips behind +N idle, which reveals them and is remembered', async () => {
      const user = userEvent.setup()
      const { unmount } = render(<TopicBar active={null} onFilter={() => {}} load={loader(list())} />)
      expect(await screen.findByRole('button', { name: 'busy' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'quiet' })).toBeNull()
      await user.click(screen.getByRole('button', { name: '+2 idle' }))
      expect(screen.getByRole('button', { name: 'quiet' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'dusty' })).toBeInTheDocument()
      unmount()
      render(<TopicBar active={null} onFilter={() => {}} load={loader(list())} />)
      expect(await screen.findByRole('button', { name: 'quiet' })).toBeInTheDocument()
      await user.click(screen.getByRole('button', { name: 'Hide 2 idle' }))
      expect(screen.queryByRole('button', { name: 'quiet' })).toBeNull()
    })

    it('keeps the selected topic visible while idle is collapsed', async () => {
      render(<TopicBar active={2} onFilter={() => {}} load={loader(list())} />)
      expect(await screen.findByRole('button', { name: 'quiet' })).toHaveAttribute('aria-pressed', 'true')
      expect(screen.queryByRole('button', { name: 'dusty' })).toBeNull()
      expect(screen.getByRole('button', { name: '+1 idle' })).toBeInTheDocument()
    })

    it('shows no idle chip when every topic is active', async () => {
      render(<TopicBar active={null} onFilter={() => {}} load={loader([t({ activity: fresh })])} />)
      await screen.findByRole('button', { name: /release/ })
      expect(screen.queryByRole('button', { name: /idle/ })).toBeNull()
    })
  })
})
