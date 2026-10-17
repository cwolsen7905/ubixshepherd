import { act, fireEvent, render, screen, within } from '@testing-library/react'
import type { ConfigDoc, ProjectDetail, ProjectList, ProjectWork } from '../api/types'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { decision, lane, project, request, run, spend, status, unassigned } from '../test/fixtures'
import { ProjectPage } from './Project'
import { ProjectsPage } from './Projects'
import { mergeRule } from './Projects'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

const detail: ProjectDetail = {
  ...project({ waiting: 2, decisions: 1, requests: 1, running: 1, lanes: 1 }),
  open_lanes: [lane({ id: 7, name: 'feat/login', mr: 34, mr_state: 'open', pipeline_status: 'passed' })],
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

const configDoc: ConfigDoc = {
  path: '/c.yaml', etag: 'e1', can_write: false, raw: {}, effective: {},
  defaults: { raw: {}, effective: { autonomy: { merge: 'human' } }, overridden: [] },
  repos: { 'acme-api': { raw: {}, effective: { autonomy: { merge: 'agent' } }, overridden: [] } },
  projects: {},
  sections: [],
}

function daemon(list: ProjectList, one: ProjectDetail = detail, work?: ProjectWork, doc: ConfigDoc = configDoc) {
  const seen: string[] = []
  vi.stubGlobal('fetch', async (path: string) => {
    seen.push(path)
    if (path === '/v1/projects') return new Response(JSON.stringify(list), { status: 200 })
    if (path === '/v1/config') return new Response(JSON.stringify(doc), { status: 200 })
    if (work && path === '/v1/projects/shop/work') return new Response(JSON.stringify(work), { status: 200 })
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
    dismissRun: async () => { throw new Error('unused') },
    undismissRun: async () => { throw new Error('unused') },
    answer: async () => { throw new Error('unused') },
  }
  const live = new Live(api, still)
  await act(() => live.refresh())
  await act(async () => {
    render(<LiveProvider live={live}>{ui}</LiveProvider>)
  })
}

afterEach(() => vi.unstubAllGlobals())

describe('mergeRule', () => {
  it('reads the repo profile first, then the defaults, then you', () => {
    expect(mergeRule(configDoc, 'acme-api')).toBe('agent merges')
    expect(mergeRule(configDoc, 'other')).toBe('you merge')
    expect(mergeRule(null, 'acme-api')).toBe('you merge')
    expect(mergeRule({} as ConfigDoc, 'acme-api')).toBe('you merge')
  })
})

describe('ProjectsPage', () => {
  it('lists each project with its repos, merge rule, lanes, spend and needs-you count', async () => {
    daemon({ projects: [project({ waiting: 1, running: 2, lanes: 3 }), unassigned()] })
    await show(<ProjectsPage />)
    const shop = screen.getByRole('link', { name: 'shop' }).closest('section') as HTMLElement
    expect(within(shop).getByText('acme-api')).toBeInTheDocument()
    expect(within(shop).getByText(/agent merges/)).toBeInTheDocument()
    expect(within(shop).getByText(/3 open/)).toBeInTheDocument()
    expect(within(shop).getByText(/waiting on you/)).toBeInTheDocument()
    expect(within(shop).getByRole('meter')).toHaveAttribute('aria-valuetext', '$1.50 of $10.00')
    const un = screen.getByRole('link', { name: 'Unassigned' }).closest('section') as HTMLElement
    expect(within(un).getByText('docs')).toBeInTheDocument()
    expect(within(un).getByText(/you merge/)).toBeInTheDocument()
    expect(within(un).queryByRole('meter')).toBeNull()
    expect(screen.getByRole('link', { name: 'Unassigned' })).toHaveAttribute('href', '#/unassigned')
    expect(screen.getByRole('link', { name: 'shop' })).toHaveAttribute('href', '#/projects/shop')
  })

  it('has a New project button that goes to Settings', async () => {
    daemon({ projects: [project(), unassigned()] })
    await show(<ProjectsPage />)
    expect(screen.getByRole('link', { name: 'New project' })).toHaveAttribute('href', '#/settings?scope=new-project')
  })

  it('makes New project the one primary action and says when there are no projects', async () => {
    daemon({ projects: [] })
    await show(<ProjectsPage />)
    expect(screen.getByRole('link', { name: 'New project' })).toHaveClass('ui-btn-primary')
    expect(await screen.findByText('No projects yet')).toBeInTheDocument()
    expect(document.querySelectorAll('.ui-btn-primary')).toHaveLength(1)
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
    const lanes = screen.getByText('Open lanes').closest('section') as HTMLElement
    expect(within(lanes).getByText('feat/login')).toBeInTheDocument()
    expect(within(lanes).getByText(/!34/)).toBeInTheDocument()
    const runs = screen.getByText('Recent runs').closest('section') as HTMLElement
    expect(within(runs).getByText(/running · /)).toBeInTheDocument()
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

  it('links lane, decision and run rows to their inspector addresses', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name="shop" />)
    const lanes = screen.getByText('Open lanes').closest('section') as HTMLElement
    expect(within(lanes).getByText('feat/login').closest('a')).toHaveAttribute('href', expect.stringContaining('open=lane'))
    const waiting = screen.getByText('Waiting on you').closest('section') as HTMLElement
    expect(within(waiting).getByText('Use SSO?').closest('a')).toHaveAttribute('href', expect.stringContaining('open=decision'))
    const runs = screen.getByText('Recent runs').closest('section') as HTMLElement
    expect(within(runs).getAllByText('feat/login')[0]?.closest('a')).toHaveAttribute('href', expect.stringContaining('open=run'))
  })

  it('] moves through the waiting rows, wrapping round', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name="shop" />)
    const waiting = screen.getByText('Waiting on you').closest('section') as HTMLElement
    const rows = within(waiting).getAllByText(/Use SSO\?|needs routing/).map((el) => el.closest('a') as HTMLElement)
    expect(rows).toHaveLength(2)
    fireEvent.keyDown(window, { key: ']' })
    expect(document.activeElement).toBe(rows[0])
    fireEvent.keyDown(window, { key: ']' })
    expect(document.activeElement).toBe(rows[1])
    fireEvent.keyDown(window, { key: ']' })
    expect(document.activeElement).toBe(rows[0])
  })

  it('counts each section with a badge and keeps Edit secondary', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name="shop" />)
    const waiting = screen.getByText('Waiting on you').closest('section') as HTMLElement
    expect(within(waiting).getByText('2')).toBeInTheDocument()
    const lanes = screen.getByText('Open lanes').closest('section') as HTMLElement
    expect(within(lanes).getByText('1')).toBeInTheDocument()
    const runs = screen.getByText('Recent runs').closest('section') as HTMLElement
    expect(within(runs).getByText('1')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Edit' })).not.toHaveClass('ui-btn-primary')
    expect(document.querySelectorAll('.ui-btn-primary')).toHaveLength(0)
  })

  it('links a project to its settings, and Unassigned to none', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name="shop" />)
    expect(await screen.findByRole('link', { name: 'Edit' })).toHaveAttribute('href', '#/settings?scope=project%3Ashop')
  })

  it('shows the Unassigned group without a brief or budget', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name={null} />)
    expect(await screen.findByRole('heading', { name: 'Unassigned' })).toBeInTheDocument()
    expect(screen.queryByText('Brief')).toBeNull()
    expect(screen.getByText('No lane is open.')).toBeInTheDocument()
    expect(screen.getByText('No run yet.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Edit' })).toBeNull()
  })

  it('names a project the daemon does not know', async () => {
    daemon({ projects: [] })
    await show(<ProjectPage name="nope" />)
    expect(await screen.findByText(/Could not load project nope/)).toBeInTheDocument()
  })
})

