import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TopicView } from '../model/topic'
import { ACTIVE_WINDOW_MS } from '../model/topic'
import { chipLabel, fitCount, TopicBar } from './TopicBar'

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

  it('shows no chips when no topic is active, and no button of its own for the topic list', async () => {
    const load = loader([t({ status: 'cleared' })])
    render(<TopicBar active={null} onFilter={() => {}} load={load} />)
    await vi.waitFor(() => expect(load).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: 'All' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'All topics' })).toBeNull()
  })

  it('opens the full topic list under the chips when the host asks', async () => {
    const { rerender } = render(<TopicBar active={null} onFilter={() => {}} load={loader([t({ id: 1, title: 'release' })])} />)
    expect(screen.queryByRole('region', { name: 'Topics' })).toBeNull()
    rerender(<TopicBar active={null} onFilter={() => {}} manage load={loader([t({ id: 1, title: 'release' })])} />)
    expect(await screen.findByRole('region', { name: 'Topics' })).toBeTruthy()
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

  describe('short labels and overflow', () => {
    it('cuts a label at a whole word, about 28 characters, and keeps the full title as the tooltip', async () => {
      const title = 'AI code review gate on dev pipelines (ubixsys, OpenCode + OpenRouter)'
      expect(chipLabel(title)).toBe('AI code review gate on dev…')
      expect(chipLabel(title).length).toBeLessThanOrEqual(28)
      expect(chipLabel('short title')).toBe('short title')
      expect(chipLabel('Supercalifragilisticexpialidocious-and-more-words')).toMatch(/^\S{1,27}…$/)
      render(<TopicBar active={null} onFilter={() => {}} load={loader([t({ title })])} />)
      const chip = await screen.findByRole('button', { name: 'AI code review gate on dev…' })
      expect(chip).toHaveAttribute('title', title)
    })

    it('fitCount takes the chips that fit one line, leaving room for +N', () => {
      expect(fitCount([100, 100, 100], 400)).toBe(3)
      expect(fitCount([100, 100, 100], 250)).toBe(1)
      expect(fitCount([100, 100, 100], 50)).toBe(0)
      expect(fitCount([100, 100], 0)).toBe(2)
    })

    it('puts the chips that do not fit behind a +N menu', async () => {
      const user = userEvent.setup()
      const onFilter = vi.fn()
      const w = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(120)
      const c = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(420)
      try {
        const list = [1, 2, 3, 4, 5].map((id) => t({ id, title: `topic ${id}` }))
        render(<TopicBar active={null} onFilter={onFilter} load={loader(list)} />)
        // 420 less All (120) leaves room for one 120 px chip beside the +N button; the other four overflow.
        const more = await screen.findByRole('button', { name: /^\+\d+$/ })
        expect(more).toHaveTextContent('+4')
        expect(document.querySelectorAll('.topic-chip.is-over')).toHaveLength(4)
        await user.click(more)
        await user.click(screen.getByRole('button', { name: 'topic 5' }))
        expect(onFilter).toHaveBeenLastCalledWith(5)
      } finally {
        w.mockRestore()
        c.mockRestore()
      }
    })

    it('keeps the order stable and puts a selected overflow topic in the last visible slot', async () => {
      const w = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(120)
      const c = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(520)
      try {
        const list = [1, 2, 3, 4, 5].map((id) => t({ id, title: `topic ${id}` }))
        const { rerender } = render(<TopicBar active={null} onFilter={() => {}} load={loader(list)} />)
        await screen.findByRole('button', { name: /^\+\d+$/ })
        const visible = () => [...document.querySelectorAll('.topic-chip-t:not(.is-over)')].map((e) => e.textContent)
        expect(visible()).toEqual(['topic 1', 'topic 2'])
        rerender(<TopicBar active={5} onFilter={() => {}} load={loader(list)} />)
        expect(visible()).toEqual(['topic 1', 'topic 5'])
        rerender(<TopicBar active={2} onFilter={() => {}} load={loader(list)} />)
        expect(visible()).toEqual(['topic 1', 'topic 2'])
      } finally {
        w.mockRestore()
        c.mockRestore()
      }
    })
  })
})
