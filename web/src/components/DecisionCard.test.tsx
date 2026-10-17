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
    expect(screen.getByRole('heading', { level: 2 })).toBeInTheDocument()
    expect(screen.getByText('web/src/components/DecisionCard.tsx').tagName).toBe('CODE')
    expect(screen.getByText('Chat.tsx').tagName).toBe('STRONG')
    expect(document.querySelectorAll('.decision-md ol li')).toHaveLength(2)
    expect(document.querySelector('script')).toBeNull()
  })

  it('marks the recommendation and sets the reason apart', () => {
    show(decision({ recommendation: 'Yes', why: 'It is `security` work' }))
    const list = document.querySelector<HTMLElement>('.options') as HTMLElement
    expect(within(list).getByText('recommended')).toBeInTheDocument()
    const why = screen.getByLabelText('Why this is your call')
    expect(within(why).getByText('security').tagName).toBe('CODE')
  })

  it('shows a recommendation that is not an option as its own block', () => {
    show(decision({ recommendation: 'Do **both**, Yes first' }))
    const rec = screen.getByLabelText('Recommendation')
    expect(within(rec).getByText('both').tagName).toBe('STRONG')
    expect(screen.queryByText('recommended')).toBeNull()
  })

  it('clamps a long question until "Show all"', async () => {
    const user = userEvent.setup()
    show(decision({ question: 'Summary\n' + Array.from({ length: 30 }, (_, i) => `- step ${i}`).join('\n') }))
    const body = document.querySelector('.decision-md') as HTMLElement
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
    expect(screen.queryByRole('heading')).toBeNull()
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
    const body = document.querySelector('.decision-md') as HTMLElement
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

describe('the inline decision card', () => {
  it('has no textarea and no Answer button', () => {
    show(decision(), 'inline')
    expect(screen.queryByRole('textbox')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Answer' })).toBeNull()
  })

  it('shows numbered chips and clicking one picks it', async () => {
    const user = userEvent.setup()
    show(decision({ options: ['Stream', 'Buffer'], recommendation: 'Buffer' }), 'inline')
    const buttons = screen.getAllByRole('button', { name: /Stream|Buffer/ })
    const [stream, buffer] = buttons
    if (!stream || !buffer) throw new Error('expected both chips')
    expect(stream).toHaveAttribute('aria-pressed', 'false')
    expect(buffer).toHaveAttribute('aria-pressed', 'false')
    await user.click(stream)
    expect(stream).toHaveAttribute('aria-pressed', 'true')
    expect(buffer).toHaveAttribute('aria-pressed', 'false')
  })

  it('is the headline and the options, with the long body left for the inspector', () => {
    show(decision({ question: PLAN, why: 'Because it is yours.', options: ['Stream', 'Buffer'] }), 'inline')
    expect(screen.getByText('Add the decision card to the chat thread')).toBeInTheDocument()
    expect(screen.queryByText('Steps')).toBeNull()
    expect(screen.queryByText('Why this is yours')).toBeNull()
    expect(screen.getByRole('button', { name: /Stream/ })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'open' }).getAttribute('href')).toContain('open=decision')
  })

  it('links the first artifact when the decision has artifacts', () => {
    const d = { ...decision(), artifacts: [{ id: 7, path: 'docs/plan.md', title: 'plan' }] } as unknown as DecisionView
    show(d, 'inline')
    const link = screen.getByRole('link', { name: '1 artifact' })
    expect(link.getAttribute('href')).toContain('open=artifact')
  })
})

describe('the decision in the inspector', () => {
  it('the pane variant has no glyph rail, shows the whole long body and marks the recommended option', () => {
    const long = ['Plan headline', ...Array(30).fill('a line of the plan')].join('\n')
    show(decision({ question: long, options: ['Stream', 'Buffer'], recommendation: 'Buffer' }), 'pane')
    const card = screen.getByRole('region')
    expect(card).toHaveClass('decision-pane')
    expect(card.querySelector('.glyph')).toBeNull()
    expect(card.querySelector('.is-clamped')).toBeNull()
    expect(screen.queryByRole('button', { name: /Show all/ })).toBeNull()
    expect(within(screen.getByRole('button', { name: /Buffer/ })).getByText('recommended')).toBeInTheDocument()
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
    expect(screen.getByText('Why this is yours:')).toBeInTheDocument()
    expect(screen.getByText('Steps')).toBeInTheDocument()
    const send = screen.getByRole('button', { name: 'Send answer' })
    expect(send).toHaveClass('ui-btn-attention')
    expect(screen.getByText(/⌘↵|Ctrl\+Enter/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Buffer/ }))
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
    await user.click(screen.getByRole('button', { name: /Buffer/ }))
    await user.click(screen.getByLabelText('composer'))
    await user.keyboard('{Meta>}{Enter}{/Meta}')
    expect(answer).not.toHaveBeenCalled()
  })
})
