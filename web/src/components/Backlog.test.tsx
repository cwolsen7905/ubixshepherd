import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { nextItem, status } from '../test/fixtures'
import { Backlog } from './Backlog'

const list = {
  items: [nextItem({ id: 1, title: 'First thing' }), nextItem({ id: 2, title: 'Second thing', repo: 'acme-web', plan: undefined })],
  next_up: 1,
}

function show(onStart = vi.fn()) {
  render(
    <Backlog
      items={[]}
      now={Date.parse('2026-10-08T10:00:00Z')}
      spend={null}
      onJump={() => {}}
      onStart={onStart}
      loadStatus={async () => status()}
      load={async () => list}
    />,
  )
  return onStart
}

describe('Backlog rail views', () => {
  beforeEach(() => localStorage.clear())
  afterEach(cleanup)

  it('starts on Now and shows the next-up items on the toggle', async () => {
    show()
    expect(screen.getByText('Nothing is waiting on you and no lane is idle.')).toBeInTheDocument()
    const group = screen.getByRole('group', { name: 'Rail view' })
    expect(within(group).getByRole('button', { name: 'Now' })).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(await within(group).findByRole('button', { name: 'Next up 2' }))
    expect(await screen.findByText('First thing')).toBeInTheDocument()
    expect(screen.getByText('Second thing')).toBeInTheDocument()
    expect(screen.queryByText('Nothing is waiting on you and no lane is idle.')).toBeNull()
    expect(localStorage.getItem('shepherd.chat.rail')).toBe('next')
    expect(screen.getByRole('link', { name: 'Next up page' })).toHaveAttribute('href', '#/next')
    expect(screen.getByText('acme-api · docs/lane-diff.md')).toBeInTheDocument()
  })

  it('remembers the view and marks the first item as next', async () => {
    localStorage.setItem('shepherd.chat.rail', 'next')
    show()
    const row = (await screen.findByText('First thing')).closest('li')
    expect(row).toHaveAttribute('aria-current', 'true')
    expect(screen.getByText('Second thing').closest('li')).not.toHaveAttribute('aria-current')
  })

  it('Start hands the request to the chat draft', async () => {
    localStorage.setItem('shepherd.chat.rail', 'next')
    const onStart = show()
    const row = (await screen.findByText('Second thing')).closest('li') as HTMLElement
    await userEvent.click(within(row).getByRole('button', { name: 'Start' }))
    await waitFor(() => expect(onStart).toHaveBeenCalledWith('Open a lane for next-up item 2: Second thing'))
  })
})
