import { render, screen } from '@testing-library/react'
import { run } from '../test/fixtures'
import { RunChanges } from './RunChanges'

const NOW = Date.parse('2026-10-08T10:05:00Z')

describe('RunChanges', () => {
  it('shows the lane diff narrowed to the run', async () => {
    const r = run({ outside: ['docs/x.md'] })
    const f = (p: string) => ({ path: p, status: 'modified', added: 1, removed: 0, in_scope: true })
    const diff = {
      lane_id: 1, lane: 'feat/login', branch: 'feat/login', base: 'dev', base_ref: 'origin/dev', head: 'def456', mode: 'stat',
      read_at: '2026-10-08T10:04:00Z', commits_total: 1, out_of_scope: 0, commits: [{ sha: 'abc1234', subject: 'run commit', author: 'a', date: '', files: 1 }],
      committed: [f('docs/x.md')], uncommitted: [f('src/wip.ts')], untracked: [],
    }
    const urls: string[] = []
    vi.stubGlobal('fetch', async (u: string) => {
      urls.push(u)
      const body = u.includes('/diff')
        ? diff
        : u.includes('/v1/runs?')
          ? [r]
          : {}
      return new Response(JSON.stringify(body), { status: 200 })
    })
    render(<RunChanges run={r} now={NOW} />)
    expect(await screen.findByText('run commit')).toBeInTheDocument()
    expect(urls).toContain('/v1/lanes/1/diff?mode=stat&run=10')
    expect(screen.getByRole('region', { name: /Uncommitted/ })).toHaveTextContent("in the lane's worktree now")
    // The run's own file is marked outside scope by the design row: lantern title, no plain text.
    const row = screen.getByText('docs/x.md').closest('li') as HTMLElement
    expect(row).toHaveClass('is-outside')
    expect(row.title).toMatch(/outside scope/)
    vi.unstubAllGlobals()
  })

  it('forgets whether the previous run was the latest while it reads the next one', async () => {
    const r10 = run()
    const r11 = run({ id: 11 })
    const diff = {
      lane_id: 1, lane: 'feat/login', branch: 'feat/login', base: 'dev', base_ref: 'origin/dev', head: 'def456', mode: 'stat',
      read_at: '2026-10-08T10:04:00Z', commits_total: 0, out_of_scope: 0, commits: [],
      committed: [], uncommitted: [{ path: 'src/wip.ts', status: 'modified', added: 1, removed: 0, in_scope: true }], untracked: [],
    }
    let release: () => void = () => {}
    let reads = 0
    vi.stubGlobal('fetch', async (u: string) => {
      if (u.includes('/v1/runs?')) {
        reads++
        if (reads > 1) await new Promise<void>((res) => { release = res })
        return new Response(JSON.stringify([r10]), { status: 200 })
      }
      return new Response(JSON.stringify(u.includes('/diff') ? diff : {}), { status: 200 })
    })
    const { rerender } = render(<RunChanges run={r10} now={NOW} />)
    expect(await screen.findByRole('region', { name: /Uncommitted/ })).toBeInTheDocument()
    rerender(<RunChanges run={r11} now={NOW} />)
    expect(await screen.findByText('Reading…')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: /Uncommitted/ })).toBeNull()
    release()
    vi.unstubAllGlobals()
  })
})
