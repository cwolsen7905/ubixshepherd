import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { TopicView } from '../model/topic'
import { TopicPill } from './TopicPill'

const t: TopicView = {
  id: 10, title: 'a very long topic title that the pill will truncate', status: 'open', source: 'desk',
  created: '', updated: '', activity: '', waits: 0, links: [],
}

describe('TopicPill', () => {
  it('shows the title, gives the full title on hover and carries the topic hue', () => {
    render(<TopicPill topic={t} active={false} onFilter={() => {}} />)
    const pill = screen.getByRole('button', { name: `Topic: ${t.title}` })
    expect(pill).toHaveAttribute('title', t.title)
    expect(pill).toHaveClass('topic-hue-2')
    expect(pill).toHaveAttribute('aria-pressed', 'false')
  })

  it('filters to the topic, and lifts the filter when it is already on', async () => {
    const user = userEvent.setup()
    const onFilter = vi.fn()
    const { rerender } = render(<TopicPill topic={t} active={false} onFilter={onFilter} />)
    await user.click(screen.getByRole('button'))
    expect(onFilter).toHaveBeenLastCalledWith(10)
    rerender(<TopicPill topic={t} active onFilter={onFilter} />)
    await user.click(screen.getByRole('button'))
    expect(onFilter).toHaveBeenLastCalledWith(null)
  })
})
