import { act, render, screen, within } from '@testing-library/react'
import type { ProjectDetail, ProjectList } from '../api/types'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { decision, lane, project, request, run, spend, status, unassigned } from '../test/fixtures'
import { ProjectPage } from './Project'
import { ProjectsPage } from './Projects'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

const detail: ProjectDetail = {
  ...project({ waiting: 2, decisions: 1, requests: 1, running: 1, lanes: 1 }),
  open_lanes: [lane({ id: 7, name: 'feat/login', mr: 34, mr_state: 'opened', pipeline_status: 'passed' })],
  runs: [run({ id: 10, state: 'running', ended: undefined })],
  pending_decisions: [decision({ id: 5, question: 'Use SSO?' })],
  pending_requests: [request({ id: 3, message: 'Who owns auth?' })],
  brief: {
    project: 'shop',
    approved: { id: 1, project: 'shop', text: 'Ship checkout.', state: 'approved', drafted_by: 'desk', approved_by: 'person', created: '2026-09-01T00:00:00Z' },
    age_text: 'approved 41 days ago',
    stale: true,
    max_age: 30 * 24 * 3600 * 1e9,
    pending: { id: 2, project: 'shop', text: 'Ship checkout and refunds.', state: 'draft', drafted_by: 'desk', created: '2026-10-07T00:00:00Z' },
  },
}

function daemon(list: ProjectList, one: ProjectDetail = detail) {
  const seen: string[] = []
  vi.stubGlobal('fetch', async (path: string) => {
    seen.push(path)
    if (path === '/v1/projects') return new Response(JSON.stringify(list), { status: 200 })
    if (path === '/v1/projects/shop') return new Response(JSON.stringify(one), { status: 200 })
    if (path === '/v1/unassigned') return new Response(JSON.stringify({ ...unassigned(), open_lanes: [], runs: [], pending_decisions: [], pending_requests: [] }), { status: 200 })
    return new Response(JSON.stringify({ error: 'no' }), { status: 404 })
  })
  return seen
}

async function show(ui: React.ReactNode) {
  const api: Api = {
    status: async () => status(),
    lanes: async () => [],
    runs: async () => [],
    run: async () => { throw new Error('unused') },
    runLog: async () => { throw new Error('unused') },
    runEvents: async () => { throw new Error('unused') },
    decisions: async () => [],
    requests: async () => [],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after) => ({ items: [], events: [], last: after }),
    answer: async () => { throw new Error('unused') },
  }
  const live = new Live(api, still)
  await act(() => live.refresh())
  await act(async () => {
    render(<LiveProvider live={live}>{ui}</LiveProvider>)
  })
}

afterEach(() => vi.unstubAllGlobals())

describe('ProjectsPage', () => {
  it('lists each project with its counts and budget, Unassigned included', async () => {
    daemon({ projects: [project({ waiting: 1, running: 2, lanes: 3 }), unassigned()] })
    await show(<ProjectsPage />)
    const shop = screen.getByRole('heading', { name: 'shop' }).closest('li') as HTMLElement
    expect(within(shop).getByText('acme-api')).toBeInTheDocument()
    expect(within(shop).getByRole('meter')).toHaveAttribute('aria-valuetext', '$1.50 of $10.00')
    expect(within(shop).getByText('waiting on you').nextSibling).toHaveTextContent('1')
    const un = screen.getByRole('heading', { name: 'Unassigned' }).closest('li') as HTMLElement
    expect(within(un).getByText('docs')).toBeInTheDocument()
    expect(within(un).queryByRole('meter')).toBeNull()
    expect(screen.getByRole('link', { name: 'Unassigned' })).toHaveAttribute('href', '#/unassigned')
    expect(screen.getByRole('link', { name: 'shop' })).toHaveAttribute('href', '#/projects/shop')
  })

  it('says so when the daemon cannot be read', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ error: 'boom' }), { status: 500 }))
    await show(<ProjectsPage />)
    expect(await screen.findByText(/Could not load projects/)).toBeInTheDocument()
  })
})

describe('ProjectPage', () => {
  it('shows lanes with MR badges, runs, what waits, spend and both briefs', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name="shop" />)
    expect(await screen.findByRole('heading', { name: 'shop' })).toBeInTheDocument()
    const lanes = within(screen.getByRole('region', { name: /Open lanes/ }))
    expect(lanes.getByText('feat/login')).toBeInTheDocument()
    expect(lanes.getByText(/!34/)).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: /Recent runs/ })).getByText(/running · /)).toBeInTheDocument()
    expect(screen.getByText('Use SSO?')).toBeInTheDocument()
    expect(screen.getByText(/Who owns auth\?/)).toBeInTheDocument()
    expect(screen.getByText('$1.50 of $10.00 today')).toBeInTheDocument()
    expect(screen.getByText('Ship checkout.')).toBeInTheDocument()
    expect(screen.getByText(/Older than 30d/)).toBeInTheDocument()
    expect(screen.getByText(/not approved/)).toBeInTheDocument()
    expect(screen.getByText('Ship checkout and refunds.')).toBeInTheDocument()
    // Read-only: nothing here can approve or edit.
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('shows the Unassigned group without a brief or budget', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name={null} />)
    expect(await screen.findByRole('heading', { name: 'Unassigned' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Brief' })).toBeNull()
    expect(screen.getByText('No lane is open.')).toBeInTheDocument()
  })

  it('names a project the daemon does not know', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name="nope" />)
    expect(await screen.findByText(/Could not load project nope/)).toBeInTheDocument()
  })
})
