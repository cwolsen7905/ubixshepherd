import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { topics } from '../api/client'
import type { TopicView } from '../model/topic'
import { LiveProvider } from '../state/context'
import { DeskThread } from '../state/desk'
import { isAboutKind, withReference, topicFor, type About } from '../state/deskAbout'
import type { Api, Clock } from '../state/live'
import { Live } from '../state/live'
import { fakeDeskApi, spend, status } from '../test/fixtures'
import { ContextComposer } from './ContextComposer'

vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  topics: { list: vi.fn() },
}))

const still: Clock = { now: () => 0, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }
const topic = (o: Partial<TopicView>): TopicView => ({
  id: 1, title: 't', status: 'open', source: 'desk', created: '', updated: '', activity: '', waits: 0, links: [], ...o,
})
const lane: About = { kind: 'lane', ids: [4], label: 'feat/export-csv' }

function shown(turn = vi.fn().mockResolvedValue({ workspace_id: 1, turn: 9, ahead: 0 }), about = lane) {
  const api = {
    status: async () => status(), lanes: async () => [], runs: async () => [], decisions: async () => [], requests: async () => [], spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after: number) => ({ items: [], events: [], last: after }),
  } as unknown as Api
  const make = (w: number) => new DeskThread(w, { api: fakeDeskApi({ turn }) })
  render(
    <LiveProvider live={new Live(api, still)}>
      <ContextComposer about={about} make={make} />
    </LiveProvider>,
  )
  return turn
}

describe('ContextComposer', () => {
  beforeEach(() => vi.mocked(topics.list).mockReset().mockResolvedValue({ topics: [topic({ id: 7, links: [{ kind: 'lane', ref: '4', by: 'desk', time: '' }] })] }))
  it('says what it is about and sends a turn with a reference line, under the entity topic', async () => {
    const user = userEvent.setup()
    const turn = shown()
    expect(screen.getByText('Message the front desk about feat/export-csv (lane 4)')).toBeInTheDocument()
    await screen.findByRole('textbox')
    await user.type(screen.getByRole('textbox'), 'Why is this blocked?{Enter}')
    expect(turn).toHaveBeenCalledWith(0, 'About feat/export-csv (lane 4):\n\nWhy is this blocked?', 7)
    expect(await screen.findByText(/Sent to the front desk/)).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveValue('')
  })
  it('keeps the words and says so when the desk refuses', async () => {
    const user = userEvent.setup()
    shown(vi.fn().mockRejectedValue(new Error('down')))
    await user.type(screen.getByRole('textbox'), 'hello')
    await user.click(screen.getByRole('button', { name: 'Send' }))
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveValue('hello')
  })
  it('sends unfiled, and says so, when the topics cannot be read', async () => {
    const user = userEvent.setup()
    vi.mocked(topics.list).mockResolvedValue({} as never) // a reply without a list is as unreadable as a refusal
    const turn = shown()
    await user.type(screen.getByRole('textbox'), 'hi{Enter}')
    expect(turn).toHaveBeenCalledWith(0, 'About feat/export-csv (lane 4):\n\nhi')
    expect(await screen.findByText(/not filed under one/)).toBeInTheDocument()
  })
  it('does not send blank words', () => {
    shown()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
  })
})

describe('deskAbout', () => {
  it('writes the reference line', () => {
    expect(withReference({ kind: 'decision', ids: [5] }, ' go ')).toBe('About decision 5:\n\ngo')
  })
  it('knows only its own kinds', () => {
    expect(isAboutKind('lane')).toBe(true)
    expect(isAboutKind('constructor')).toBe(false)
    expect(isAboutKind('__proto__')).toBe(false)
  })
  it('picks the open topic that links the entity', () => {
    const l = { by: 'desk' as const, time: '' }
    const ts = [
      topic({ id: 1, status: 'cleared', links: [{ ...l, kind: 'lane', ref: '4' }] }),
      topic({ id: 2, links: [{ ...l, kind: 'decision', ref: '4' }] }),
      topic({ id: 3, links: [{ ...l, kind: 'lane', ref: '4' }] }),
    ]
    expect(topicFor({ kind: 'lane', ids: [4] }, ts)).toBe(3)
    expect(topicFor({ kind: 'run', ids: [4] }, ts)).toBeUndefined()
    expect(topicFor({ kind: 'artifact', ids: [4] }, ts)).toBeUndefined()
    expect(topicFor({ kind: 'topic', ids: [2] }, ts)).toBe(2)
    expect(topicFor({ kind: 'topic', ids: [1] }, ts)).toBeUndefined()
  })
})
