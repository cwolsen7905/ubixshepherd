import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Decision, DecisionView } from '../api/types'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { decision, spend, status } from '../test/fixtures'
import { DecisionCard, isLong, splitQuestion } from './DecisionCard'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

function live(answer: Api['answer'] = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words }))) {
  const api: Api = {
    status: async () => status(), lanes: async () => [], runs: async () => [],
    run: async () => { throw new Error('unused') }, runLog: async () => { throw new Error('unused') }, runEvents: async () => { throw new Error('unused') },
    decisions: async () => [], requests: async () => [], spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after) => ({ items: [], events: [], last: after }),
    dismissRun: async () => { throw new Error('unused') }, undismissRun: async () => { throw new Error('unused') },
    answer,
  }
  return new Live(api, still)
}

const PLAN = [
  'Add the decision card to the chat thread',
  '',
  '## Steps',
  '1. Create `web/src/components/DecisionCard.tsx`',
  '2. Use it in **Chat.tsx**',
  '',
  '```',
  'make check',
  '```',
].join('\n')

function show(d = decision(), variant: 'page' | 'inline' | 'pane' = 'page') {
  window.localStorage.clear()
  render(
    <LiveProvider live={live()}>
      <DecisionCard d={d} variant={variant} />
    </LiveProvider>,
  )
}

describe('inline card at 390px', () => {
  it('sends a typed own answer with no option picked, and the typed words win over a pick', async () => {
    const answer = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words }))
    window.localStorage.clear()
    render(
      <LiveProvider live={live(answer)}>
        <DecisionCard d={decision({ options: ['Stream', 'Buffer'] })} variant="inline" mobile />
      </LiveProvider>,
    )
    const user = userEvent.setup()
    expect(screen.getByRole('button', { name: /Send answer/ })).toBeDisabled()
    await user.click(screen.getByRole('radio', { name: /Stream/ }))
    await user.type(screen.getByLabelText(/Your own answer/), 'Neither, stream in chunks')
    await user.click(screen.getByRole('button', { name: /Send answer/ }))
    expect(answer).toHaveBeenCalledWith(decision().id, 'Neither, stream in chunks')
  })
})

describe('splitQuestion', () => {
  it('takes the first line as the summary', () => {
    expect(splitQuestion(PLAN)).toMatchObject({ head: 'Add the decision card to the chat thread' })
    expect(splitQuestion('One line only')).toEqual({ head: 'One line only', body: '' })
    expect(splitQuestion('# Title\nbody').head).toBe('Title')
  })
  it('has no summary when the first line is a list item, a fence or a wall of words', () => {
    expect(splitQuestion('1. first\n2. second').head).toBe('')
    expect(splitQuestion('```\ncode\n```').head).toBe('')
    expect(splitQuestion('x'.repeat(300) + '\nmore').head).toBe('')
  })
  it('knows a long body', () => {
    expect(isLong('short')).toBe(false)
    expect(isLong('x'.repeat(800))).toBe(true)
    expect(isLong(Array(20).fill('line').join('\n'))).toBe(true)
  })
})

