import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StoredArtifactView } from './StoredArtifact'

const ART = {
  id: 12, workspace_id: 1, repo_id: 1, lane_id: 4, run_id: 17, path: 'docs/plan.md', kind: 'markdown', source: 'declared',
  intent: 'keep', intent_by: 'agent', state: 'live', sha256: 'bbb', size: 30, created: '2026-10-15T10:00:00Z', updated: '2026-10-15T11:00:00Z',
  versions: [
    { artifact_id: 12, sha256: 'bbb', size: 30, run_id: 19, seen: '2026-10-15T11:00:00Z' },
    { artifact_id: 12, sha256: 'aaa', size: 20, run_id: 17, seen: '2026-10-15T10:00:00Z' },
  ],
}

function stub(calls: { path: string; method?: string }[]) {
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    calls.push({ path, method: init?.method })
    if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }))
    if (path === '/v1/artifacts/12') return new Response(JSON.stringify(ART))
    if (path.includes('version=aaa')) return new Response('# Plan\n\nold line\n\nkept')
    if (path.startsWith('/v1/artifacts/12/content')) return new Response('# Plan\n\nnew line\n\nkept')
    if (path === '/v1/artifacts/12/discard') return new Response(JSON.stringify({ ...ART, state: 'discarded' }))
    return new Response('{}', { status: 404 })
  })
}

afterEach(() => vi.unstubAllGlobals())

describe('StoredArtifactView', () => {
  it('shows the latest, lists versions with their runs, and compares an older one with the latest', async () => {
    const calls: { path: string }[] = []
    stub(calls)
    render(<StoredArtifactView id={12} />)
    expect(await screen.findByText('new line')).toBeInTheDocument()
    expect(screen.getByText(/run 17 ·/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'v1' }))
    expect(await screen.findByText('old line')).toBeInTheDocument()
    expect(calls.map((c) => c.path)).toContain('/v1/artifacts/12/content?version=aaa')
    await userEvent.click(screen.getByRole('button', { name: 'Compare with latest' }))
    const diff = await screen.findByLabelText('Changes from this version to the latest')
    expect(diff).toHaveTextContent('- old line')
    expect(diff).toHaveTextContent('+ new line')
  })

  it('discards only after the confirmation', async () => {
    const calls: { path: string }[] = []
    stub(calls)
    render(<StoredArtifactView id={12} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Discard…' }))
    expect(calls.some((c) => c.path.endsWith('/discard'))).toBe(false)
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }))
    expect(calls.some((c) => c.path === '/v1/artifacts/12/discard')).toBe(true)
  })

  it('keeps the way back and a retry when a page-layout artifact fails to load', async () => {
    stub([])
    render(<StoredArtifactView id={99} layout="page" backHref="#/artifacts" />)
    expect(await screen.findByText(/Could not read artifact 99/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /← Artifacts/ })).toHaveAttribute('href', '#/artifacts')
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })
})
