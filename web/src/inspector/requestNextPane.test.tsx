import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { nextUp, requestActions } from '../api/client'
import type { Api, Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { Live } from '../state/live'
import { lane, nextItem, request, spend, status } from '../test/fixtures'
import { RequestPane } from './requestPane'
import { NextPane } from './nextPane'

vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  requestActions: { route: vi.fn(), close: vi.fn() },
  nextUp: { list: vi.fn(), add: vi.fn(), update: vi.fn() },
}))

const still: Clock = { now: () => 0, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }
async function shown(ui: React.ReactNode) {
  const api = {
    status: async () => status(),
    lanes: async () => [lane({ id: 1, name: 'feat/login' }), lane({ id: 2, name: 'feat/billing' })],
    runs: async () => [],
    decisions: async () => [],
    requests: async () => [request({ id: 3 })],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after: number) => ({ items: [], events: [], last: after }),
    next: async () => ({ items: [nextItem({ id: 5, note: 'Do the thing', lane_id: 2, lane_name: 'feat/billing' })] }),
  } as unknown as Api
  const live = new Live(api, still, async () => ({ count: 0, counts: { decisions: 0, mrs: 0, ship: 0, next: 0 }, items: [] }))
  await act(async () => {
    await live.refresh()
    await live.readWaiting()
  })
  render(<LiveProvider live={live}>{ui}</LiveProvider>)
}

describe('RequestPane', () => {
  beforeEach(() => {
    vi.mocked(requestActions.route).mockReset().mockResolvedValue({} as never)
    vi.mocked(requestActions.close).mockReset().mockResolvedValue({} as never)
  })
  it('shows the request, who asked and the lanes to send it to', async () => {
    await shown(<RequestPane id={3} />)
    expect(screen.getByText('What should we do about authentication?')).toBeInTheDocument()
    expect(screen.getByText('needs routing')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'acme-api:feat/login' })).toBeInTheDocument()
    const options = screen.getAllByRole('option').map((o) => o.textContent)
    expect(options).toEqual(['Choose a lane…', 'acme-api:feat/billing'])
  })
  it('routes to the chosen lane', async () => {
    const user = userEvent.setup()
    await shown(<RequestPane id={3} />)
    expect(screen.getByRole('button', { name: 'Route request' })).toBeDisabled()
    await user.selectOptions(screen.getByLabelText('Lane to route to'), '2')
    await user.click(screen.getByRole('button', { name: 'Route request' }))
    expect(requestActions.route).toHaveBeenCalledWith(3, { lane: 'feat/billing' })
  })
  it('closes with a reason after a confirm', async () => {
    const user = userEvent.setup()
    await shown(<RequestPane id={3} />)
    await user.click(screen.getByRole('button', { name: 'Close without routing' }))
    await user.type(screen.getByLabelText(/Reason/), 'stale')
    await user.click(screen.getByRole('button', { name: 'Close request' }))
    expect(requestActions.close).toHaveBeenCalledWith(3, 'stale')
  })
  it('says so when the request is gone', async () => {
    await shown(<RequestPane id={99} />)
    expect(screen.getByText(/not waiting on you any more/)).toBeInTheDocument()
  })
  it('shows the daemon\'s refusal', async () => {
    vi.mocked(requestActions.route).mockRejectedValue(new Error('request 3 is routed'))
    const user = userEvent.setup()
    await shown(<RequestPane id={3} />)
    await user.selectOptions(screen.getByLabelText('Lane to route to'), '2')
    await user.click(screen.getByRole('button', { name: 'Route request' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('request 3 is routed')
  })
})

describe('NextPane', () => {
  beforeEach(() => vi.mocked(nextUp.update).mockReset().mockResolvedValue({} as never))
  it('shows the item, its note, plan and lane', async () => {
    await shown(<NextPane id={5} />)
    expect(screen.getByText(/Lane diff step 3/)).toBeInTheDocument()
    expect(screen.getByText('Do the thing')).toBeInTheDocument()
    expect(screen.getByText('docs/lane-diff.md')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'feat/billing' })).toBeInTheDocument()
  })
  it('defers through the same update the page uses', async () => {
    const user = userEvent.setup()
    await shown(<NextPane id={5} />)
    await user.click(screen.getByRole('button', { name: 'Defer' }))
    expect(nextUp.update).toHaveBeenCalledWith(5, { state: 'deferred' })
  })
  it('says so when the item left the open list', async () => {
    await shown(<NextPane id={42} />)
    expect(screen.getByText(/not on the open list any more/)).toBeInTheDocument()
  })
})
