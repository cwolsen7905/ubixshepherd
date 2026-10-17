import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { OfflineContext } from './offline'
import { ArtifactView } from './ArtifactView'
import { DecisionCard } from './DecisionCard'
import { DecisionOptions } from './DecisionOptions'
import { DecisionView } from './DecisionView'

describe('DecisionOptions', () => {
  it('draws numbered chips, marks the recommended one and picks on click', async () => {
    const onPick = vi.fn()
    render(<DecisionOptions label="Options" options={['Stream', 'Buffer']} recommended="Stream" picked="Buffer" onPick={onPick} />)
    const group = screen.getByRole('radiogroup', { name: 'Options' })
    expect(group).toHaveAttribute('data-variant', 'chips')
    expect(within(group).getByRole('radio', { name: /Buffer/ })).toHaveAttribute('aria-checked', 'true')
    expect(within(group).getByRole('radio', { name: /Stream/ })).toHaveClass('is-recommended')
    await userEvent.click(within(group).getByRole('radio', { name: /Stream/ }))
    expect(onPick).toHaveBeenCalledWith('Stream')
  })

  it('draws rows with the word "recommended"', () => {
    render(<DecisionOptions label="Options" variant="rows" options={['Stream', 'Buffer']} recommended="Stream" />)
    expect(screen.getByRole('radiogroup')).toHaveAttribute('data-variant', 'rows')
    expect(screen.getByText('recommended')).toBeInTheDocument()
  })
})

describe('DecisionCard', () => {
  it('opens on a click away from a control, and sends only by its own button', async () => {
    const onOpen = vi.fn()
    const onSend = vi.fn()
    const { rerender } = render(
      <DecisionCard label="Decision 5" meta="Decision 5" question="Stream or buffer?" options={['Stream', 'Buffer']} onOpen={onOpen} onSend={onSend} />,
    )
    await userEvent.click(screen.getByRole('heading', { name: 'Stream or buffer?' }))
    expect(onOpen).toHaveBeenCalledTimes(1)
    await userEvent.click(screen.getByRole('radio', { name: /Stream/ }))
    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: /Send/ })).toBeNull()
    rerender(<DecisionCard label="Decision 5" meta="" question="Stream or buffer?" options={['Stream', 'Buffer']} picked="Stream" onSend={onSend} selected />)
    expect(screen.getByRole('region', { name: 'Decision 5' })).toHaveClass('is-selected')
    await userEvent.click(screen.getByRole('button', { name: /Send: Stream/ }))
    expect(onSend).toHaveBeenCalled()
  })

  it('makes the mobile card tall, with an own answer and a full-width send', () => {
    render(<DecisionCard mobile label="Decision 5" meta="" question="q" options={['A']} picked="A" own="" onOwn={() => {}} onSend={() => {}} />)
    expect(screen.getByRole('radio', { name: /A/ })).toHaveClass('sh-tap')
    expect(screen.getByRole('button', { name: 'Send answer: A' })).toHaveClass('sh-card-send')
    expect(screen.getByPlaceholderText('Or write your own answer')).toBeInTheDocument()
  })

  it('lets the mobile card send a typed answer with no option picked', async () => {
    const onSend = vi.fn()
    const { rerender } = render(<DecisionCard mobile label="Decision 5" meta="" question="q" options={['A']} own="" onOwn={() => {}} onSend={onSend} />)
    expect(screen.getByRole('button', { name: /Send answer/ })).toBeDisabled()
    rerender(<DecisionCard mobile label="Decision 5" meta="" question="q" options={['A']} own="my words" sendLabel="“my words”" onOwn={() => {}} onSend={onSend} />)
    await userEvent.click(screen.getByRole('button', { name: 'Send answer: “my words”' }))
    expect(onSend).toHaveBeenCalled()
  })

  it('turns Send off with the reason while offline', () => {
    render(
      <OfflineContext.Provider value={{ offline: true, retryIn: 4 }}>
        <DecisionCard label="Decision 5" meta="" question="q" options={['A']} picked="A" onSend={() => {}} />
      </OfflineContext.Provider>,
    )
    const send = screen.getByRole('button', { name: /Send: A/ })
    expect(send).toBeDisabled()
    expect(send).toHaveAttribute('title', expect.stringContaining('Offline'))
  })
})

