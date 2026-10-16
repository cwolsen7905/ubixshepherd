import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { DeskQuestion } from '../api/types'
import type { DeskThread } from '../state/desk'
import { QuestionActions } from './Chat'

const q: DeskQuestion = { id: 3, workspace_id: 1, turn: 2, text: 'Ship it?', state: 'open', created: '2026-10-08T10:00:00Z' }

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
