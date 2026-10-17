import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ApiError } from '../api/client'
import type { Decision, DecisionView } from '../api/types'
import * as client from '../api/client'
import { Live, type Api, type Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { DeskThread } from '../state/desk'
import { decision, deskEvent, FakeSource, fakeDeskApi, feedItem, lane, request, run, spend, status } from '../test/fixtures'
import type { DeskApi } from '../api/desk'
import { ChatPage, Markdown } from './Chat'
import { publishChat, useChatLink, type ChatLink } from '../shell/chatLink'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

function liveWith(
  feed = [feedItem({ id: 1, kind: 'decision', ref: 5, text: 'decision 5 asked', created: '2026-10-08T10:01:00Z' })],
  answer?: Api['answer'],
  lists: Partial<Api> = {},
) {
  const api: Api = {
    status: async () => status(),
    lanes: async () => [],
    runs: async () => [],
    run: async () => { throw new Error('unused') },
    runLog: async () => { throw new Error('unused') },
    runEvents: async () => { throw new Error('unused') },
    decisions: async (state) => (state === 'open' ? [decision()] : []),
    requests: async () => [request()],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: feed.length }),
    feed: async (after) => ({ items: after === 0 ? feed : [], events: feed.map((f) => f.kind === 'decision' ? 'decision_asked' : 'run_passed'), last: feed.length }),
    dismissRun: async () => { throw new Error('unused') },
    undismissRun: async () => { throw new Error('unused') },
    answer: answer ?? vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words })),
    ...lists,
  }
  return new Live(api, still)
}

async function show(api: DeskApi, live = liveWith()) {
  FakeSource.all = []
  await act(async () => {
    await live.refresh()
  })
  await act(async () => {
    live.start()
  })
  render(
    <LiveProvider live={live}>
      <ChatPage make={(id) => new DeskThread(id, { api, open: (u) => new FakeSource(u) })} />
    </LiveProvider>,
  )
  await waitFor(() => expect(FakeSource.all.length).toBeGreaterThan(0))
  const src = FakeSource.get(0)
  act(() => src.open())
  return { src, live }
}

const history = [
  deskEvent({ seq: 1, kind: 'user', text: 'what is up?', created: '2026-10-08T10:00:00Z' }),
  deskEvent({ seq: 2, kind: 'assistant', text: 'Two lanes **need** you.', created: '2026-10-08T10:00:30Z' }),
]

/** The connection the chat published for the rail, read the way the rail reads it. */
function currentLink(): ChatLink | null {
  let link: ChatLink | null = null
  function Probe() {
    link = useChatLink()
    return null
  }
  const { unmount } = render(<Probe />)
  unmount()
  return link
}

