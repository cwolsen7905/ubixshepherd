import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ApiError } from '../api/client'
import type { MatrixRow, NextItem, NextList } from '../api/types'
import { Live, type Api, type Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { lane, nextItem, spend, status } from '../test/fixtures'
import { refocus } from '../router'
import { NextPage } from './Next'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

function live(feedItems: { id: number; kind: string; text: string; created: string }[] = []) {
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
    feedLatest: async () => ({ items: [], events: [], last: feedItems.at(-1)?.id ?? 0 }),
    feed: async (after) => {
      const items = feedItems.filter((f) => f.id > after)
      return { items, events: items.map((f) => f.kind), last: feedItems.at(-1)?.id ?? after }
    },
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
    nextItem({ id: 6, title: 'Agreed', rank: 4, state: 'planned', stored_state: 'planned', source: 'chat' }),
    nextItem({ id: 4, title: 'Later', rank: 4, state: 'deferred', stored_state: 'deferred' }),
    nextItem({ id: 5, title: 'Shipped', rank: 5, state: 'done', stored_state: 'done', mr: '!40', mr_state: 'merged' }),
  ] as NextItem[],
}

const matrixList: NextList = {
  ...everything,
  matrix: [
    nextItem({ id: 0, key: 'matrix:acme-api:W2', row_id: 'W2', title: 'Typed work orders', state: 'planned', stored_state: 'planned', source: 'matrix', read_only: true, area: 'core', repo: 'acme-api', plan: undefined, rank: 0 }),
    nextItem({ id: 0, key: 'matrix:acme-api:W3', row_id: 'W3', title: 'Needs a decision', source: 'matrix', read_only: true, repo: 'acme-api', plan: undefined, rank: 0, mr: '#12', pr_url: 'https://example.com/pull/12' }),
  ].map((i): MatrixRow => {
    const state = i.row_id === 'W3' ? 'blocked' : 'planned'
    return { ...i, state, stored_state: state }
  }),
}

function show(api = { add: vi.fn(async () => ({})), update: vi.fn(async () => ({})) }, load = vi.fn(async (all: boolean): Promise<NextList> => (all ? everything : { ...everything, items: everything.items.slice(0, 5) }))) {
  render(
    <LiveProvider live={live()}>
      <NextPage load={load} api={api} />
    </LiveProvider>,
  )
  return { api, load }
}

