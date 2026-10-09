import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Decision } from '../api/types'
import { Live, type Api, type Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { decision, request, spend, status } from '../test/fixtures'
import { DecisionsPage } from './Decisions'

// Timers that never fire: the page is tested on one load of the lists.
const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

function setup(answer: Api['answer'] = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words, answer_run: 11 }))) {
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
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after) => ({ items: [], events: [], last: after }),
    answer,
  }
  const live = new Live(api, still)
  return { live, answer }
}

/** An option's button, found inside the options list so Send's label cannot match. */
function option(name: string) {
  const list = document.querySelector<HTMLElement>('.options')
  if (!list) throw new Error('no options list')
  return within(list).getByRole('button', { name: new RegExp(`^\\d?${name}`) })
}

async function show(live: Live) {
  await act(() => live.refresh())
  render(
    <LiveProvider live={live}>
      <DecisionsPage />
    </LiveProvider>,
  )
}

describe('DecisionsPage', () => {
  it('shows the question, its options and the recommendation, with nothing sent', async () => {
    const { live, answer } = setup()
    await show(live)
    const d = decision()
    expect(screen.getByRole('heading', { name: d.question })).toBeInTheDocument()
    for (const o of d.options ?? []) expect(option(o)).toBeInTheDocument()
    expect(screen.getByText('recommended')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send answer' })).toBeDisabled()
    expect(answer).not.toHaveBeenCalled()
  })

  it('picking an option, by click or number key, does not answer; only Send does', async () => {
    const user = userEvent.setup()
    const { live, answer } = setup()
    await show(live)
    const [first = '', second = ''] = decision().options ?? []
    await user.click(option(first))
    await user.keyboard('2')
    expect(option(second)).toHaveAttribute('aria-pressed', 'true')
    expect(option(first)).toHaveAttribute('aria-pressed', 'false')
    expect(answer).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: /^Send/ }))
    expect(answer).toHaveBeenCalledTimes(1)
    expect(answer).toHaveBeenCalledWith(decision().id, second)
    expect(await screen.findByRole('status')).toHaveTextContent('run 11')
  })

  it("sends the person's own words over a picked option", async () => {
    const user = userEvent.setup()
    const { live, answer } = setup()
    await show(live)
    await user.click(option((decision().options ?? [])[0] ?? ''))
    await user.type(screen.getByPlaceholderText('Or write your own answer'), 'Neither: ask the API owner first')
    await user.click(screen.getByRole('button', { name: /^Send/ }))
    expect(answer).toHaveBeenCalledWith(decision().id, 'Neither: ask the API owner first')
  })

  it('says why an answer was not sent and lets the person try again', async () => {
    const user = userEvent.setup()
    const { live } = setup(vi.fn(async () => {
      throw new Error('decision 5 is already answered')
    }))
    await show(live)
    await user.click(option((decision().options ?? [])[0] ?? ''))
    await user.click(screen.getByRole('button', { name: /^Send/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not sent: decision 5 is already answered')
    expect(screen.getByRole('button', { name: /^Send/ })).toBeEnabled()
  })

  it('lists requests that need routing, read-only', async () => {
    const { live } = setup()
    await show(live)
    expect(screen.getByRole('heading', { name: 'Requests no rule could route' })).toBeInTheDocument()
    expect(screen.getByText(request().message)).toBeInTheDocument()
  })
})