describe('ChatPage', () => {
  beforeEach(() => localStorage.clear())

  it('loads the thread from history and shows the connection', async () => {
    await show(fakeDeskApi({ history: async () => ({ events: history, more: false }) }))
    expect(await screen.findByText('what is up?')).toBeInTheDocument()
    expect(screen.getByText('need').tagName).toBe('STRONG')
    // a healthy stream says nothing in the chat: the rail's dot is the one place it shows
    expect(screen.queryByText('Live')).not.toBeInTheDocument()
    expect(document.querySelector('.chat-stream')).toBeEmptyDOMElement()
    expect(document.querySelector('.chat-stream')).toHaveAttribute('role', 'status')
    expect(screen.queryByText(/reconnecting/i)).not.toBeInTheDocument()
    expect(screen.getByRole('log')).toHaveAttribute('aria-live', 'polite')
  })

  it('publishes its connection for the rail while open, and clears it on leaving', async () => {
    await show(fakeDeskApi({ history: async () => ({ events: history, more: false }) }))
    await screen.findByText('what is up?')
    expect(currentLink()?.connection).toBe('live')
    cleanup()
    expect(currentLink()).toBeNull()
    publishChat(null)
  })

  it('keeps the link of a chat that is still open when another closes', () => {
    publishChat({ connection: 'live', retryAt: null }, 'a')
    publishChat({ connection: 'reconnecting', retryAt: null }, 'b')
    expect(currentLink()?.connection).toBe('reconnecting')
    publishChat(null, 'b')
    expect(currentLink()?.connection).toBe('live')
    publishChat(null, 'a')
    expect(currentLink()).toBeNull()
  })

  it('shows Reply here and Skip under the desk message that asked an open question, and nowhere else', async () => {
    const asked = { id: 7, workspace_id: 1, turn: 1, text: 'Ship it?', state: 'open', created: '2026-10-08T10:00:40Z' }
    vi.stubGlobal('fetch', async (path: string) => {
      if (path.startsWith('/v1/questions')) return new Response(JSON.stringify({ questions: [asked] }))
      return new Response('{}', { status: 404 })
    })
    try {
      const turned = history.map((e) => ({ ...e, turn: 1 }))
      await show(fakeDeskApi({ history: async () => ({ events: turned, more: false }) }))
      const reply = await screen.findByRole('button', { name: 'Reply here' })
      expect(reply.closest('.msg-assistant')).toHaveTextContent('Two lanes need you.')
      expect(screen.getAllByRole('button', { name: 'Skip' })).toHaveLength(1)
      expect(screen.getByText('what is up?').closest('.msg-user')?.querySelector('.question-actions')).toBeNull()
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('draws a partial reply as one growing message and a final one replaces it', async () => {
    const { src } = await show(fakeDeskApi())
    act(() => src.emit('partial', { kind: 'partial', text: 'Looking' }))
    act(() => src.emit('partial', { kind: 'partial', text: ' at the board' }))
    expect(screen.getByText('Looking at the board')).toBeInTheDocument()
    act(() => src.emit('assistant', deskEvent({ seq: 3, kind: 'assistant', text: 'All quiet.', created: '2026-10-08T10:04:00Z' })))
    expect(screen.queryByText('Looking at the board')).not.toBeInTheDocument()
    expect(screen.getByText('All quiet.')).toBeInTheDocument()
  })

  it('reopens a dropped stream after the last event it saw', async () => {
    const { src } = await show(fakeDeskApi({ history: async () => ({ events: history, more: false }) }))
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      act(() => src.emit('assistant', deskEvent({ seq: 8, kind: 'assistant', text: 'one more' })))
      act(() => src.fail())
      expect(await screen.findByText(/Chat reconnecting…, retrying in \ds/)).toBeInTheDocument()
      expect(currentLink()?.connection).toBe('reconnecting')
      expect(screen.getByRole('button', { name: 'Retry now' })).toBeInTheDocument()
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1100)
      })
      expect(FakeSource.all[1]?.url).toBe('/stream?ws=0&after=8')
      act(() => FakeSource.get(1).open())
      await waitFor(() => expect(document.querySelector('.chat-stream')).toBeEmptyDOMElement())
      expect(currentLink()?.connection).toBe('live')
    } finally {
      vi.useRealTimers()
    }
  })

  it('Enter sends, Shift+Enter adds a line, and the box empties on success', async () => {
    const user = userEvent.setup()
    const turn = vi.fn(async () => ({ workspace_id: 1, turn: 9, ahead: 1 }))
    await show(fakeDeskApi({ turn }))
    const box = screen.getByLabelText('Message to the front desk')
    await user.type(box, 'line one{Shift>}{Enter}{/Shift}line two')
    expect(box).toHaveValue('line one\nline two')
    await user.keyboard('{Enter}')
    expect(turn).toHaveBeenCalledWith(0, 'line one\nline two')
    await waitFor(() => expect(box).toHaveValue(''))
    expect(await screen.findByRole('button', { name: 'Interrupt' })).toBeInTheDocument()
  })

  it('disables the composer while a send is pending', async () => {
    const user = userEvent.setup()
    let release: () => void = () => {}
    const turn = vi.fn(() => new Promise<{ workspace_id: number; turn: number; ahead: number }>((r) => {
      release = () => r({ workspace_id: 1, turn: 2, ahead: 1 })
    }))
    await show(fakeDeskApi({ turn }))
    await user.type(screen.getByLabelText('Message to the front desk'), 'hi{Enter}')
    expect(await screen.findByRole('button', { name: 'Sending…' })).toBeDisabled()
    await user.keyboard('{Enter}')
    expect(turn).toHaveBeenCalledTimes(1)
    await act(async () => release())
    expect(await screen.findByRole('button', { name: 'Send' })).toBeDisabled()
  })

  it('says the queue is full and keeps the words', async () => {
    const user = userEvent.setup()
    const turn = vi.fn().mockRejectedValue(new ApiError(429, 'the desk has 8 turns waiting'))
    await show(fakeDeskApi({ turn }))
    const box = screen.getByLabelText('Message to the front desk')
    await user.type(box, 'hello{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent(/as many messages waiting/)
    expect(box).toHaveValue('hello')
  })

  it('keeps a half-typed message across leaving the page and coming back', async () => {
    const user = userEvent.setup()
    await show(fakeDeskApi())
    await user.type(screen.getByLabelText('Message to the front desk'), 'half a thou')
    cleanup()
    await show(fakeDeskApi())
    expect(screen.getByLabelText('Message to the front desk')).toHaveValue('half a thou')
  })

  it('clears the draft once a send succeeds, so it does not come back', async () => {
    const user = userEvent.setup()
    const turn = vi.fn(async () => ({ workspace_id: 1, turn: 9, ahead: 1 }))
    await show(fakeDeskApi({ turn }))
    const box = screen.getByLabelText('Message to the front desk')
    await user.type(box, 'ship it{Enter}')
    await waitFor(() => expect(box).toHaveValue(''))
    cleanup()
    await show(fakeDeskApi({ turn }))
    expect(screen.getByLabelText('Message to the front desk')).toHaveValue('')
  })

  it('keeps the draft when a send fails', async () => {
    const user = userEvent.setup()
    const turn = vi.fn().mockRejectedValue(new ApiError(429, 'the desk has 8 turns waiting'))
    await show(fakeDeskApi({ turn }))
    await user.type(screen.getByLabelText('Message to the front desk'), 'hello{Enter}')
    await screen.findByRole('alert')
    cleanup()
    await show(fakeDeskApi({ turn }))
    expect(screen.getByLabelText('Message to the front desk')).toHaveValue('hello')
  })

  it('keeps a half-written decision answer, and clears it once answered', async () => {
    const user = userEvent.setup()
    const answer = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words }))
    const api = () => fakeDeskApi({ history: async () => ({ events: history, more: false }) })
    const label = `Or write your own answer to decision ${decision().id}`
    await show(api(), liveWith(undefined, answer))
    await user.type(await screen.findByLabelText(label), 'let me thi')
    cleanup()
    await show(api(), liveWith(undefined, answer))
    const box = await screen.findByLabelText(label)
    expect(box).toHaveValue('let me thi')
    await user.click(screen.getByRole('button', { name: 'Answer' }))
    expect(await screen.findByText(/^Answered “let me thi”/)).toBeInTheDocument()
    expect(localStorage.getItem(`shepherd.chat.answer.${decision().id}`)).toBeNull()
  })

  it('tells a signed-out browser how to sign in', async () => {
    const user = userEvent.setup()
    const turn = vi.fn().mockRejectedValue(new ApiError(401, 'missing, wrong or revoked token'))
    await show(fakeDeskApi({ turn }))
    await user.type(screen.getByLabelText('Message to the front desk'), 'hello{Enter}')
    await waitFor(() => expect(screen.getAllByRole('alert')[0]).toHaveTextContent('shepherd web'))
    expect(screen.getByText('Signed out')).toBeInTheDocument()
  })

  it('shows a 403 with the daemon words', async () => {
    const user = userEvent.setup()
    const turn = vi.fn().mockRejectedValue(new ApiError(403, 'cross-site request refused'))
    await show(fakeDeskApi({ turn }))
    await user.type(screen.getByLabelText('Message to the front desk'), 'hello{Enter}')
    expect(await screen.findByText(/cross-site request refused/)).toBeInTheDocument()
  })

  it('interrupts a running turn', async () => {
    const user = userEvent.setup()
    const interrupt = vi.fn(async () => ({ interrupted: true }))
    const { src } = await show(fakeDeskApi({ interrupt }))
    expect(screen.queryByRole('button', { name: 'Interrupt' })).not.toBeInTheDocument()
    act(() => src.emit('turn_start', deskEvent({ seq: 3, kind: 'turn_start', text: '' })))
    await user.click(await screen.findByRole('button', { name: 'Interrupt' }))
    expect(interrupt).toHaveBeenCalledWith(0)
    expect(await screen.findByText('Interrupted.')).toBeInTheDocument()
  })

  it('asks before starting a new conversation, and cancel does nothing', async () => {
    const user = userEvent.setup()
    const newConversation = vi.fn(async () => ({}))
    await show(fakeDeskApi({ newConversation }))
    await user.click(screen.getByRole('button', { name: 'New conversation' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(newConversation).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'New conversation' }))
    await user.click(screen.getByRole('button', { name: 'Start new' }))
    expect(newConversation).toHaveBeenCalledWith(0)
    expect(screen.queryByRole('button', { name: 'Start new' })).not.toBeInTheDocument()
  })

  it('draws a new-conversation row and a run row between messages', async () => {
    const { src } = await show(fakeDeskApi({ history: async () => ({ events: history, more: false }) }))
    act(() => src.emit('new', deskEvent({ seq: 9, kind: 'new', text: '', created: '2026-10-08T10:04:00Z' })))
    expect(await screen.findByText('New conversation', { selector: 'span' })).toBeInTheDocument()
  })

  it('answers a decision in place, with the same call as the Decisions page', async () => {
    const user = userEvent.setup()
    const answer = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words }))
    await show(fakeDeskApi({ history: async () => ({ events: history, more: false }) }), liveWith(undefined, answer))
    const card = (await screen.findByText(decision().question)).closest('section') as HTMLElement
    await user.click(within(card).getByRole('button', { name: /^No/ }))
    expect(answer).not.toHaveBeenCalled()
    await user.click(within(card).getByRole('button', { name: 'Answer' }))
    expect(answer).toHaveBeenCalledWith(decision().id, 'No')
    expect(await screen.findByText(/^Answered “No”/)).toBeInTheDocument()
  })

  it('renders hostile text inert', async () => {
    const evil = [
      '<script>window.pwned = 1</script><img src=x onerror="window.pwned=1">',
      '[click](javascript:alert(1)) and [ok](https://example.com/a?b=c) and [data](data:text/html,x)',
    ].join('\n\n')
    await show(fakeDeskApi({ history: async () => ({ events: [deskEvent({ seq: 1, kind: 'assistant', text: evil }), deskEvent({ seq: 2, kind: 'user', text: '<b onclick="x()">hi</b>' })], more: false }) }))
    expect(await screen.findByText(/<script>window.pwned = 1<\/script>/)).toBeInTheDocument()
    expect(document.querySelector('script')).toBeNull()
    expect(document.querySelector('img')).toBeNull()
    expect(document.querySelector('[onclick], [onerror]')).toBeNull()
    expect(screen.getByText('<b onclick="x()">hi</b>')).toBeInTheDocument()
    const links = Array.from(document.querySelectorAll('.md a'))
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['https://example.com/a?b=c'])
    expect(links[0]).toHaveAttribute('rel', 'noopener noreferrer')
    expect(document.body.innerHTML).not.toMatch(/href="javascript:/i)
  })
})