const workFixture: ProjectWork = {
  project: 'shop',
  repos: [
    {
      repo: 'acme-api',
      found: true,
      rows: [
        { id: 'W1', area: 'web', item: 'Login page', status: 'doing', lane: 'feat/login', lane_id: 7, pr: 34, pr_url: 'https://gitlab.example.com/g/r/-/merge_requests/34', repo: 'acme-api' },
        { id: 'W2', area: 'web', item: 'Reset flow', status: 'blocked', notes: 'needs SMTP', repo: 'acme-api' },
        { id: 'W3', area: 'core', item: 'Schema', status: 'todo', repo: 'acme-api' },
        { id: 'W4', area: 'core', item: 'Shipped thing', status: 'done', pr: 2, repo: 'acme-api' },
      ],
      warnings: [{ line: 9, text: '| W5 | x |', reason: '2 columns, want 7' }],
    },
    { repo: 'acme-web', found: false, rows: [], warnings: [] },
  ],
  drift: [
    { kind: 'lane_untracked', repo: 'acme-api', lane: 'feat/extra', lane_id: 9, message: 'lane feat/extra is open and no WORK.md row names it' },
    { kind: 'doing_lane_closed', repo: 'acme-api', lane: 'old', row_id: 'W9', message: 'W9 is doing but lane old is closed' },
    { kind: 'no_work_file', repo: 'acme-web', message: 'acme-web has no docs/WORK.md on origin/main' },
  ],
}

describe('ProjectPage work section', () => {
  it('groups rows by status with doing first, folds done, links lane and PR, and shows drift', async () => {
    const seen = daemon({ projects: [project()] }, detail, workFixture)
    await show(<ProjectPage name="shop" />)
    expect(seen).toContain('/v1/projects/shop/work')
    const section = screen.getByRole('heading', { name: /^Work/ }).closest('section') as HTMLElement
    const heads = within(section).getAllByRole('heading', { level: 3 }).map((h) => h.textContent)
    expect(heads.slice(0, 3).map((t) => t?.split(' ')[0])).toEqual(['doing', 'blocked', 'todo'])
    expect(within(section).getByRole('link', { name: 'feat/login' })).toHaveAttribute('href', '#/lanes/7')
    expect(within(section).getByRole('link', { name: '#34' })).toHaveAttribute('href', 'https://gitlab.example.com/g/r/-/merge_requests/34')
    const folded = within(section).getByText('Shipped thing').closest('details') as HTMLDetailsElement
    expect(folded.open).toBe(false)
    expect(within(section).getByRole('link', { name: /feat\/extra is open/ })).toHaveAttribute('href', '#/lanes/9')
    expect(within(section).getByText(/W9 is doing but lane old is closed/)).toBeInTheDocument()
    expect(within(section).getByText(/2 columns, want 7/)).toBeInTheDocument()
    const hint = within(section).getByText(/acme-web has no docs\/WORK.md/)
    expect(hint).toHaveClass('faint')
    expect(hint).not.toHaveClass('tone-bad')
  })

  it('says so quietly when the work is not available', async () => {
    daemon({ projects: [project()] })
    await show(<ProjectPage name="shop" />)
    expect(await screen.findByText(/Work is not available/)).toBeInTheDocument()
  })
})
