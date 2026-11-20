import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import type { ReviewBotCounts, ReviewBotProject, ReviewBots } from '../api/types'
import { ReviewBotsPage } from './ReviewBotsPage'
import { spend, summary } from './model'

const zero: ReviewBotCounts = { jobs: 0, working: 0, proposals: 0, dismissals: 0, needs_you: 0, fixes: 0, spend_usd: 0, stamped: 0 }
const proj = (name: string, c: Partial<ReviewBotProject> = {}): ReviewBotProject => ({ name, ...zero, ...c })

const report: ReviewBots = {
  as_of: '2026-11-19T11:55:00Z',
  bots: [
    {
      name: 'Reviewer',
      user: 'review-bot',
      label_prefix: 'bot::',
      window_days: 30,
      projects: [
        proj('app', { jobs: 6, proposals: 2, dismissals: 1, needs_you: 1, fixes: 3, spend_usd: 0.0122, stamped: 2, needs_you_items: [{ iid: 3, title: 'Stuck merge', url: 'https://gl.example/app/-/merge_requests/3' }] }),
        proj('lib', { error: 'glab: not logged in' }),
      ],
      total: { ...zero, jobs: 6, proposals: 2, dismissals: 1, needs_you: 1, fixes: 3, spend_usd: 0.0122, stamped: 2 },
    },
    { name: 'Other', user: 'other-bot', label_prefix: 'o::', window_days: 7, projects: [proj('app')], total: zero },
  ],
}

function daemon(body: unknown, status = 200) {
  const seen: string[] = []
  vi.stubGlobal('fetch', async (path: string) => {
    seen.push(path)
    return new Response(JSON.stringify(body), { status })
  })
  return seen
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  document.documentElement.removeAttribute('data-theme')
})

describe('ReviewBotsPage', () => {
  it.each(['light', 'dark'])('shows each project with its counts in the %s theme', async (theme) => {
    document.documentElement.setAttribute('data-theme', theme)
    daemon(report)
    render(<ReviewBotsPage />)
    const row = (await screen.findByRole('row', { name: 'app' })) as HTMLElement
    expect(within(row).getByText('6')).toBeInTheDocument()
    expect(within(row).getByText('3')).toBeInTheDocument()
    expect(within(row).getByText('$0.0122')).toBeInTheDocument()
    expect(row.closest('[data-component="ReviewBotTable"]')).not.toBeNull()
  })

  it('says needs you with a glyph and words, and lists the request waiting', async () => {
    daemon(report)
    render(<ReviewBotsPage />)
    const row = (await screen.findByRole('row', { name: 'app' })) as HTMLElement
    expect(within(row).getByText(/1 needs you/)).toHaveClass('sh-bots-waiting')
    const waiting = document.querySelector('[data-component="ReviewBotWaiting"]') as HTMLElement
    expect(within(waiting).getByRole('link', { name: 'app!3' })).toHaveAttribute('href', 'https://gl.example/app/-/merge_requests/3')
  })

  it('shows zero faint, and a repo it could not read as a failure with words', async () => {
    daemon(report)
    render(<ReviewBotsPage />)
    const lib = (await screen.findByRole('row', { name: 'lib' })) as HTMLElement
    for (const z of within(lib).getAllByText('0')) expect(z).toHaveClass('sh-bots-zero')
    const err = document.querySelector('[data-component="ReviewBotError"]') as HTMLElement
    expect(err).toHaveTextContent('lib: glab: not logged in')
    expect(within(err).getByText('Failed')).toBeInTheDocument()
  })

  it('has a tab per bot and a total row for a bot with several projects', async () => {
    daemon(report)
    render(<ReviewBotsPage />)
    await screen.findByRole('row', { name: 'app' })
    expect(screen.getByRole('row', { name: 'Total' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: /Other/ }))
    expect(screen.queryByRole('row', { name: 'Total' })).toBeNull()
    expect(screen.getByText(/other-bot · labels o::… · last 7 days/)).toBeInTheDocument()
  })

  it('refresh reads the forge again', async () => {
    const seen = daemon(report)
    render(<ReviewBotsPage />)
    await screen.findByRole('row', { name: 'app' })
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await screen.findByRole('button', { name: 'Refresh' })
    expect(seen).toContain('/v1/review-bots?refresh=1')
  })

  it('says how to configure a bot when there are none', async () => {
    daemon({ as_of: '2026-11-19T11:55:00Z', bots: [] })
    render(<ReviewBotsPage />)
    expect(await screen.findByText('No review bots configured')).toBeInTheDocument()
  })

  it('says so when the daemon cannot be read', async () => {
    daemon({ error: 'boom' }, 500)
    render(<ReviewBotsPage />)
    expect(await screen.findByText(/Review bots did not load/)).toBeInTheDocument()
  })
})

describe('review bots model', () => {
  it('writes spend and the header summary', () => {
    expect(spend(0, 0)).toBe('not stamped')
    expect(spend(0.0022, 1)).toBe('$0.0022')
    expect(spend(12.5, 3)).toBe('$12.50')
    expect(summary(report.bots)).toBe('2 bots · 1 needing you')
  })
})
