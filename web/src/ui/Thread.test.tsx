import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Thread, type ThreadEntry } from './Thread'

const items: ThreadEntry[] = [
  { kind: 'you', key: 'a', age: '3m', topics: [1], children: 'Stream it.' },
  {
    kind: 'desk', key: 'b', age: '2m', unseen: true, topics: [1],
    text: 'Needs you:\n1. Answer decision 5. [Answer](#/?open=decision%3A5)\n\n- !41 passed.',
    work: [{ key: 1, kind: 'tool', text: 'decision_list {"state":"open"}' }],
  },
  { kind: 'desk', key: 'c', text: 'Nothing needs you.\n\n- Run 16 passed.', topics: [2] },
  { kind: 'event', key: 'd', glyph: '✓', tone: 'ok', label: 'run passed', age: '26m', children: 'run 16 passed', topics: [2] },
  { kind: 'new', key: 'n', label: '2 new' },
]

describe('Thread', () => {
  it('draws your messages, the desk, event lines and the "n new" divider', () => {
    render(<Thread items={items} />)
    const log = screen.getByRole('log', { name: 'Conversation with the front desk' })
    expect(log).toHaveAttribute('data-component', 'Thread')
    expect(within(log).getByText('Stream it.').closest('.sh-msg--you')).not.toBeNull()
    expect(within(log).getByText('run 16 passed').closest('.sh-event')).not.toBeNull()
    expect(within(log).getByRole('separator')).toHaveTextContent('2 new')
    expect(within(log).getByRole('img', { name: 'unseen' })).toBeInTheDocument()
  })

  it('opens a desk message with "Needs you:" over its list, or "Nothing needs you."', () => {
    render(<Thread items={items} />)
    expect(screen.getByText('Needs you:')).toHaveClass('sh-desk-needs')
    expect(screen.getByText(/Answer decision 5/).closest('ol')).not.toBeNull()
    expect(screen.getByRole('link', { name: 'Answer' })).toHaveAttribute('href', '#/?open=decision%3A5')
    expect(screen.getByText('Nothing needs you.')).toHaveClass('sh-desk-nothing')
    expect(screen.getByText('!41 passed.').closest('ul')).not.toBeNull()
  })

  it('keeps "Show work" closed until opened', async () => {
    render(<Thread items={items} />)
    const fold = screen.getByText('Show work (1 tool call)').closest('details') as HTMLDetailsElement
    expect(fold.open).toBe(false)
    await userEvent.click(screen.getByText('Show work (1 tool call)'))
    expect(fold.open).toBe(true)
    expect(within(fold).getByText(/decision_list/)).toBeInTheDocument()
  })

  it('shows only the rows of a topic when filtered, and none of the divider', () => {
    render(<Thread items={items} topic={2} />)
    expect(screen.queryByText('Stream it.')).toBeNull()
    expect(screen.queryByRole('separator')).toBeNull()
    expect(screen.getByText('run 16 passed')).toBeInTheDocument()
    expect(screen.getByRole('log')).toHaveAttribute('data-filtered', 'true')
  })

  it('shows the empty note when a filter leaves nothing', () => {
    render(<Thread items={items} topic={9} empty={<p>Nothing here.</p>} />)
    expect(screen.getByText('Nothing here.')).toBeInTheDocument()
  })
})
