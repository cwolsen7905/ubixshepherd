import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ApiError } from '../api/client'
import type { NextList } from '../api/types'
import { Live, type Api, type Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { lane, nextItem, spend, status } from '../test/fixtures'
import { NextPage } from './Next'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

function live() {
  const api: Api = {
    status: async () => status(),
    lanes: async () => [lane({ id: 4, name: 'feat/export-csv', repo: 'acme-api' })],
    runs: async () => [],
    run: async () => { throw new Error('unused') },
    runLog: async () => { throw new Error('unused') },
    runEvents: async () => { throw new Error('unused') },
    decisions: async () => [],
    requests: async () => [],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async () => ({ items: [], events: [], last: 0 }),
    dismissRun: async () => { throw new Error('unused') },
    undismissRun: async () => { throw new Error('unused') },
    answer: async () => { throw new Error('unused') },
  }
  return new Live(api, still)
}

const everything: NextList = {
  next_up: 1,
  items: [
    nextItem({ id: 1, title: 'First approved', rank: 1 }),
    nextItem({ id: 2, title: 'Second approved', rank: 2 }),
    nextItem({ id: 3, title: 'Idea', rank: 3, state: 'planned', stored_state: 'planned', proposed: true, source: 'agent' }),
    nextItem({ id: 4, title: 'Later', rank: 4, state: 'deferred', stored_state: 'deferred' }),
    nextItem({ id: 5, title: 'Shipped', rank: 5, state: 'done', stored_state: 'done', mr: '!40', mr_state: 'merged' }),
  ],
}

function show(api = { add: vi.fn(async () => ({})), update: vi.fn(async () => ({})) }, load = vi.fn(async (all: boolean): Promise<NextList> => (all ? everything : { ...everything, items: everything.items.slice(0, 4) }))) {
  render(
    <LiveProvider live={live()}>
      <NextPage load={load} api={api} />
    </LiveProvider>,
  )
  return { api, load }
}

describe('NextPage', () => {
  beforeEach(() => localStorage.clear())
  afterEach(cleanup)

  it('lists in order with the next marker, and holds done behind a toggle', async () => {
    show()
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    expect(first).toHaveAttribute('aria-current', 'true')
    expect(within(first).getByText('next')).toBeInTheDocument()
    expect(screen.getByText('Second approved').closest('li')).not.toHaveAttribute('aria-current')
    expect(screen.getByText('proposed')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Approved/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Deferred/ })).toBeInTheDocument()
    expect(screen.queryByText('Shipped')).toBeNull()
    await userEvent.click(screen.getByRole('checkbox', { name: /Show done and dropped/ }))
    expect(await screen.findByText('Shipped')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Done/ })).toBeInTheDocument()
  })

  it('changes a state, moves an item and links a lane through the API', async () => {
    const { api } = show()
    const idea = (await screen.findByText('Idea')).closest('li') as HTMLElement
    await userEvent.click(within(idea).getByRole('button', { name: 'Approve' }))
    expect(api.update).toHaveBeenCalledWith(3, { state: 'approved' })
    const second = screen.getByText('Second approved').closest('li') as HTMLElement
    await userEvent.click(within(second).getByRole('button', { name: /up$/ }))
    expect(api.update).toHaveBeenCalledWith(2, { move: 'up' })
    await userEvent.selectOptions(within(second).getByRole('combobox'), 'feat/export-csv')
    expect(api.update).toHaveBeenCalledWith(2, { lane_id: 4 })
  })

  it('cannot move the first item up or the last item down', async () => {
    show()
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    expect(within(first).getByRole('button', { name: /up$/ })).toBeDisabled()
    expect(within(first).getByRole('button', { name: /down$/ })).toBeEnabled()
    const second = screen.getByText('Second approved').closest('li') as HTMLElement
    expect(within(second).getByRole('button', { name: /down$/ })).toBeDisabled()
  })

  it('adds an item as planned, or approved when asked', async () => {
    const { api } = show()
    await screen.findByText('First approved')
    await userEvent.type(screen.getByPlaceholderText(/Add an item/), 'Write the thing')
    await userEvent.type(screen.getByPlaceholderText('repo (optional)'), 'acme-web')
    await userEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(api.add).toHaveBeenCalledWith({ title: 'Write the thing', repo: 'acme-web', state: 'planned' }))
    await userEvent.type(screen.getByPlaceholderText(/Add an item/), 'Now this')
    await userEvent.click(screen.getByRole('checkbox', { name: 'Approved' }))
    await userEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(api.add).toHaveBeenLastCalledWith({ title: 'Now this', repo: 'acme-web', state: 'approved' }))
  })

  it('shows a refusal as the daemon worded it', async () => {
    const api = { add: vi.fn(), update: vi.fn(async () => { throw new ApiError(403, 'only the person may approve') }) }
    show(api)
    const idea = (await screen.findByText('Idea')).closest('li') as HTMLElement
    await userEvent.click(within(idea).getByRole('button', { name: 'Approve' }))
    expect(await screen.findByText('only the person may approve')).toBeInTheDocument()
  })

  it('Start puts the request in the chat draft and goes to the chat page', async () => {
    show()
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    await userEvent.click(within(first).getByRole('button', { name: 'Start' }))
    expect(localStorage.getItem('shepherd.chat.draft.0')).toBe('Open a lane for next-up item 1: First approved')
    expect(window.location.hash).toBe('#/chat')
  })
})
