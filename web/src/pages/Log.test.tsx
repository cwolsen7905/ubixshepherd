import { act, render, screen } from '@testing-library/react'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { decision, feedItem, lane, run, spend, status } from '../test/fixtures'
import { LogPage } from './Log'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

describe('the run page', () => {
  it('opens with a way back to the board', async () => {
    const api: Api = {
      status: async () => status(),
      lanes: async () => [lane()],
      runs: async () => [run()],
      run: async () => run(),
      runLog: async () => ({ offset: 0, data: '', size: 0, done: true }) as never,
      runEvents: async () => { throw new Error('unused') },
      decisions: async () => [decision()],
      requests: async () => [],
      spend: async () => spend(),
      feedLatest: async () => ({ items: [], events: [], last: 1 }),
      feed: async () => ({ items: [feedItem({ id: 1 })], events: [], last: 1 }),
      dismissRun: async () => { throw new Error('unused') },
      undismissRun: async () => { throw new Error('unused') },
      answer: async () => { throw new Error('unused') },
    }
    const live = new Live(api, still)
    await act(async () => {
      await live.refresh()
    })
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ offset: 0, data: '', size: 0, done: true }), { status: 200 }))
    render(
      <LiveProvider live={live}>
        <LogPage run={1} />
      </LiveProvider>,
    )
    expect(screen.getByRole('link', { name: '← Board' })).toHaveAttribute('href', '#/')
    vi.unstubAllGlobals()
  })
})
