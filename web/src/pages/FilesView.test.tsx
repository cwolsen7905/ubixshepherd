import { act, render, screen, within } from '@testing-library/react'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, status } from '../test/fixtures'
import { FilesPage } from './Files'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const still: Clock = { now: () => NOW, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

async function show(props: Parameters<typeof FilesPage>[0], lanes = [lane(), lane({ id: 2, repo_id: 2, repo: 'blog', name: 'old', branch: 'old', state: 'closed' })]) {
  const seen: URL[] = []
  vi.stubGlobal('fetch', async (path: string) => {
    seen.push(new URL(path, 'http://x'))
    return new Response(JSON.stringify({ path: '', name: '', kind: 'dir', children: [{ path: 'README.md', name: 'README.md', kind: 'file', size: 10 }] }), { status: 200 })
  })
  const api = {
    status: async () => status(), lanes: async () => lanes, runs: async () => [], decisions: async () => [], requests: async () => [],
    spend: async () => null,
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async () => ({ items: [], events: [], last: 0 }),
  } as unknown as Api
  const live = new Live(api, still)
  await act(() => live.refresh())
  render(
    <LiveProvider live={live}>
      <FilesPage {...props} />
    </LiveProvider>,
  )
  return seen
}

afterEach(() => vi.unstubAllGlobals())

describe('the Files page', () => {
  it('reads a lane worktree of the chosen repo', async () => {
    const seen = await show({ repo: 1 })
    expect(await screen.findByText('README.md')).toBeInTheDocument()
    expect(seen[0]?.pathname).toBe('/v1/lanes/1/files')
    expect(seen[0]?.searchParams.has('ref')).toBe(false)
    expect(screen.getByRole('combobox', { name: 'Repo' })).toHaveValue('1')
  })

  it('reads a closed lane at its branch, and any ref as given', async () => {
    const seen = await show({ lane: 2 })
    await screen.findByText('README.md')
    expect(seen[0]?.searchParams.get('ref')).toBe('old')
    expect(screen.getByText(/branch was deleted after the merge|If the branch was deleted/)).toBeInTheDocument()
    vi.unstubAllGlobals()
    const again = await show({ lane: 1, gitRef: 'v1.0' })
    await screen.findAllByText('README.md')
    expect(again.at(-1)?.searchParams.get('ref')).toBe('v1.0')
  })

  it('has the shared page header: the way back, Close with Esc, the title and the pickers', async () => {
    await show({ repo: 1 })
    const head = document.querySelector('[data-component="PageHeader"]') as HTMLElement
    expect(within(head).getByRole('link', { name: '← Board' })).toBeInTheDocument()
    expect(within(head).getByRole('button', { name: /^Close\s*Esc$/ })).toBeInTheDocument()
    expect(within(head).getByRole('heading', { level: 1, name: 'Files' })).toBeInTheDocument()
    expect(within(head).getByRole('combobox', { name: 'Repo' })).toBeInTheDocument()
  })

  it('says so when no repo has a lane', async () => {
    await show({}, [])
    expect(await screen.findByText('No repo to browse.')).toBeInTheDocument()
  })
})