describe('following the bottom', () => {
  // The thread is its own scroll region: its scrollTop, scrollHeight and clientHeight.
  let height = 2000
  let y = 1300
  const scrollTo = vi.fn()
  const observers: (() => void)[] = []
  const proto = HTMLElement.prototype as unknown as Record<string, unknown>
  const saved = { scrollTo: proto.scrollTo }
  const log = () => screen.getByRole('log')

  beforeEach(() => {
    height = 2000
    y = 1300 // 1300 + 700 is the bottom
    scrollTo.mockClear()
    observers.length = 0
    proto.scrollTo = scrollTo
    Object.defineProperty(HTMLElement.prototype, 'scrollHeight', { configurable: true, get: () => height })
    Object.defineProperty(HTMLElement.prototype, 'clientHeight', { configurable: true, get: () => 700 })
    Object.defineProperty(HTMLElement.prototype, 'scrollTop', { configurable: true, get: () => y, set: () => {} })
    vi.stubGlobal('ResizeObserver', class {
      constructor(cb: () => void) {
        observers.push(cb)
      }
      observe() {}
      disconnect() {}
    })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    proto.scrollTo = saved.scrollTo
    delete proto.scrollHeight
    delete proto.clientHeight
    delete proto.scrollTop
  })

  const grow = (to: number) => {
    height = to
    act(() => observers.forEach((cb) => cb()))
  }
  const scrollTop = (to: number) => {
    y = to
    act(() => {
      log().dispatchEvent(new Event('scroll'))
    })
  }
  const last = () => scrollTo.mock.calls.at(-1)?.[0] as { top: number } | undefined

  it('lands at the bottom when the thread first loads', async () => {
    await show(fakeDeskApi({ history: async () => ({ events: history, more: false }) }))
    await screen.findByText('what is up?')
    expect(last()).toEqual({ top: 2000 })
  })

  it('follows content that grows without a new item, such as a streaming reply', async () => {
    const { src } = await show(fakeDeskApi())
    scrollTo.mockClear()
    act(() => src.emit('partial', { kind: 'partial', text: 'a' }))
    grow(2400)
    expect(last()).toEqual({ top: 2400 })
  })

  it('stays put once the reader scrolls up, and shows a way back', async () => {
    const { src } = await show(fakeDeskApi())
    scrollTop(600)
    scrollTo.mockClear()
    act(() => src.emit('partial', { kind: 'partial', text: 'more' }))
    grow(2600)
    expect(scrollTo).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Jump to latest' })).toBeInTheDocument()
  })

  it('stops following when the wheel goes up during a reply, before any scroll event', async () => {
    const { src } = await show(fakeDeskApi())
    act(() => src.emit('partial', { kind: 'partial', text: 'a' }))
    act(() => {
      log().dispatchEvent(new WheelEvent('wheel', { deltaY: -40 }))
    })
    scrollTo.mockClear()
    grow(2500)
    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('follows again when the reader scrolls back to the bottom', async () => {
    await show(fakeDeskApi())
    scrollTop(600)
    grow(2600)
    scrollTo.mockClear()
    scrollTop(1900) // 1900 + 700 = 2600
    grow(2800)
    expect(last()).toEqual({ top: 2800 })
    expect(screen.queryByRole('button', { name: 'Jump to latest' })).not.toBeInTheDocument()
  })

  it('does not take its own scrolling, or growth under it, for the reader leaving', async () => {
    await show(fakeDeskApi())
    grow(2500) // we scroll to the bottom, which the browser reports as scrollY 1800...
    scrollTo.mockClear()
    height = 3000 // ...but more arrives before that scroll event is handled
    scrollTop(1800)
    grow(3000)
    expect(last()).toEqual({ top: 3000 })
  })

  it('Jump to latest returns to the bottom and resumes following', async () => {
    const user = userEvent.setup()
    await show(fakeDeskApi())
    scrollTop(500)
    scrollTo.mockClear()
    await user.click(screen.getByRole('button', { name: 'Jump to latest' }))
    expect(last()).toEqual({ top: 2000 })
    grow(2200)
    expect(last()).toEqual({ top: 2200 })
  })
})

describe('backlog rail', () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => vi.restoreAllMocks())

  const quiet = [feedItem({ id: 1, kind: 'run_passed', ref: 10, text: 'run 10 passed in feat/login', created: '2026-10-08T10:04:00Z' })]
  const lists = { lanes: async () => [lane({ id: 1 })], runs: async () => [run({ id: 10, lane_id: 1, ended: '2026-10-08T10:04:00Z' })] }
  const withHistory = fakeDeskApi({ history: async () => ({ events: history, more: false }) })

  it('lists what waits, most urgent first, with counts and links to its page', async () => {
    await show(withHistory, liveWith(quiet, undefined, { lanes: async () => [lane({ id: 1 }), lane({ id: 2, name: 'docs/readme' })], runs: async () => [run({ id: 11, lane_id: 2, ended: '2026-10-08T10:04:00Z' })], decisions: async (st) => (st === 'open' ? [decision({ id: 3 }) as DecisionView] : []) }))
    await screen.findByText('what is up?')
    const rail = await screen.findByRole('complementary', { name: 'Backlog' })
    expect(within(rail).getByRole('button', { name: 'Backlog 2 needs you · 1 idle' })).toBeInTheDocument()
    const rows = within(rail).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('decision 3')
    expect(rows[rows.length - 1]).toHaveTextContent('last run 11 succeeded')
    expect(within(rows[rows.length - 1] as HTMLElement).getByRole('link', { name: 'docs/readme' })).toHaveAttribute('href', '#/?open=lane%3A2')
  })

  it('jumps to the thread row where an idle lane went quiet', async () => {
    const user = userEvent.setup()
    const scrollIntoView = vi.fn()
    Element.prototype.scrollIntoView = scrollIntoView
    await show(withHistory, liveWith(quiet, undefined, { ...lists, decisions: async () => [], requests: async () => [] }))
    await screen.findByText('what is up?')
    const rail = await screen.findByRole('complementary', { name: 'Backlog' })
    await user.click(await within(rail).findByRole('button', { name: 'in thread' }))
    expect(scrollIntoView).toHaveBeenCalledTimes(1)
    const row = scrollIntoView.mock.contexts[0] as HTMLElement
    expect(row.id).toBe('thread-f1')
    expect(row).toHaveClass('is-flash')
  })

  it('remembers it was closed', async () => {
    const user = userEvent.setup()
    await show(withHistory, liveWith(quiet, undefined, lists))
    await screen.findByText('what is up?')
    const toggle = await screen.findByRole('button', { name: /Backlog/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(localStorage.getItem('shepherd.chat.backlog')).toBe('closed')
  })

  it('says when nothing waits, and a quota hold, and no spend meter (it lives in the rail)', async () => {
    vi.spyOn(client.daemonInfo, 'status').mockResolvedValue(
      status({ detail: { os: 'linux', arch: 'amd64', now: '', config_loaded: '', running: 0, open_lanes: 0, decisions: 0, spend_usd: 0, quota_holds: [{ agent: 'claude', until: '2026-10-08T14:05:00Z' }], forge: { last_poll: '2026-10-08T10:03:00Z' } } }),
    )
    await show(withHistory, liveWith([], undefined, { decisions: async () => [], requests: async () => [] }))
    await screen.findByText('what is up?')
    const rail = await screen.findByRole('complementary', { name: 'Backlog' })
    expect(within(rail).getByText('nothing waiting')).toBeInTheDocument()
    expect(within(rail).getByText(/Nothing is waiting on you/)).toBeInTheDocument()
    expect(within(rail).queryByRole('meter')).not.toBeInTheDocument()
    expect(await within(rail).findByText(/claude is out of quota until/)).toBeInTheDocument()
    expect(within(rail).getByText(/Forge polled .+ ago/)).toBeInTheDocument()
  })

  it('says when the forge poll is failing, so merge badges are known to be stale', async () => {
    vi.spyOn(client.daemonInfo, 'status').mockResolvedValue(
      status({ detail: { os: 'linux', arch: 'amd64', now: '', config_loaded: '', running: 0, open_lanes: 0, decisions: 0, spend_usd: 0, forge: { error: 'glab: 401' } } }),
    )
    await show(withHistory, liveWith(quiet, undefined, lists))
    await screen.findByText('what is up?')
    const rail = await screen.findByRole('complementary', { name: 'Backlog' })
    expect(await within(rail).findByText(/Forge poll failing: glab: 401/)).toBeInTheDocument()
  })
})

describe('Markdown', () => {
  it('renders lists and fenced code as plain text inside elements', () => {
    const { container } = render(<Markdown text={'- a\n- `b`\n\n```\n<i>raw</i>\n```'} />)
    expect(container.querySelectorAll('li')).toHaveLength(2)
    expect(container.querySelector('pre code')?.textContent).toBe('<i>raw</i>')
    expect(container.querySelector('i')).toBeNull()
  })

  it('renders tables', () => {
    const { container } = render(<Markdown text={'| A | B |\n|---|---|\n| 1 | 2 |'} />)
    expect(container.querySelectorAll('.md-table th')).toHaveLength(2)
    expect(container.querySelector('.md-table th:nth-child(1)')?.textContent).toBe('A')
    expect(container.querySelector('.md-table tbody tr')?.querySelectorAll('td')).toHaveLength(2)
    expect(container.querySelector('.md-table tbody td:nth-child(1)')?.textContent).toBe('1')
  })

  it('renders blockquotes', () => {
    const { container } = render(<Markdown text={'> this is a quote\n> with two lines'} />)
    const bq = container.querySelector('.md-quote')
    expect(bq).toBeTruthy()
    expect(bq?.textContent).toBe('this is a quote\nwith two lines')
  })

  it('renders horizontal rules', () => {
    const { container } = render(<Markdown text={'before\n\n---\n\nafter'} />)
    const hrs = container.querySelectorAll('.md-hr')
    expect(hrs).toHaveLength(1)
    expect(hrs.item(0)?.tagName).toBe('HR')
  })

  it('renders task lists', () => {
    const { container } = render(<Markdown text={'- [ ] pending\n- [x] done\n- [ ] also pending'} />)
    const checkboxs = Array.from(container.querySelectorAll('.md-task input[type="checkbox"]'))
    expect(checkboxs).toHaveLength(3)
    expect(checkboxs.at(0)?.hasAttribute('checked')).toBe(false)
    expect(checkboxs.at(1)?.hasAttribute('checked')).toBe(true)
    expect(checkboxs.at(2)?.hasAttribute('checked')).toBe(false)
  })

  it('renders all new block types together', () => {
    const text = [
      'Before paragraph.',
      '',
      '> A blockquote with **bold** text.',
      '',
      '---',
      '',
      '| Col1 | Col2 |',
      '|------|------|',
      '| a    | b    |',
      '',
      '- [ ] task one',
      '- [x] task two',
      '',
      'After paragraph.',
    ].join('\n')
    const { container } = render(<Markdown text={text} />)
    expect(container.querySelector('.md-quote')).toBeTruthy()
    expect(container.querySelector('.md-hr')).toBeTruthy()
    expect(container.querySelector('.md-table')).toBeTruthy()
    expect(container.querySelectorAll('.md-task input[type="checkbox"]')).toHaveLength(2)
    // "Before paragraph.", "After paragraph." and the blockquote <p> = 3 paragraphs
    expect(container.querySelectorAll('p')).toHaveLength(3)
  })
})

describe('topic bar and rail', () => {
  beforeEach(() => {
    localStorage.clear()
    window.location.hash = '#/chat'
  })
  afterEach(() => {
    vi.restoreAllMocks()
    window.location.hash = ''
  })

  it('share one filter: a chip sets it for the rail, the rail for the chip, and back clears it', async () => {
    const user = userEvent.setup()
    const topic = (id: number, title: string) => ({
      id, title, status: 'open' as const, source: 'desk', created: '', updated: '', activity: new Date().toISOString(), waits: 0, links: [],
    })
    vi.spyOn(client.topics, 'list').mockResolvedValue({ topics: [topic(1, 'release'), topic(2, 'docs')] })
    await show(fakeDeskApi({ history: async () => ({ events: history, more: false }) }))
    const bar = await screen.findByRole('group', { name: 'Filter the thread by topic' })
    await user.click(within(bar).getByRole('button', { name: 'All topics' }))
    const rail = within(bar).getByRole('region', { name: 'Topics' })

    await user.click(await within(bar).findByRole('button', { name: 'release' }))
    expect(window.location.hash).toBe('#/chat?topic=1')
    await waitFor(() => expect(within(rail).getByRole('button', { name: /release/ })).toHaveAttribute('aria-pressed', 'true'))
    expect(within(bar).getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'false')

    await user.click(within(rail).getByRole('button', { name: /docs/ }))
    expect(window.location.hash).toBe('#/chat?topic=2')
    expect(within(bar).getByRole('button', { name: 'docs' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(bar).getByRole('button', { name: 'release' })).toHaveAttribute('aria-pressed', 'false')

    await user.click(within(bar).getByRole('button', { name: 'All' }))
    expect(window.location.hash).toBe('#/chat')
    expect(within(rail).getByRole('button', { name: /docs/ })).toHaveAttribute('aria-pressed', 'false')

    act(() => {
      window.location.hash = '#/chat?topic=1'
    })
    await waitFor(() => expect(within(bar).getByRole('button', { name: 'release' })).toHaveAttribute('aria-pressed', 'true'))
  })
})

describe('topic pill', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.location.hash = ''
  })

  it('names the topic on a tied turn, not on an untied one, and a click filters the thread', async () => {
    window.location.hash = '#/chat'
    const user = userEvent.setup()
    vi.spyOn(client.topics, 'list').mockResolvedValue({
      topics: [{
        id: 3, title: 'the release of v0.3', status: 'open', source: 'desk', created: '', updated: '', activity: new Date().toISOString(),
        waits: 0, links: [{ kind: 'turn', ref: '1', by: 'desk', time: '' }],
      }],
    })
    const events = [
      deskEvent({ seq: 1, kind: 'user', text: 'tied question', turn: 1 }),
      deskEvent({ seq: 2, kind: 'assistant', text: 'tied answer', turn: 1 }),
      deskEvent({ seq: 3, kind: 'user', text: 'loose question', turn: 3 }),
      deskEvent({ seq: 4, kind: 'assistant', text: 'loose answer', turn: 3 }),
    ]
    await show(fakeDeskApi({ history: async () => ({ events, more: false }) }))
    const log = screen.getByRole('log')
    const pills = await within(log).findAllByRole('button', { name: 'Topic: the release of v0.3' })
    expect(pills).toHaveLength(2)
    expect(pills[0]).toHaveAttribute('title', 'the release of v0.3')

    await user.click(pills[1] as HTMLElement)
    expect(window.location.hash).toBe('#/chat?topic=3')
    await waitFor(() => expect(screen.queryByText('loose answer')).not.toBeInTheDocument())
    expect(screen.getByText('tied answer')).toBeInTheDocument()
  })

  it('sends the topic being viewed with the message, and none otherwise', async () => {
    window.location.hash = '#/chat?topic=3'
    const user = userEvent.setup()
    vi.spyOn(client.topics, 'list').mockResolvedValue({
      topics: [{
        id: 3, title: 'the release of v0.3', status: 'open', source: 'desk', created: '', updated: '', activity: new Date().toISOString(),
        waits: 0, links: [],
      }],
    })
    const turn = vi.fn(async () => ({ workspace_id: 1, turn: 9, ahead: 1 }))
    await show(fakeDeskApi({ turn }))
    await user.type(screen.getByLabelText('Message to the front desk'), 'from here{Enter}')
    await waitFor(() => expect(turn).toHaveBeenCalledWith(0, 'from here', 3))
  })
})