const view = (over = {}) => (
  <DecisionView
    label="Decision 5" kind="Decision 5, claude" question="Stream or buffer?" why="It is your call." context={<p>Context.</p>}
    options={['Stream', 'Buffer']} recommended="Stream" own="" onOwn={() => {}} onSend={() => {}} canSend={false}
    plan={{ path: 'docs/plans/x.md', tag: 'kept local', tagTone: 'local', meta: 'run 17 · 4.1 KB', children: <ArtifactView layout="inline"><p>The plan.</p></ArtifactView> }}
    {...over}
  />
)

describe('DecisionView', () => {
  it('shows the question, why it is yours, the options as rows and the plan inline', async () => {
    render(view())
    expect(screen.getByRole('heading', { level: 1, name: 'Stream or buffer?' })).toBeInTheDocument()
    expect(screen.getByText('Why this is yours.')).toBeInTheDocument()
    expect(screen.getByRole('radiogroup')).toHaveAttribute('data-variant', 'rows')
    expect(screen.getByText('The plan.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /docs\/plans\/x\.md/ }))
    expect(screen.queryByText('The plan.')).toBeNull()
  })

  it('turns Send answer off with the reason while offline, keeping what was typed', () => {
    render(
      <OfflineContext.Provider value={{ offline: true, retryIn: 3 }}>
        {view({ canSend: true, own: 'my words' })}
      </OfflineContext.Provider>,
    )
    const send = screen.getByRole('button', { name: 'Send answer' })
    expect(send).toBeDisabled()
    expect(send).toHaveAttribute('title', expect.stringContaining('Offline'))
    expect(screen.getByLabelText(/Your own answer/)).toHaveValue('my words')
  })

  it('has Send answer off until there is an answer, and Ask the desk first', () => {
    const { rerender } = render(view({ onAsk: () => {} }))
    expect(screen.getByRole('button', { name: 'Send answer' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Ask the desk first' })).toBeInTheDocument()
    rerender(view({ canSend: true, onAsk: () => {} }))
    expect(screen.getByRole('button', { name: 'Send answer' })).toBeEnabled()
  })

  it('has the mobile layout with tall controls', () => {
    render(view({ layout: 'mobile' }))
    expect(screen.getByRole('article')).toHaveAttribute('data-layout', 'mobile')
    expect(screen.getByRole('radio', { name: /Stream/ })).toHaveClass('sh-tap')
  })

  it('replaces the answer controls with the outcome once answered', () => {
    render(view({ done: <p role="status">Answered.</p> }))
    expect(screen.getByRole('status')).toHaveTextContent('Answered.')
    expect(screen.queryByRole('button', { name: 'Send answer' })).toBeNull()
  })
})

describe('ArtifactView', () => {
  it('draws the page with its bar, and the inline layout as content alone', async () => {
    const onBack = vi.fn()
    const { rerender } = render(
      <ArtifactView title="Plan" path="docs/plan.md" tag="kept local" tagTone="local" meta="run 17" actions={<button>Keep</button>} onBack={onBack}>
        <p>Body</p>
      </ArtifactView>,
    )
    expect(screen.getByRole('heading', { name: 'Plan' })).toBeInTheDocument()
    expect(screen.getByText('docs/plan.md')).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'What to do with this file' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '← Artifacts' }))
    expect(onBack).toHaveBeenCalled()
    rerender(
      <ArtifactView layout="inline" title="Plan">
        <p>Body</p>
      </ArtifactView>,
    )
    expect(screen.queryByRole('heading')).toBeNull()
    expect(screen.getByText('Body')).toBeInTheDocument()
  })
})