describe('DecisionCard', () => {
  it('renders the question as markdown under its summary, inert', () => {
    show(decision({ question: PLAN + '\n\n<script>window.x=1</script>' }))
    expect(screen.getByRole('heading', { name: 'Add the decision card to the chat thread' })).toBeInTheDocument()
    expect(screen.getByText('decision 5')).toHaveAttribute('data-component', 'IdChip')
    expect(screen.getByText('Steps')).toBeInTheDocument()
    expect(screen.getByText('web/src/components/DecisionCard.tsx').tagName).toBe('CODE')
    expect(screen.getByText('Chat.tsx').tagName).toBe('STRONG')
    expect(document.querySelectorAll('.sh-decision-md ol li')).toHaveLength(2)
    expect(document.querySelector('script')).toBeNull()
  })

  it('marks the recommendation and sets the reason apart', () => {
    show(decision({ recommendation: 'Yes', why: 'It is `security` work' }))
    const list = screen.getByRole('radiogroup', { name: /Options for/ })
    expect(within(list).getByText('recommended')).toBeInTheDocument()
    const why = document.querySelector<HTMLElement>('.sh-decision-why') as HTMLElement
    expect(why).toHaveTextContent('Why this is yours.')
    expect(within(why).getByText('security').tagName).toBe('CODE')
  })

  it('shows a recommendation that is not an option as its own block', () => {
    show(decision({ recommendation: 'Do **both**, Yes first' }))
    const rec = document.querySelector<HTMLElement>('.sh-decision-rec') as HTMLElement
    expect(within(rec).getByText('both').tagName).toBe('STRONG')
    expect(screen.queryByText('recommended')).toBeNull()
  })

  it('clamps a long question until "Show all"', async () => {
    const user = userEvent.setup()
    show(decision({ question: 'Summary\n' + Array.from({ length: 30 }, (_, i) => `- step ${i}`).join('\n') }))
    const body = document.querySelector('.sh-decision-md') as HTMLElement
    expect(body).toHaveClass('is-clamped')
    const more = screen.getByRole('button', { name: /^Show all/ })
    expect(more).toHaveAttribute('aria-expanded', 'false')
    await user.click(more)
    expect(body).not.toHaveClass('is-clamped')
    expect(screen.getByRole('button', { name: 'Show less' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('does not clamp a short one', () => {
    show()
    expect(screen.queryByRole('button', { name: /Show all/ })).toBeNull()
  })

  it('has an accessible name when there is no summary line, and a section when inline', () => {
    show(decision({ question: '1. do this\n2. do that' }), 'inline')
    expect(screen.getByRole('heading', { name: '1. do this 2. do that' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: `Decision ${decision().id}` }).tagName).toBe('SECTION')
  })

  it('renders tables, blockquotes, rules and task lists in a long body', () => {
    const longBody = [
      'Here is the plan:',
      '',
      '| Step | Action |',
      '|------|--------|',
      '| 1    | Build  |',
      '| 2    | Test   |',
      '',
      '> This is the key design note.',
      '',
      '---',
      '',
      '- [ ] Step one',
      '- [x] Step two',
      '',
      'Done.',
    ].join('\n')
    show(decision({ question: 'Summary\n' + longBody }))
    const body = document.querySelector('.sh-decision-md') as HTMLElement
    expect(body).toHaveClass('is-clamped')
    expect(screen.getByRole('heading', { name: 'Summary' })).toBeInTheDocument()
    // All elements are in the DOM (clamped via CSS), but the "Show all" button is present
    expect(document.querySelector('.cv-table')).toBeTruthy()
    expect(document.querySelector('.cv-quote')).toBeTruthy()
    expect(document.querySelector('.cv-hr')).toBeTruthy()
    const checkboxs = document.querySelectorAll('.cv-task input[type="checkbox"]')
    expect(checkboxs).toHaveLength(2)
    const more = screen.getByRole('button', { name: /Show all/ })
    expect(more).toBeInTheDocument()
  })
})

describe('the answer draft', () => {
  it('survives a remount in the pane, is not cleared by an inline chip, and is cleared once sent', async () => {
    const user = userEvent.setup()
    const d = decision({ id: 11, options: ['Stream', 'Buffer'] })
    const ui = (variant: 'pane' | 'inline') => (
      <LiveProvider live={live()}>
        <DecisionCard d={d} variant={variant} />
      </LiveProvider>
    )
    window.localStorage.clear()
    const first = render(ui('pane'))
    await user.type(screen.getByRole('textbox'), 'half written')
    first.unmount()
    const inl = render(ui('inline'))
    await user.click(screen.getByRole('radio', { name: /Buffer/ }))
    inl.unmount()
    render(ui('pane'))
    expect(screen.getByRole('textbox')).toHaveValue('half written')
    await user.click(screen.getByRole('button', { name: 'Send answer' }))
    await screen.findByRole('status')
    expect(window.localStorage.getItem('shepherd.chat.answer.11')).toBeNull()
  })
})

describe('the inline decision card', () => {
  it('has no textarea and no Answer button', () => {
    show(decision(), 'inline')
    expect(screen.queryByRole('textbox')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Answer' })).toBeNull()
  })

  it('shows numbered chips and clicking one reveals a send button', async () => {
    const user = userEvent.setup()
    const answer = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words }))
    window.localStorage.clear()
    render(
      <LiveProvider live={live(answer)}>
        <DecisionCard d={decision({ id: 9, options: ['Stream', 'Buffer'], recommendation: 'Buffer' })} variant="inline" focused />
      </LiveProvider>,
    )
    const buttons = screen.getAllByRole('radio', { name: /Stream|Buffer/ })
    const [stream, buffer] = buttons
    if (!stream || !buffer) throw new Error('expected both chips')
    expect(stream).toHaveAttribute('aria-checked', 'false')
    expect(buffer).toHaveAttribute('aria-checked', 'false')
    expect(screen.queryByRole('button', { name: /Send/ })).toBeNull()
    await user.click(stream)
    expect(stream).toHaveAttribute('aria-checked', 'true')
    expect(buffer).toHaveAttribute('aria-checked', 'false')

    const send = screen.getByRole('button', { name: /Send/ })
    await user.click(send)
    expect(answer).toHaveBeenCalledWith(9, 'Stream')
  })

  it('does not send a chip choice with Cmd+Enter in the inline card; the button does', async () => {
    const user = userEvent.setup()
    const answer = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words }))
    window.localStorage.clear()
    render(
      <LiveProvider live={live(answer)}>
        <DecisionCard d={decision({ id: 10, options: ['Stream', 'Buffer'] })} variant="inline" focused />
      </LiveProvider>,
    )
    await user.click(screen.getByRole('radio', { name: /Stream/ }))
    await user.keyboard('{Meta>}{Enter}{/Meta}')
    expect(answer).not.toHaveBeenCalled()
  })

  it('is the headline and the options, with the long body left for the inspector', () => {
    show(decision({ question: PLAN, why: 'Because it is yours.', options: ['Stream', 'Buffer'] }), 'inline')
    expect(screen.getByText('Add the decision card to the chat thread')).toBeInTheDocument()
    expect(screen.queryByText('Steps')).toBeNull()
    expect(screen.queryByText('Why this is yours')).toBeNull()
    expect(screen.getByRole('radio', { name: /Stream/ })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open the full decision' }).getAttribute('href')).toContain('open=decision')
  })

  it('links every artifact, one link each', () => {
    const d = { ...decision(), artifacts: [{ id: 7, path: 'a.md', title: 'a' }, { id: 8, path: 'b.md', title: 'b' }] } as DecisionView
    show(d, 'inline')
    expect(screen.getByRole('link', { name: 'a.md' }).getAttribute('href')).toContain('open=artifact')
    expect(screen.getByRole('link', { name: 'b.md' }).getAttribute('href')).toContain('open=artifact')
  })

  it('says answering happens in the inspector when an inline decision has no options', () => {
    show(decision({ options: [] }), 'inline')
    expect(screen.getByText('Open this decision to write your answer.')).toBeTruthy()
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('links the artifact when the decision has artifacts', () => {
    const d = { ...decision(), artifacts: [{ id: 7, path: 'docs/plan.md', title: 'plan' }] } as DecisionView
    show(d, 'inline')
    const link = screen.getByRole('link', { name: 'docs/plan.md' })
    expect(link.getAttribute('href')).toContain('open=artifact')
  })
})

describe('the decision in the inspector', () => {
  it('the pane variant has no glyph rail, shows the whole long body and marks the recommended option', () => {
    const long = ['Plan headline', ...Array(30).fill('a line of the plan')].join('\n')
    show(decision({ question: long, options: ['Stream', 'Buffer'], recommendation: 'Buffer' }), 'pane')
    const card = screen.getByRole('article', { name: 'Decision 5' })
    expect(card).toHaveClass('sh-decision')
    expect(card.querySelector('.glyph')).toBeNull()
    expect(card.querySelector('.is-clamped')).toBeNull()
    expect(screen.queryByRole('button', { name: /Show all/ })).toBeNull()
    expect(within(screen.getByRole('radio', { name: /Buffer/ })).getByText('recommended')).toBeInTheDocument()
  })

  it('shows the body and the why, and sends with Cmd+Enter', async () => {
    const user = userEvent.setup()
    const answer = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words }))
    const onSent = vi.fn()
    window.localStorage.clear()
    render(
      <LiveProvider live={live(answer)}>
        <DecisionCard d={decision({ question: PLAN, why: 'Because it is yours.', options: ['Stream', 'Buffer'] })} variant="pane" focused onSent={onSent} />
      </LiveProvider>,
    )
    expect(screen.getByText('Why this is yours.')).toBeInTheDocument()
    expect(screen.getByText('Steps')).toBeInTheDocument()
    const send = screen.getByRole('button', { name: 'Send answer' })
    expect(send).toHaveClass('ui-btn-attention')
    expect(screen.getByText(/⌘↵|Ctrl\+Enter/)).toBeInTheDocument()
    await user.click(screen.getByRole('radio', { name: /Buffer/ }))
    await user.keyboard('{Meta>}{Enter}{/Meta}')
    expect(answer).toHaveBeenCalledWith(expect.any(Number), 'Buffer')
    expect(await screen.findByText(/Answered/)).toBeInTheDocument()
    expect(onSent).toHaveBeenCalledTimes(1)
  })

  it('leaves Cmd+Enter in a box outside the card alone', async () => {
    const user = userEvent.setup()
    const answer = vi.fn(async (id: number, words: string): Promise<Decision> => ({ ...decision({ id }), state: 'answered', answer: words }))
    window.localStorage.clear()
    render(
      <LiveProvider live={live(answer)}>
        <textarea aria-label="composer" />
        <DecisionCard d={decision({ options: ['Stream', 'Buffer'] })} variant="pane" focused />
      </LiveProvider>,
    )
    await user.click(screen.getByRole('radio', { name: /Buffer/ }))
    await user.click(screen.getByLabelText('composer'))
    await user.keyboard('{Meta>}{Enter}{/Meta}')
    expect(answer).not.toHaveBeenCalled()
  })
})
