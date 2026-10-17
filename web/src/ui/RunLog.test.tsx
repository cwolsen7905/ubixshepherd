import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RunLog } from './RunLog'

const LOG = '# shepherd run 7: claude\nI will read it.\n→ Bash {"command":"ls"}\n→ Read {"file_path":"a.go"}\nDone.\n'

beforeEach(() => {
  vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ offset: LOG.length, data: LOG, size: LOG.length, done: true })))
})
afterEach(() => vi.unstubAllGlobals())

describe('RunLog', () => {
  it('finished: shows the text, folds tool calls and searches', async () => {
    render(<RunLog id={7} state="succeeded" variant="finished" />)
    expect(await screen.findByText('I will read it.')).toBeInTheDocument()
    expect(screen.getByText('2 tool calls')).toBeInTheDocument()
    expect(screen.getByText('End of log.')).toBeInTheDocument()
    await userEvent.type(screen.getByRole('searchbox'), 'done')
    expect(screen.getByText('1 of 1')).toBeInTheDocument()
    expect(document.querySelector('[data-component="RunLog"]')).toHaveAttribute('data-variant', 'finished')
  })

  it('live: offers to follow while the run goes', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ offset: 5, data: 'hello', size: 5, done: false })))
    render(<RunLog id={7} state="running" variant="live" />)
    await screen.findByText('hello')
    expect(screen.getByRole('button', { name: /Following/ })).toBeEnabled()
    expect(screen.getByText('running')).toBeInTheDocument()
  })

  it('finished: the follow button is off and says why', async () => {
    render(<RunLog id={7} state="succeeded" />)
    await screen.findByText('End of log.')
    expect(screen.getByRole('button', { name: /Follow/ })).toBeDisabled()
  })

  it('transcript: no search or follow controls', async () => {
    render(<RunLog id={7} state="succeeded" variant="transcript" />)
    await screen.findByText('I will read it.')
    expect(screen.queryByRole('toolbar')).toBeNull()
  })

  it('says when it cannot read the log, and retries', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ error: 'gone' }), { status: 500 }))
    render(<RunLog id={7} state="failed" />)
    await waitFor(() => expect(screen.getByText(/Could not read the log: gone/)).toBeInTheDocument())
  })
})
