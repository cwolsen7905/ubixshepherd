import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LaneArtifactsPanel } from './LaneArtifacts'

const LIST = {
  lane_id: 4, lane: 'feat/x', globs: ['**/*.md'],
  artifacts: [
    { path: 'docs/plan.md', kind: 'markdown', source: 'untracked', status: 'untracked', size: 40, readable: true },
    { path: 'notes.txt', kind: 'text', source: 'committed', status: 'added', size: 9, readable: true },
    { path: 'shot.png', kind: 'image', source: 'untracked', status: 'untracked', size: 2048, readable: true },
    { path: 'big.log', kind: 'text', source: 'untracked', status: 'untracked', size: 9999999, readable: false },
  ],
}

function stub(calls: string[], list: unknown = LIST) {
  vi.stubGlobal('fetch', async (path: string) => {
    calls.push(path)
    if (path === '/v1/lanes/4/artifacts') return new Response(JSON.stringify(list))
    if (path.includes('plan.md')) return new Response('# Plan\n\n<script>alert(1)</script>\n', { headers: { 'X-Shepherd-Redacted': '2' } })
    if (path.includes('notes.txt')) return new Response('plain **words**')
    return new Response(JSON.stringify({ error: 'gone' }), { status: 404 })
  })
}

afterEach(() => vi.unstubAllGlobals())

describe('LaneArtifactsPanel', () => {
  it('lists artifacts and renders markdown without raw HTML', async () => {
    const calls: string[] = []
    stub(calls)
    const { container } = render(<LaneArtifactsPanel laneId={4} />)
    await userEvent.click(await screen.findByRole('button', { name: 'docs/plan.md' }))
    expect(await screen.findByText('Plan')).toBeInTheDocument()
    expect(calls).toContain('/v1/lanes/4/artifacts/content?path=docs%2Fplan.md')
    expect(container.querySelector('script')).toBeNull()
    expect(container.textContent).toContain('<script>alert(1)</script>')
    expect(screen.getByText(/2 secret-like spans were masked/)).toBeInTheDocument()
  })

  it('shows plain text verbatim', async () => {
    stub([])
    render(<LaneArtifactsPanel laneId={4} />)
    await userEvent.click(await screen.findByRole('button', { name: 'notes.txt' }))
    expect(await screen.findByText('plain **words**')).toBeInTheDocument()
  })

  it('shows an image by its content URL and does not fetch unreadable files', async () => {
    const calls: string[] = []
    stub(calls)
    render(<LaneArtifactsPanel laneId={4} />)
    await userEvent.click(await screen.findByRole('button', { name: 'shot.png' }))
    expect(screen.getByRole('img')).toHaveAttribute('src', '/v1/lanes/4/artifacts/content?path=shot.png')
    await userEvent.click(screen.getByRole('button', { name: 'big.log' }))
    expect(screen.getByText(/Not readable here/)).toBeInTheDocument()
    expect(calls.some((c) => c.includes('big.log'))).toBe(false)
  })

  it('says so when there are none, and when the read fails', async () => {
    stub([], { ...LIST, artifacts: null })
    const { unmount } = render(<LaneArtifactsPanel laneId={4} />)
    expect(await screen.findByText(/No artifacts in this lane yet/)).toBeInTheDocument()
    unmount()
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ error: 'lane is closed' }), { status: 409 }))
    render(<LaneArtifactsPanel laneId={4} />)
    expect(await screen.findByText(/lane is closed/)).toBeInTheDocument()
  })
})
