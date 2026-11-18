import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { DeskQuestion } from '../api/types'
import type { DeskThread } from '../state/desk'
import { QuestionActions } from './questions'

const q: DeskQuestion = { id: 5, workspace_id: 1, turn: 2, text: 'Which repo?', state: 'open', created: '2026-10-08T10:00:00Z' }

afterEach(cleanup)

describe('QuestionActions after a reply', () => {
  it('names the question with a chip beside the waiting flag', () => {
    render(<QuestionActions q={q} thread={{ send: async () => true } as unknown as DeskThread} onCleared={() => {}} />)
    expect(screen.getByText('question 5')).toHaveAttribute('data-component', 'IdChip')
  })
  it('keeps Reply here and Skip and does not clear it, so a follow-up can be sent', async () => {
    const send = vi.fn(async () => true)
    const cleared = vi.fn()
    render(<QuestionActions q={q} thread={{ send } as unknown as DeskThread} onCleared={cleared} />)
    await userEvent.click(screen.getByRole('button', { name: 'Reply here' }))
    await userEvent.type(screen.getByRole('textbox'), 'what do you mean?')
    await userEvent.click(screen.getByRole('button', { name: 'Send reply' }))
    await waitFor(() => expect(send).toHaveBeenCalledWith('what do you mean?', null, 5))
    expect(cleared).not.toHaveBeenCalled()
    expect(await screen.findByRole('button', { name: 'Reply here' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Skip' })).toBeInTheDocument()
  })
})

describe('Yes and No buttons', () => {
  const yn: DeskQuestion = { ...q, id: 7, text: 'Should I merge it?' }
  it('send the word as a reply linked to the question and clear it', async () => {
    const send = vi.fn(async () => true)
    const replied = vi.fn()
    render(<QuestionActions q={yn} thread={{ send } as unknown as DeskThread} topicId={3} onCleared={() => {}} onReplied={replied} />)
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() => expect(send).toHaveBeenCalledWith('Yes', 3, 7))
    expect(replied).toHaveBeenCalled()
  })
  it('send No the same way', async () => {
    const send = vi.fn(async () => true)
    render(<QuestionActions q={yn} thread={{ send } as unknown as DeskThread} onCleared={() => {}} />)
    await userEvent.click(screen.getByRole('button', { name: 'No' }))
    await waitFor(() => expect(send).toHaveBeenCalledWith('No', null, 7))
  })
  it('say so when the send fails, and keep the question', async () => {
    const replied = vi.fn()
    render(<QuestionActions q={yn} thread={{ send: async () => false } as unknown as DeskThread} onCleared={() => {}} onReplied={replied} />)
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not sent')
    expect(replied).not.toHaveBeenCalled()
  })
  it('are absent on an open question', () => {
    render(<QuestionActions q={q} thread={{ send: async () => true } as unknown as DeskThread} onCleared={() => {}} />)
    expect(screen.queryByRole('button', { name: 'Yes' })).toBeNull()
  })
})
