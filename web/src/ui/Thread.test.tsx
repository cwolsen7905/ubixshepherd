import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Thread, type ThreadEntry } from './Thread'

const NOW = Date.parse('2026-11-22T12:00:00Z')

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
    render(<Thread items={items} now={NOW} />)
    const log = screen.getByRole('log', { name: 'Conversation with the front desk' })
    expect(log).toHaveAttribute('data-component', 'Thread')
    expect(within(log).getByText('Stream it.').closest('.sh-msg--you')).not.toBeNull()
    expect(within(log).getByText('run 16 passed').closest('.sh-event')).not.toBeNull()
    expect(within(log).getByRole('separator')).toHaveTextContent('2 new')
    expect(within(log).getByRole('img', { name: 'unseen' })).toBeInTheDocument()
  })

  it('opens a desk message with "Needs you:" over its list, or "Nothing needs you."', () => {
    render(<Thread items={items} now={NOW} />)
    expect(screen.getByText('Needs you:')).toHaveClass('sh-desk-needs')
    expect(screen.getByText(/Answer decision 5/).closest('ol')).not.toBeNull()
    expect(screen.getByRole('link', { name: 'Answer' })).toHaveAttribute('href', '#/?open=decision%3A5')
    expect(screen.getByText('Nothing needs you.')).toHaveClass('sh-desk-nothing')
    expect(screen.getByText('!41 passed.').closest('ul')).not.toBeNull()
  })

  it('keeps "Show work" closed until opened', async () => {
    render(<Thread items={items} now={NOW} />)
    const fold = screen.getByText('Show work (1 tool call)').closest('details') as HTMLDetailsElement
    expect(fold.open).toBe(false)
    await userEvent.click(screen.getByText('Show work (1 tool call)'))
    expect(fold.open).toBe(true)
    expect(within(fold).getByText(/decision_list/)).toBeInTheDocument()
  })

  it('shows only the rows of a topic when filtered, and none of the divider', () => {
    render(<Thread items={items} topic={2} now={NOW} />)
    expect(screen.queryByText('Stream it.')).toBeNull()
    expect(screen.queryByRole('separator')).toBeNull()
    expect(screen.getByText('run 16 passed')).toBeInTheDocument()
    expect(screen.getByRole('log')).toHaveAttribute('data-filtered', 'true')
  })

  it('shows the empty note when a filter leaves nothing', () => {
    render(<Thread items={items} topic={9} now={NOW} empty={<p>Nothing here.</p>} />)
    expect(screen.getByText('Nothing here.')).toBeInTheDocument()
  })
})

describe('Thread times', () => {
  // Local times, so the tests hold in any time zone.
  const at = (d: number, h: number, m: number) => new Date(2026, 10, d, h, m).toISOString()
  const now = new Date(2026, 10, 22, 14, 10).getTime()
  const dated: ThreadEntry[] = [
    { kind: 'you', key: 'a', at: at(20, 9, 3), age: '2d', children: 'Morning.' },
    { kind: 'desk', key: 'b', at: at(20, 9, 4), age: '2d', text: 'Nothing needs you.' },
    { kind: 'event', key: 'c', at: at(21, 0, 30), glyph: '✓', tone: 'ok', label: 'run passed', age: '1d', children: 'run 16 passed' },
    { kind: 'new', key: 'n', label: '2 new' },
    { kind: 'you', key: 'd', at: at(22, 14, 5), age: '5m', children: 'Status?' },
    { kind: 'desk', key: 'e', at: at(22, 14, 6), age: '4m', text: 'All green.' },
    { kind: 'desk', key: 'f', text: 'Still arriving', partial: true },
  ]

  it('shows a 24-hour clock time on messages and event lines, the relative age in its title', () => {
    render(<Thread items={dated} now={now} />)
    const you = screen.getByText('Morning.').closest('.sh-msg') as HTMLElement
    const clock = within(you).getByText('09:03')
    expect(clock.tagName).toBe('TIME')
    expect(clock).toHaveClass('sh-clock')
    expect(clock).toHaveAttribute('dateTime', at(20, 9, 3))
    expect(clock.getAttribute('title')).toMatch(/Nov 20, 2026 09:03, 2d ago$/)
    expect(within(you).queryByText('2d')).toBeNull()
    expect(within(screen.getByText('run 16 passed').closest('.sh-event') as HTMLElement).getByText('00:30')).toBeInTheDocument()
    expect(within(screen.getByText('All green.').closest('.sh-msg') as HTMLElement).getByText('14:06')).toBeInTheDocument()
  })

  it('puts a date line where the day changes, ahead of the "n new" divider', () => {
    render(<Thread items={dated} now={now} />)
    const [day1, day2, today, divider] = screen.getAllByRole('separator') as [HTMLElement, HTMLElement, HTMLElement, HTMLElement]
    const lines = [day1, day2, today, divider]
    expect(lines.map((l) => l.textContent)).toEqual(['Friday, Nov 20', 'Yesterday', 'Today', '2 new'])
    expect(today).toHaveAttribute('aria-label', 'Today, Sunday, November 22, 2026')
    // Each line comes just before the first row of its day.
    expect(day1.nextElementSibling).toHaveTextContent('Morning.')
    expect(day2.nextElementSibling).toHaveTextContent('run 16 passed')
    expect(today.nextElementSibling).toBe(divider)
  })

  it('adds the year to a date line from another year, and keeps the age where a row has no time', () => {
    render(
      <Thread
        now={now}
        items={[
          { kind: 'you', key: 'a', at: new Date(2025, 11, 31, 23, 59).toISOString(), age: '1y', children: 'Last year.' },
          { kind: 'you', key: 'b', age: '3m', children: 'Undated.' },
        ]}
      />,
    )
    expect(screen.getByRole('separator')).toHaveTextContent('Wednesday, Dec 31, 2025')
    expect(screen.getByText('23:59')).toBeInTheDocument()
    expect(within(screen.getByText('Undated.').closest('.sh-msg') as HTMLElement).getByText('3m')).toBeInTheDocument()
  })

  it('counts days within a topic filter', () => {
    render(<Thread items={dated.map((it) => (it.key === 'd' ? { ...it, topics: [4] } : it))} topic={4} now={now} />)
    expect(screen.getAllByRole('separator').map((l) => l.textContent)).toEqual(['Today'])
  })
})

describe('Thread clock', () => {
  it('ticks no clock of its own when the caller passes one', () => {
    const tick = vi.spyOn(window, 'setInterval')
    render(<Thread items={items} now={NOW} />)
    expect(tick).not.toHaveBeenCalled()
    tick.mockRestore()
  })
})
