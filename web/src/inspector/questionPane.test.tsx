import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { WorkspaceSummary } from '../api/types'
import { deskQuestions } from '../api/client'
import type { Api, Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { Live } from '../state/live'
import { spend, status } from '../test/fixtures'
import { advanceFrom } from '../chat/strip'
import type { DeskThread } from '../state/desk'
import { QuestionPane } from './questionPane'

vi.mock('../chat/strip', async (orig) => ({ ...(await orig<typeof import('../chat/strip')>()), advanceFrom: vi.fn() }))

vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  deskQuestions: { list: vi.fn(), skip: vi.fn(), answer: vi.fn() },
}))

const still: Clock = { now: () => 0, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }
function shown(make?: (w: number) => DeskThread, two = false) {
  const api = {
    status: async () => (two ? status({ workspaces: [...status().workspaces, { ...status().workspaces[0], id: 2, name: 'workspace-2' } as WorkspaceSummary] }) : status()), lanes: async () => [], runs: async () => [], decisions: async () => [], requests: async () => [], spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after: number) => ({ items: [], events: [], last: after }),
  } as unknown as Api
  render(
    <LiveProvider live={new Live(api, still)}>
      <QuestionPane id={4} make={make} />
    </LiveProvider>,
  )
}
const q = { id: 4, workspace_id: 1, turn: 2, text: 'Which branch should I cut from?', state: 'open' as const, created: '2026-10-08T10:00:00Z' }
const other = { id: 5, workspace_id: 1, turn: 3, text: 'Should I tag the release?', state: 'open' as const, created: '2026-10-08T11:00:00Z' }

describe('QuestionPane', () => {
  beforeEach(() => vi.mocked(deskQuestions.list).mockReset())
  it('shows what the desk asked, with Reply and Skip', async () => {
    vi.mocked(deskQuestions.list).mockResolvedValue([q])
    shown()
    expect(await screen.findByText('Which branch should I cut from?')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Skip' })).toBeInTheDocument()
    // The reply row's action carries the question's typed-ID chip.
    expect(screen.getByText('question 4')).toHaveAttribute('data-component', 'IdChip')
  })
  it('lists the open questions and lights the one asked for', async () => {
    vi.mocked(deskQuestions.list).mockResolvedValue([other, q])
    shown()
    await screen.findByText('Which branch should I cut from?')
    expect(screen.getByText('Should I tag the release?')).toBeInTheDocument()
    expect(screen.getByText('Which branch should I cut from?').closest('.sh-row')).toHaveClass('is-selected')
    expect(screen.getByText('Should I tag the release?').closest('.sh-row')).not.toHaveClass('is-selected')
  })
  it('shows the empty state when no question is open', async () => {
    vi.mocked(deskQuestions.list).mockResolvedValue([])
    shown()
    expect(await screen.findByText('No open questions.')).toBeInTheDocument()
  })
  it('says when the read failed and reads again on request', async () => {
    const user = userEvent.setup()
    vi.mocked(deskQuestions.list).mockRejectedValueOnce(new Error('down')).mockResolvedValue([q])
    shown()
    expect(await screen.findByText(/Could not read the questions/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText('Which branch should I cut from?')).toBeInTheDocument()
  })
  it('replies to a question on the thread of its own workspace', async () => {
    const user = userEvent.setup()
    const sends: Record<number, ReturnType<typeof vi.fn>> = { 1: vi.fn(async () => true), 2: vi.fn(async () => true) }
    const make = vi.fn((w: number) => ({ send: sends[w] }) as unknown as DeskThread)
    vi.mocked(deskQuestions.list).mockResolvedValue([q, { ...other, workspace_id: 2 }])
    shown(make, true)
    await screen.findByText('Should I tag the release?')
    await vi.waitFor(() => expect(make).toHaveBeenCalledWith(2))
    const row = screen.getByText('Should I tag the release?').closest('li') as HTMLElement
    await user.click(within(row).getByRole('button', { name: 'Reply here' }))
    await user.type(within(row).getByRole('textbox'), 'yes')
    await user.click(within(row).getByRole('button', { name: 'Send reply' }))
    await vi.waitFor(() => expect(sends[2]).toHaveBeenCalledWith('yes', null, 5))
    expect(sends[1]).not.toHaveBeenCalled()
  })
  it('hands the inspector on to the next waiting item once the last question is cleared', async () => {
    const user = userEvent.setup()
    vi.mocked(advanceFrom).mockClear()
    vi.mocked(deskQuestions.skip).mockResolvedValue(undefined as never)
    vi.mocked(deskQuestions.list).mockResolvedValueOnce([q, other]).mockResolvedValueOnce([other]).mockResolvedValue([])
    shown()
    await screen.findByText('Should I tag the release?')
    const rowOf = (t: string) => screen.getByText(t).closest('li') as HTMLElement
    await user.click(within(rowOf('Which branch should I cut from?')).getByRole('button', { name: 'Skip' }))
    expect(advanceFrom).not.toHaveBeenCalled()
    await user.click(within(rowOf('Should I tag the release?')).getByRole('button', { name: 'Skip' }))
    await vi.waitFor(() => expect(advanceFrom).toHaveBeenCalledTimes(1))
  })
})
