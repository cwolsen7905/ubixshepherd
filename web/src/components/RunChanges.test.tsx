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
    expect(screen.getByText('outside scope')).toBeInTheDocument()
    vi.unstubAllGlobals()
  })
})
