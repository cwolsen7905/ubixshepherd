import type { BoardItem } from './board'
import {
  budgetUse, collapseKey, COLLAPSED_KEY, groupByProject, projectOfRepo, readCollapsed, readView, summaryText, VIEW_KEY,
  writeCollapsed, writeView,
} from './projects'
import { project, unassigned } from '../test/fixtures'

const item = (o: Partial<BoardItem>): BoardItem => ({
  key: 'k', group: 'working', name: 'lane', laneId: 1, repo: 'acme-api', detail: 'run 1', at: '2026-10-08T10:00:00Z', ...o,
})

describe('groupByProject', () => {
  const list = [project({ name: 'shop', repos: ['acme-api'], lanes: 3, spend_today: 1.2 }), project({ name: 'web', repos: ['site'], lanes: 0, spend_today: 0 }), unassigned()]

  it('puts each item under its repo’s project, Unassigned last', () => {
    const items = [
      item({ key: 'a', repo: 'docs', laneId: 5 }),
      item({ key: 'b', repo: 'acme-api', group: 'needs', laneId: 2 }),
      item({ key: 'c', repo: 'acme-api', group: 'working', laneId: 3 }),
    ]
    const groups = groupByProject(items, list)
    expect(groups.map((g) => g.title)).toEqual(['shop', 'web', 'Unassigned'])
    expect(groups[0]?.items.map((i) => i.key)).toEqual(['b', 'c'])
    expect(groups[0]?.summary).toEqual({ open: 3, running: 1, waiting: 1, spend: 1.2 })
    expect(groups[1]?.items).toEqual([])
    expect(groups[2]?.items.map((i) => i.key)).toEqual(['a'])
  })

  it('treats a repo no project names as Unassigned, even before the daemon lists it', () => {
    const groups = groupByProject([item({ repo: 'mystery' })], [project()])
    expect(groups.map((g) => g.title)).toEqual(['shop', 'Unassigned'])
    expect(groups[1]?.summary.spend).toBeNull()
  })

  it('leaves Unassigned out when nothing is in it', () => {
    const groups = groupByProject([], [project(), unassigned({ lanes: 0 })])
    expect(groups.map((g) => g.title)).toEqual(['shop'])
  })

  it('counts a fixing broken lane as running', () => {
    const groups = groupByProject([item({ group: 'broken', detail: 'pipeline failed, fixing' })], [project()])
    expect(groups[0]?.summary.running).toBe(1)
  })
})

describe('projectOfRepo', () => {
  it('names the project or the empty string', () => {
    const of = projectOfRepo([project({ name: 'shop', repos: ['a'] }), unassigned({ repos: ['b'] })])
    expect(of('a')).toBe('shop')
    expect(of('b')).toBe('')
  })
})

describe('summaryText', () => {
  it('shows open always and the rest when they are not zero', () => {
    const money = (n: number) => `$${n.toFixed(2)}`
    expect(summaryText({ open: 2, running: 0, waiting: 0, spend: null }, money)).toBe('2 open')
    expect(summaryText({ open: 3, running: 1, waiting: 2, spend: 1.2 }, money)).toBe('3 open · 1 running · 2 waiting · $1.20 today')
  })
})

describe('budgetUse', () => {
  const b = (spent: number, amount = 10) => ({ amount, cap: 'hard', period: 'day', since: 'x', spent })
  it('has no use without a cap', () => {
    expect(budgetUse(undefined)).toBeNull()
    expect(budgetUse(b(5, 0))).toBeNull()
  })
  it('warns at 80% and is bad at 100%', () => {
    expect(budgetUse(b(5))?.tone).toBe('ok')
    expect(budgetUse(b(8))?.tone).toBe('warn')
    expect(budgetUse(b(10))?.tone).toBe('bad')
    expect(budgetUse(b(12))?.percent).toBe(120)
  })
})

describe('stored choices', () => {
  const memory = () => {
    const m = new Map<string, string>()
    return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) }
  }
  it('remembers the view and the collapsed groups', () => {
    const s = memory()
    expect(readView(s)).toBeNull()
    writeView(s, 'flat')
    expect(readView(s)).toBe('flat')
    writeCollapsed(s, new Set(['shop', collapseKey({ name: '', unassigned: true })]))
    expect([...readCollapsed(s)].sort()).toEqual(['\u0000unassigned', 'shop'])
  })
  it('ignores a stored value it does not understand and a storage that throws', () => {
    const s = memory()
    s.setItem(VIEW_KEY, 'sideways')
    s.setItem(COLLAPSED_KEY, '{not json')
    expect(readView(s)).toBeNull()
    expect(readCollapsed(s).size).toBe(0)
    const broken = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('denied') } }
    expect(readView(broken)).toBeNull()
    expect(() => writeView(broken, 'flat')).not.toThrow()
  })
})
