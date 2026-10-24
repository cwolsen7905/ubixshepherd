import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { deskQuestions } from '../api/client'
import type { Api, Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { Live } from '../state/live'
import { spend, status } from '../test/fixtures'
import { QuestionPane } from './questionPane'

vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  deskQuestions: { list: vi.fn(), skip: vi.fn() },
}))

const still: Clock = { now: () => 0, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }
function shown() {
  const api = {
    status: async () => status(), lanes: async () => [], runs: async () => [], decisions: async () => [], requests: async () => [], spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after: number) => ({ items: [], events: [], last: after }),
  } as unknown as Api
  render(
    <LiveProvider live={new Live(api, still)}>
      <QuestionPane id={4} />
    </LiveProvider>,
  )
}
const q = { id: 4, workspace_id: 1, turn: 2, text: 'Which branch should I cut from?', state: 'open' as const, created: '2026-10-08T10:00:00Z' }

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
  it('says so when the question is no longer open', async () => {
    vi.mocked(deskQuestions.list).mockResolvedValue([])
    shown()
    expect(await screen.findByText(/not waiting on you any more/)).toBeInTheDocument()
  })
  it('says when the read failed and reads again on request', async () => {
    const user = userEvent.setup()
    vi.mocked(deskQuestions.list).mockRejectedValueOnce(new Error('down')).mockResolvedValue([q])
    shown()
    expect(await screen.findByText(/Could not read the question/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText('Which branch should I cut from?')).toBeInTheDocument()
  })
})
