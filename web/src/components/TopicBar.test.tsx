import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { TopicView } from '../model/topic'
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
})
