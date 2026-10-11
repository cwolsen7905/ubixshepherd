import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { assemble, type TimelineData } from '../model/timeline'
import { busyDay, BUSY_NOW } from '../model/timeline.fixtures'
import { TimelineChart, type TimelineChartProps } from './Timeline'
import { TimelineLegend } from './TimelineLegend'

const busy = busyDay()
const data = assemble(busy.src)

function chart(o: Partial<TimelineChartProps> = {}) {
  const props: TimelineChartProps = { data, range: 'today', now: BUSY_NOW, agent: '', showIdle: false, width: 1200, ...o }
  return render(<TimelineChart {...props} />)
}

function stubMedia(matches: (q: string) => boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: matches(query), media: query, addEventListener: () => {}, removeEventListener: () => {},
  }))
}

beforeEach(() => window.localStorage.clear())
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('the timeline chart', () => {
  it('draws one track per lane that did something, grouped by repo', () => {
    const { container } = chart()
    expect(container.querySelectorAll('.tl-row')).toHaveLength(24)
    const heads = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
    expect(heads).toEqual([
      '▾acme-api7 lanes', '▾acme-docs3 lanes', '▾acme-infra5 lanes', '▾acme-mobile3 lanes', '▾acme-web6 lanes',
    ])
    expect(container.querySelector('.tl-row .tl-name')).toHaveAttribute('href', expect.stringMatching(/^#\/lanes\/\d+$/))
  })

  it('gives every bar a link to its log and an accessible name', () => {
    const { container } = chart()
    const bars = [...container.querySelectorAll<SVGAElement>('a.tl-run')]
    expect(bars.length).toBeGreaterThan(100)
    for (const b of bars) {
      expect(b.getAttribute('href')).toMatch(/^#\/runs\/\d+\/log$/)
      expect(b.getAttribute('aria-label')).toMatch(/^(claude|opencode|cursor|copilot), .+, (running for|succeeded in|failed after|interrupted after|stopped after) .+, \d+ commits?/)
    }
    expect(container.querySelectorAll('a.tl-run.is-running')).toHaveLength(5)
  })

  it('colours a bar by its agent and an unknown agent neutrally, with its name always printed', () => {
    const odd: TimelineData = { ...data, runs: data.runs.map((r, i) => (i === 0 ? { ...r, agent: 'zed' } : r)) }
    const { container } = chart({ data: odd })
    const first = container.querySelector(`a[data-run="${odd.runs[0]?.id}"]`)
    expect(first).toHaveClass('tl-agent-other')
    expect(first?.querySelector('.tl-bar-label')).toHaveTextContent('zed')
    expect(container.querySelector('a.tl-agent-claude')).not.toBeNull()
  })

  it('marks failures with a notch, merges with a tick, and what waits with a lantern', () => {
    const { container } = chart()
    expect(container.querySelectorAll('a.is-failed .tl-notch')).toHaveLength(2)
    expect(container.querySelectorAll('a.tl-merge')).toHaveLength(3)
    const open = container.querySelectorAll('a.tl-ask.is-open')
    expect(open).toHaveLength(2)
    expect(container.querySelectorAll('a.tl-ask.is-request')).toHaveLength(1)
    expect(container.querySelectorAll('a.tl-ask.is-answered')).toHaveLength(2)
    const decision = [...open].find((a) => !a.classList.contains('is-request'))
    expect(decision).toHaveAttribute('href', '#/decisions?d=900')
    expect(decision?.getAttribute('aria-label')).toMatch(/^Decision waiting on you, waiting .+: Stream the export/)
    expect(container.querySelectorAll('.tl-row.is-waiting')).toHaveLength(2)
  })

  it('draws the spend band against the budget, and the now line', () => {
    const { container } = chart()
    expect(screen.getByRole('img', { name: /Spend so far today: 18.40 dollars of 25 budget/ })).toBeInTheDocument()
    expect(container.querySelector('.tl-budget-label')).toHaveTextContent('$25 budget')
    expect(container.querySelector('.tl-now-line')).not.toBeNull()
  })

  it('shows the same tooltip on hover and on keyboard focus', async () => {
    const user = userEvent.setup()
    const { container } = chart()
    const bar = container.querySelector('a.tl-run:not(.is-running)') as SVGAElement
    await user.hover(bar)
    const hovered = screen.getByRole('tooltip').textContent
    expect(hovered).toMatch(/commit/)
    expect(hovered).toMatch(/Task \d+ for /)
    await user.unhover(bar)
    expect(screen.queryByRole('tooltip')).toBeNull()
    act(() => bar.focus())
    expect(screen.getByRole('tooltip').textContent).toBe(hovered)
    act(() => bar.blur())
    expect(screen.queryByRole('tooltip')).toBeNull()
  })

  it('shows what a decision asks, how long it has waited and what is recommended', async () => {
    const user = userEvent.setup()
    const { container } = chart()
    await user.hover(container.querySelector('a.tl-ask.is-open:not(.is-request)') as SVGAElement)
    const tip = screen.getByRole('tooltip')
    expect(tip).toHaveTextContent('decision, waiting on you')
    expect(tip).toHaveTextContent('Stream the export or buffer the whole file first?')
    expect(tip).toHaveTextContent('Stream')
    expect(tip).toHaveTextContent('recommended')
    expect(tip).toHaveTextContent('waiting since')
  })

  it('says when the spend moves: a running run adds its cost when it ends', async () => {
    const user = userEvent.setup()
    const { container } = chart()
    const hit = container.querySelector('.tl-spend-hit') as SVGRectElement
    fireEvent.pointerMove(hit, { clientX: 600, clientY: 10 })
    expect(screen.getByRole('tooltip')).toHaveTextContent('A running run adds its cost when it ends.')
    await user.unhover(hit)
  })

  it('dims the agents that are not the filter, and keeps them', () => {
    const { container } = chart({ agent: 'claude' })
    expect(container.querySelectorAll('a.tl-run.tl-agent-claude.is-dim')).toHaveLength(0)
    expect(container.querySelectorAll('a.tl-run.tl-agent-cursor:not(.is-dim)')).toHaveLength(0)
    expect(container.querySelectorAll('a.tl-run.tl-agent-cursor.is-dim').length).toBeGreaterThan(0)
  })

  it('hides a repo’s tracks when its group is collapsed, and remembers it', async () => {
    const user = userEvent.setup()
    const { container, unmount } = chart()
    await user.click(screen.getByRole('button', { name: /acme-web/ }))
    expect(container.querySelectorAll('.tl-row')).toHaveLength(18)
    expect(screen.getByRole('button', { name: /acme-web/ })).toHaveAttribute('aria-expanded', 'false')
    expect(JSON.parse(window.localStorage.getItem('shepherd.timeline.collapsed') ?? '[]')).toEqual(['acme-web'])
    unmount()
    const again = chart()
    expect(again.container.querySelectorAll('.tl-row')).toHaveLength(18)
  })

  it('leaves out idle lanes unless asked', () => {
    const a = chart()
    expect(a.container.querySelectorAll('.tl-row')).toHaveLength(24)
    a.unmount()
    expect(chart({ showIdle: true }).container.querySelectorAll('.tl-row')).toHaveLength(25)
  })

  it('says so, and draws no ghost bars, when nothing ran', () => {
    const empty: TimelineData = { lanes: [], runs: [], decisions: [], spend: null }
    const { container } = chart({ data: empty, empty: 'No run started today.' })
    expect(screen.getByText('No run started today.')).toBeInTheDocument()
    expect(container.querySelectorAll('a.tl-run')).toHaveLength(0)
    expect(container.querySelector('.tl-axis')).not.toBeNull()
  })

  it('takes away the motion for a person who asked for less, and keeps the markers', () => {
    stubMedia((q) => q.includes('prefers-reduced-motion'))
    const { container } = chart()
    expect(container.querySelector('section.tl')).toHaveClass('tl-still')
    expect(container.querySelectorAll('.tl-cap').length).toBeGreaterThan(0)
    expect(container.querySelectorAll('.tl-halo').length).toBeGreaterThan(0)
  })

  it('animates by default', () => {
    stubMedia(() => false)
    const { container } = chart()
    expect(container.querySelector('section.tl')).not.toHaveClass('tl-still')
  })

  it('moves a running bar and the now line from a timer, once a second', () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(BUSY_NOW)
    const { container } = chart()
    const bar = container.querySelector('a.is-running .tl-bar') as SVGRectElement
    const now = container.querySelector('.tl-now-line') as SVGLineElement
    const w0 = Number(bar.getAttribute('width'))
    const x0 = Number(now.getAttribute('x1'))
    act(() => {
      vi.advanceTimersByTime(10 * 60_000)
    })
    // Ten minutes of a day of about 15 hours across 1200 px is roughly 13 px.
    expect(Number(bar.getAttribute('width')) - w0).toBeGreaterThan(10)
    expect(Number(now.getAttribute('x1')) - x0).toBeGreaterThan(10)
    expect(container.querySelector('[data-live="now-label"]')).toHaveTextContent('15:10')
  })

  it('does not tick while the tab is hidden', () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(BUSY_NOW)
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    const { container } = chart()
    const bar = container.querySelector('a.is-running .tl-bar') as SVGRectElement
    const w0 = bar.getAttribute('width')
    act(() => {
      vi.advanceTimersByTime(60_000)
    })
    expect(bar.getAttribute('width')).toBe(w0)
  })

  it('holds the marks that did not change when the data is read again', () => {
    const { container, rerender } = chart()
    const before = container.querySelector('a[data-run]') as SVGAElement
    rerender(<TimelineChart data={assemble(structuredClone(busy.src))} range="today" now={BUSY_NOW} agent="" showIdle={false} width={1200} />)
    expect(container.querySelector('a[data-run]')).toBe(before)
  })
})

describe('the legend', () => {
  it('links each agent to the page filtered to it, and the chosen one back to all', () => {
    render(<TimelineLegend agents={['claude', 'opencode', 'cursor', 'copilot']} agent="cursor" range="24h" />)
    const nav = screen.getByRole('navigation', { name: 'Agents' })
    expect(within(nav).getByRole('link', { name: 'claude' })).toHaveAttribute('href', '#/timeline?range=24h&agent=claude')
    const on = within(nav).getByRole('link', { name: 'cursor' })
    expect(on).toHaveAttribute('aria-current', 'true')
    expect(on).toHaveAttribute('href', '#/timeline?range=24h')
  })
})
