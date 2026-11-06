import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { DeskQuestion } from '../api/types'
import type { DeskThread } from '../state/desk'
import { QuestionList } from '../chat/questions'
import { QuestionActions } from './Chat'

const q: DeskQuestion = { id: 3, workspace_id: 1, turn: 2, text: 'Ship it?', state: 'open', created: '2026-10-08T10:00:00Z' }
const qa: DeskQuestion = { id: 1, workspace_id: 1, turn: 1, text: 'Which branch?', state: 'open', created: '2026-10-08T10:00:00Z' }
const qb: DeskQuestion = { id: 2, workspace_id: 1, turn: 2, text: 'Ship it today?', state: 'open', created: '2026-10-08T10:05:00Z' }
const qc: DeskQuestion = { id: 4, workspace_id: 1, turn: 3, text: 'Tag the release?', state: 'open', created: '2026-10-08T10:10:00Z' }

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function thread(ok = true) {
  const send = vi.fn(async () => ok)
  return { send, t: { send } as unknown as DeskThread }
}

describe('QuestionActions', () => {
  it('offers Reply here, Skip and Mark answered', () => {
    render(<QuestionActions q={q} thread={thread().t} onCleared={() => {}} />)
    expect(screen.getByRole('button', { name: 'Reply here' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Skip' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Mark answered' })).toBeInTheDocument()
  })

  it('sends the reply linked to the question and leaves it open', async () => {
    const { send, t } = thread()
    const cleared = vi.fn()
    const replied = vi.fn()
    render(<QuestionActions q={q} thread={t} topicId={9} onCleared={cleared} onReplied={replied} />)
    await userEvent.click(screen.getByRole('button', { name: 'Reply here' }))
    await userEvent.type(screen.getByRole('textbox'), 'yes, ship')
    await userEvent.click(screen.getByRole('button', { name: 'Send reply' }))
    await waitFor(() => expect(replied).toHaveBeenCalled())
    expect(cleared).not.toHaveBeenCalled()
    expect(send).toHaveBeenCalledWith('yes, ship', 9, 3)
  })

  it('keeps the words and says so when the desk refuses the reply', async () => {
    const { t } = thread(false)
    const cleared = vi.fn()
    render(<QuestionActions q={q} thread={t} onCleared={cleared} />)
    await userEvent.click(screen.getByRole('button', { name: 'Reply here' }))
    await userEvent.type(screen.getByRole('textbox'), 'hello')
    await userEvent.click(screen.getByRole('button', { name: 'Send reply' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not sent')
    expect(screen.getByRole('textbox')).toHaveValue('hello')
    expect(cleared).not.toHaveBeenCalled()
  })

  it('skips through the daemon', async () => {
    const calls: string[] = []
    vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${path}`)
      if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }))
      return new Response(JSON.stringify({ ...q, state: 'skipped' }))
    })
    const cleared = vi.fn()
    render(<QuestionActions q={q} thread={thread().t} onCleared={cleared} />)
    await act(async () => {
      await userEvent.click(screen.getByRole('button', { name: 'Skip' }))
    })
    await waitFor(() => expect(cleared).toHaveBeenCalled())
    expect(calls).toContain('POST /v1/questions/3/skip')
  })

  it('marks it answered through the daemon', async () => {
    const calls: string[] = []
    vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${path}`)
      if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }))
      return new Response(JSON.stringify({ ...q, state: 'answered' }))
    })
    const cleared = vi.fn()
    render(<QuestionActions q={q} thread={thread().t} onCleared={cleared} />)
    await act(async () => {
      await userEvent.click(screen.getByRole('button', { name: 'Mark answered' }))
    })
    await waitFor(() => expect(cleared).toHaveBeenCalled())
    expect(calls).toContain('POST /v1/questions/3/answer')
  })
})

function stubClear(calls: string[], state: DeskQuestion['state']) {
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? 'GET'} ${path}`)
    if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }))
    return new Response(JSON.stringify({ state }))
  })
}

const row = (text: string) => screen.getByText(text).closest('li') as HTMLElement

describe('QuestionList', () => {
  it('lights the first open question, and a click lights the row', async () => {
    render(<QuestionList questions={[qa, qb, qc]} thread={thread().t} />)
    expect(screen.getByText('Which branch?').closest('.sh-row')).toHaveClass('is-selected')
    await userEvent.click(screen.getByText('Tag the release?'))
    expect(screen.getByText('Tag the release?').closest('.sh-row')).toHaveClass('is-selected')
    expect(screen.getByText('Which branch?').closest('.sh-row')).not.toHaveClass('is-selected')
  })

  it('drops the answered question at once and lights the next', async () => {
    const calls: string[] = []
    stubClear(calls, 'answered')
    const cleared = vi.fn()
    const selected = vi.fn()
    render(<QuestionList questions={[qa, qb, qc]} thread={thread().t} onCleared={cleared} onSelected={selected} />)
    await userEvent.click(within(row('Which branch?')).getByRole('button', { name: 'Mark answered' }))
    await waitFor(() => expect(screen.queryByText('Which branch?')).not.toBeInTheDocument())
    expect(screen.getByText('Ship it today?').closest('.sh-row')).toHaveClass('is-selected')
    expect(screen.getByText('Tag the release?')).toBeInTheDocument()
    expect(cleared).toHaveBeenCalledWith(1)
    expect(selected).toHaveBeenLastCalledWith(2)
    expect(calls).toContain('POST /v1/questions/1/answer')
  })

  it('keeps the lit question when another is answered', async () => {
    const calls: string[] = []
    stubClear(calls, 'answered')
    render(<QuestionList questions={[qa, qb, qc]} thread={thread().t} selected={2} />)
    expect(screen.getByText('Ship it today?').closest('.sh-row')).toHaveClass('is-selected')
    await userEvent.click(within(row('Tag the release?')).getByRole('button', { name: 'Mark answered' }))
    await waitFor(() => expect(screen.queryByText('Tag the release?')).not.toBeInTheDocument())
    expect(screen.getByText('Ship it today?').closest('.sh-row')).toHaveClass('is-selected')
    expect(calls).toContain('POST /v1/questions/4/answer')
  })

  it('drops the skipped question and lights the next', async () => {
    const calls: string[] = []
    stubClear(calls, 'skipped')
    render(<QuestionList questions={[qa, qb]} thread={thread().t} />)
    await userEvent.click(within(row('Which branch?')).getByRole('button', { name: 'Skip' }))
    await waitFor(() => expect(screen.queryByText('Which branch?')).not.toBeInTheDocument())
    expect(screen.getByText('Ship it today?').closest('.sh-row')).toHaveClass('is-selected')
    expect(calls).toContain('POST /v1/questions/1/skip')
  })

  it('the last one leaves the empty state', async () => {
    const calls: string[] = []
    stubClear(calls, 'answered')
    const selected = vi.fn()
    render(<QuestionList questions={[qa]} thread={thread().t} onSelected={selected} />)
    expect(screen.getByText('Which branch?')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Mark answered' }))
    await waitFor(() => expect(screen.getByText('No open questions.')).toBeInTheDocument())
    expect(screen.queryByText('Which branch?')).not.toBeInTheDocument()
    expect(selected).toHaveBeenLastCalledWith(null)
  })

  it('shows the empty state with no questions', () => {
    render(<QuestionList questions={[]} thread={thread().t} />)
    expect(screen.getByText('No open questions.')).toBeInTheDocument()
  })
})
