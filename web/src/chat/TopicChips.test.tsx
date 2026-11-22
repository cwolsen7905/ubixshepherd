import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { TopicView } from '../model/topic'
import { TopicChips } from './TopicChips'

const topic = (id: number, title: string, waits = 0): TopicView => ({
  id, title, status: 'open', source: 'desk', created: '', updated: '', activity: new Date().toISOString(), waits, links: [],
})

function chips(list: TopicView[], active: number | null = null) {
  const onFilter = vi.fn()
  const load = vi.fn(async () => ({ topics: list }))
  render(<TopicChips active={active} onFilter={onFilter} load={load} />)
  return { onFilter }
}

describe('the topic switcher', () => {
  afterEach(() => vi.restoreAllMocks())

  it('offers All first, then the open topics in the order given, each with what waits on the person', async () => {
    chips([topic(4, 'release', 2), topic(2, 'docs'), { ...topic(9, 'old'), status: 'cleared' }])
    const group = screen.getByRole('group', { name: 'Filter the thread by topic' })
    expect(group).toHaveAttribute('data-component', 'TopicChips')
    await within(group).findByRole('button', { name: /release/ })
    const names = within(group).getAllByRole('button').map((b) => b.textContent)
    expect(names).toEqual(['All', 'release2', 'docs'])
    expect(within(group).getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(group).queryByRole('button', { name: /old/ })).not.toBeInTheDocument()
  })

  it('is a keyboard list: j and k or the arrows move along it, Home and End go to its ends, Enter picks', async () => {
    const user = userEvent.setup()
    const { onFilter } = chips([topic(1, 'release'), topic(2, 'docs')])
    const all = screen.getByRole('button', { name: 'All' })
    const release = await screen.findByRole('button', { name: 'release' })
    const docs = screen.getByRole('button', { name: 'docs' })

    all.focus()
    await user.keyboard('j')
    expect(release).toHaveFocus()
    await user.keyboard('{ArrowRight}')
    expect(docs).toHaveFocus()
    await user.keyboard('j')
    expect(docs).toHaveFocus()
    await user.keyboard('k')
    expect(release).toHaveFocus()
    await user.keyboard('{ArrowLeft}')
    expect(all).toHaveFocus()
    await user.keyboard('{End}')
    expect(docs).toHaveFocus()
    await user.keyboard('{Home}')
    expect(all).toHaveFocus()

    await user.keyboard('j{Enter}')
    expect(onFilter).toHaveBeenLastCalledWith(1)
    all.focus()
    await user.keyboard('{Enter}')
    expect(onFilter).toHaveBeenLastCalledWith(null)
  })

  it('leaves a key with a modifier, and keys it does not use, alone', async () => {
    const user = userEvent.setup()
    chips([topic(1, 'release')])
    const all = screen.getByRole('button', { name: 'All' })
    await screen.findByRole('button', { name: 'release' })
    all.focus()
    await user.keyboard('{Meta>}j{/Meta}')
    expect(all).toHaveFocus()
    await user.keyboard('x')
    expect(all).toHaveFocus()
  })
})