describe('NextPage', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => {
    cleanup()
    window.location.hash = ''
  })

  describe('focus', () => {
    let scrolled: HTMLElement[]
    beforeEach(() => {
      scrolled = []
      Element.prototype.scrollIntoView = vi.fn(function (this: HTMLElement) {
        scrolled.push(this)
      })
    })

    it('scrolls the focused row to the middle and marks it, once the list has loaded', async () => {
      window.location.hash = '#/next?item=2'
      show()
      const row = (await screen.findByText('Second approved')).closest('li') as HTMLElement
      await waitFor(() => expect(row).toHaveClass('is-focus-target'))
      expect(row).toHaveAttribute('aria-current', 'true')
      expect(scrolled).toEqual([row])
      expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ block: 'center' })
      expect(screen.getByText('Idea').closest('li')).not.toHaveClass('is-focus-target')
    })

    it('selects the focused item so its detail shows beside the list', async () => {
      window.location.hash = '#/next?item=2'
      show()
      await screen.findByText('Second approved')
      expect(await screen.findByRole('complementary', { name: /Details for Second approved/ })).toBeInTheDocument()
    })

    it('does not scroll or mark anything without a focus', async () => {
      show()
      await screen.findByText('Second approved')
      expect(scrolled).toEqual([])
      expect(document.querySelector('.is-focus-target')).toBeNull()
    })

    it('focuses again on a refocus, and on a new item in the route', async () => {
      window.location.hash = '#/next?item=2'
      show()
      await waitFor(() => expect(scrolled).toHaveLength(1))
      act(() => refocus())
      await waitFor(() => expect(scrolled).toHaveLength(2))
      act(() => {
        window.location.hash = '#/next?item=3'
      })
      await waitFor(() => expect(scrolled).toHaveLength(3))
      expect(scrolled[2]).toBe((screen.getByText('Idea').closest('li')) as HTMLElement)
    })
  })

  it('lists in order with the next marker, and holds done behind a toggle', async () => {
    show()
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    expect(first).toHaveAttribute('aria-current', 'true')
    expect(within(first).getByText('next')).toBeInTheDocument()
    expect(screen.getByText('Second approved').closest('li')).not.toHaveAttribute('aria-current')
    expect(screen.getByText('proposed')).toBeInTheDocument()
    const chat = screen.getByText('Agreed').closest('li') as HTMLElement
    expect(within(chat).getByText('chat')).toBeInTheDocument()
    expect(within(chat).queryByText('proposed')).toBeNull()
    const queue = screen.getByRole('region', { name: /Queue/ })
    expect(within(queue).getByText('First approved')).toBeInTheDocument()
    expect(within(queue).getByText('deferred')).toBeInTheDocument()
    expect(screen.queryByText('Shipped')).toBeNull()
    await userEvent.click(screen.getByRole('checkbox', { name: /Show done and dropped/ }))
    expect(await screen.findByText('Shipped')).toBeInTheDocument()
    const queue2 = screen.getByRole('region', { name: /Queue/ })
    expect(within(queue2).getByText('Done and dropped')).toBeInTheDocument()
  })

  it('heads the page with counts and the hint, and numbers the queue', async () => {
    show()
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    expect(within(first).getByText('1')).toBeInTheDocument()
    expect(screen.getByText(/^Open \d+$/)).toBeInTheDocument()
    expect(screen.getByText(/^Started \d+$/)).toBeInTheDocument()
    expect(screen.getByText(/^Rejected \d+$/)).toBeInTheDocument()
    expect(screen.getByText('Starts when a slot frees, while Keep going is on')).toBeInTheDocument()
  })

  it('selects a row and opens the detail panel', async () => {
    show()
    const idea = (await screen.findByText('Idea')).closest('li') as HTMLElement
    await userEvent.click(idea)
    expect(idea).toHaveClass('is-selected')
    expect(screen.getByRole('complementary', { name: /Details for Idea/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Approve and queue' })).toBeInTheDocument()
  })

  it('changes a state, moves an item and links a lane through the detail panel', async () => {
    const { api } = show()
    await screen.findByText('Idea')
    await userEvent.click(screen.getByText('Idea').closest('li') as HTMLElement)
    await userEvent.click(screen.getByRole('button', { name: 'Approve and queue' }))
    expect(api.update).toHaveBeenCalledWith(3, { state: 'approved' })

    const second = screen.getByText('Second approved').closest('li') as HTMLElement
    await userEvent.click(second)
    await userEvent.click(screen.getByRole('button', { name: /up$/ }))
    expect(api.update).toHaveBeenCalledWith(2, { move: 'up' })

    await userEvent.selectOptions(screen.getByLabelText('Link to lane'), '4')
    expect(api.update).toHaveBeenCalledWith(2, { lane_id: 4 })
  })

  it('cannot move the first item up or the last item down', async () => {
    show()
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    await userEvent.click(first)
    expect(screen.getByRole('button', { name: /up$/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /down$/ })).toBeEnabled()

    const second = screen.getByText('Second approved').closest('li') as HTMLElement
    await userEvent.click(second)
    expect(screen.getByRole('button', { name: /down$/ })).toBeDisabled()
  })

  it('adds an item as planned, or approved when asked', async () => {
    const { api } = show()
    await screen.findByText('First approved')
    await userEvent.type(screen.getByPlaceholderText(/Add to next up/), 'Write the thing')
    await userEvent.type(screen.getByPlaceholderText('repo (optional)'), 'acme-web')
    await userEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(api.add).toHaveBeenCalledWith({ title: 'Write the thing', repo: 'acme-web', state: 'planned' }))
    await userEvent.type(screen.getByPlaceholderText(/Add to next up/), 'Now this')
    await userEvent.click(screen.getByRole('checkbox', { name: 'Approved' }))
    await userEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(api.add).toHaveBeenLastCalledWith({ title: 'Now this', repo: 'acme-web', state: 'approved' }))
  })

  it('shows a refusal as the daemon worded it', async () => {
    const api = { add: vi.fn(), update: vi.fn(async () => { throw new ApiError(403, 'only the person may approve') }) }
    show(api)
    await screen.findByText('Idea')
    await userEvent.click(screen.getByText('Idea').closest('li') as HTMLElement)
    await userEvent.click(screen.getByRole('button', { name: 'Approve and queue' }))
    expect(await screen.findByText('only the person may approve')).toBeInTheDocument()
  })

  it('Start puts the request in the chat draft and goes to the chat page', async () => {
    show()
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    await userEvent.click(first)
    await userEvent.click(screen.getByRole('button', { name: 'Start' }))
    expect(localStorage.getItem('shepherd.chat.draft.0')).toBe('Open a lane for next-up item 1: First approved')
    expect(window.location.hash).toBe('#/')
  })

  it('a startable approved item still offers Defer and Drop beside Start', async () => {
    show()
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    await userEvent.click(first)
    expect(screen.getByRole('button', { name: 'Start' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Defer' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Drop' })).toBeInTheDocument()
  })

  it('shows matrix rows read-only, blocked first, with no actions', async () => {
    const load = vi.fn(async (): Promise<NextList> => matrixList)
    render(
      <LiveProvider live={live()}>
        <NextPage load={load} api={{ add: vi.fn(), update: vi.fn() }} />
      </LiveProvider>,
    )
    const todo = (await screen.findByText('Typed work orders')).closest('li') as HTMLElement
    const blocked = screen.getByText('Needs a decision').closest('li') as HTMLElement
    expect(screen.getByText('From work matrix')).toBeInTheDocument()
    expect(blocked.compareDocumentPosition(todo) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(todo).queryByRole('button')).toBeNull()
    expect(within(blocked).getByRole('link', { name: /PR #12/ })).toHaveAttribute('href', 'https://example.com/pull/12')
    expect(within(todo).getByText('matrix')).toBeInTheDocument()
  })

  it('shows proposed items as waiting on you, apart from the queue and the matrix', async () => {
    const api = { add: vi.fn(async () => ({})), update: vi.fn(async () => ({})) }
    const load = vi.fn(async (): Promise<NextList> => matrixList)
    render(
      <LiveProvider live={live()}>
        <NextPage load={load} api={api} />
      </LiveProvider>,
    )
    await screen.findByText('First approved')
    const waiting = screen.getByRole('region', { name: /Waiting for your approval/ })
    expect(within(waiting).getByText('Idea')).toBeInTheDocument()
    expect(within(waiting).queryByText('First approved')).toBeNull()
    const queue = screen.getByRole('region', { name: /Queue/ })
    expect(within(queue).getByText('First approved')).toBeInTheDocument()
    expect(within(queue).getByText('Later')).toBeInTheDocument()
    expect(within(queue).queryByText('Idea')).toBeNull()
    const matrix = screen.getByRole('region', { name: /From work matrix/ })
    expect(within(matrix).getByText('Typed work orders')).toBeInTheDocument()
    expect(within(queue).queryByText('Typed work orders')).toBeNull()
    await userEvent.click(within(waiting).getByText('Idea'))
    await userEvent.click(screen.getByRole('button', { name: 'Approve and queue' }))
    expect(api.update).toHaveBeenCalledWith(3, { state: 'approved' })
  })

  it('does not count a proposed item above the queue when enabling move up', async () => {
    const list: NextList = {
      next_up: 0,
      items: [
        nextItem({ id: 1, title: 'Idea', rank: 1, state: 'planned', stored_state: 'planned', proposed: true, source: 'agent' }),
        nextItem({ id: 2, title: 'Agreed plan', rank: 2, state: 'planned', stored_state: 'planned' }),
      ] as NextItem[],
    }
    render(
      <LiveProvider live={live()}>
        <NextPage load={async () => list} api={{ add: vi.fn(), update: vi.fn() }} />
      </LiveProvider>,
    )
    await screen.findByText('Agreed plan')
    await userEvent.click(screen.getByText('Agreed plan').closest('li') as HTMLElement)
    expect(screen.getByRole('button', { name: 'Move Agreed plan up' })).toBeDisabled()
    await userEvent.click(screen.getByText('Idea').closest('li') as HTMLElement)
    expect(screen.getByRole('button', { name: 'Move Idea down' })).toBeDisabled()
  })

  it('shows a blocked-by tag from blocked_on data', async () => {
    const list: NextList = {
      next_up: 0,
      items: [
        nextItem({ id: 1, title: 'Blocked item', rank: 1 }),
      ] as NextItem[],
    }
    ;(list.items[0] as { blocked_on?: unknown }).blocked_on = [{ id: 7, title: 'Earlier thing', state: 'approved' }]
    render(
      <LiveProvider live={live()}>
        <NextPage load={async () => list} api={{ add: vi.fn(), update: vi.fn() }} />
      </LiveProvider>,
    )
    const tag = await screen.findByText(/blocked by #7/)
    expect(tag).toHaveClass('sh-tag', 'sh-tag--blocked')
    expect(tag).not.toHaveClass('sh-tag--warn')
  })

  it('says so when nothing is planned', async () => {
    render(
      <LiveProvider live={live()}>
        <NextPage load={async () => ({ next_up: 0, items: [] })} api={{ add: vi.fn(), update: vi.fn() }} />
      </LiveProvider>,
    )
    expect(await screen.findByText('Nothing is planned')).toBeInTheDocument()
  })

  it('opens the detail panel when Enter is pressed on a focused row', async () => {
    render(
      <LiveProvider live={live()}>
        <NextPage load={async () => everything} api={{ add: vi.fn(), update: vi.fn() }} />
      </LiveProvider>,
    )
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    first.focus()
    await userEvent.keyboard('{enter}')
    expect(screen.getByRole('complementary', { name: /Details for First approved/ })).toBeInTheDocument()
  })

  it('drag reorder moves the dragged item toward the drop target', async () => {
    const api = { add: vi.fn(), update: vi.fn(async () => ({})) }
    render(
      <LiveProvider live={live()}>
        <NextPage load={async () => everything} api={api} />
      </LiveProvider>,
    )
    const first = (await screen.findByText('First approved')).closest('li') as HTMLElement
    const second = screen.getByText('Second approved').closest('li') as HTMLElement
    const store: Record<string, string> = {}
    const dataTransfer = {
      setData: (type: string, value: string) => { store[type] = value },
      getData: (type: string) => store[type] ?? '',
      effectAllowed: '',
      dropEffect: '',
    }
    fireEvent.dragStart(first, { dataTransfer })
    fireEvent.dragOver(second, { dataTransfer })
    fireEvent.drop(second, { dataTransfer })
    expect(api.update).toHaveBeenCalledWith(1, { move: 'down' })
  })

  it('drag reorder stops on the first failed move', async () => {
    const list: NextList = {
      next_up: 1,
      items: [
        nextItem({ id: 1, title: 'A', rank: 1 }),
        nextItem({ id: 2, title: 'B', rank: 2 }),
        nextItem({ id: 3, title: 'C', rank: 3 }),
      ] as NextItem[],
    }
    let calls = 0
    const api = {
      add: vi.fn(),
      update: vi.fn(async () => {
        calls++
        if (calls === 1) throw new ApiError(403, 'cannot move')
        return {}
      }),
    }
    render(
      <LiveProvider live={live()}>
        <NextPage load={async () => list} api={api} />
      </LiveProvider>,
    )
    const a = (await screen.findByText('A')).closest('li') as HTMLElement
    const c = screen.getByText('C').closest('li') as HTMLElement
    const store: Record<string, string> = {}
    const dataTransfer = {
      setData: (type: string, value: string) => { store[type] = value },
      getData: (type: string) => store[type] ?? '',
      effectAllowed: '',
      dropEffect: '',
    }
    fireEvent.dragStart(a, { dataTransfer })
    fireEvent.dragOver(c, { dataTransfer })
    fireEvent.drop(c, { dataTransfer })
    expect(api.update).toHaveBeenCalledTimes(1)
    expect(api.update).toHaveBeenCalledWith(1, { move: 'down' })
    expect(await screen.findByText('cannot move')).toBeInTheDocument()
  })
  describe('a row linked to a lane', () => {
    const linked: NextList = {
      next_up: 0,
      items: [
        nextItem({ id: 7, title: 'Linked', state: 'approved', stored_state: 'approved', lane_id: 4, lane: 'feat/export-csv', lane_name: 'feat/export-csv' }),
      ] as NextItem[],
    }
    const showLinked = async () => {
      window.location.hash = '#/next'
      render(
        <LiveProvider live={live()}>
          <NextPage load={async () => linked} api={{ add: vi.fn(), update: vi.fn() }} />
        </LiveProvider>,
      )
      return (await screen.findByText('Linked')).closest('li') as HTMLElement
    }

    it('opens the lane in the inspector on click, and still selects the row', async () => {
      await showLinked()
      await userEvent.click(screen.getByText('Linked'))
      expect(window.location.hash).toContain('open=lane%3A4')
      expect(screen.getByRole('complementary', { name: /Details for Linked/ })).toBeInTheDocument()
    })

    it('opens the lane on Enter', async () => {
      const row = await showLinked()
      row.focus()
      await userEvent.keyboard('{enter}')
      expect(window.location.hash).toContain('open=lane%3A4')
    })

    it('opens the lane from its own link without a second open', async () => {
      const row = await showLinked()
      await userEvent.click(within(row).getByRole('link', { name: /lane feat\/export-csv/ }))
      expect(window.location.hash).toContain('open=lane%3A4')
    })
  })
})
