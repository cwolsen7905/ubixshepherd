// Projects on the board and on their own pages. Membership is the daemon's (GET
// /v1/projects); the counts beside each group come from the same items the rows are, so a
// group's summary never disagrees with what sits under it.
import type { ProjectBudget, ProjectSummary } from '../api/types'
import type { BoardItem } from './board'

export const VIEW_KEY = 'shepherd.board.view'
export const COLLAPSED_KEY = 'shepherd.board.collapsed'

export type BoardView = 'grouped' | 'flat'

/** What a group of the board is called when it has no project. */
export const UNASSIGNED = 'Unassigned'

export interface ProjectGroup {
  /** A project's name; '' is Unassigned. */
  name: string
  title: string
  unassigned: boolean
  items: BoardItem[]
  summary: GroupSummary
}

export interface GroupSummary {
  /** Open lanes, as the daemon counts them (including idle ones the board has no row for). */
  open: number
  /** Lanes with a run going, and what waits on the person: from the rows. */
  running: number
  waiting: number
  /** Today's dollars, or null before the daemon has answered. */
  spend: number | null
}

/** Which project's group a repo sits in: the name, or '' for Unassigned. */
export function projectOfRepo(projects: ProjectSummary[]): (repo: string) => string {
  const by = new Map<string, string>()
  for (const p of projects) {
    if (p.unassigned) continue
    for (const r of p.repos) by.set(r, p.name)
  }
  return (repo) => by.get(repo) ?? ''
}

/**
 * The board's items in a group per project, in the daemon's order (by name) with
 * Unassigned last; a project with nothing on the board still has its group, since an
 * empty project is information. Unassigned is shown only when something is in it.
 */
export function groupByProject(items: BoardItem[], projects: ProjectSummary[]): ProjectGroup[] {
  const of = projectOfRepo(projects)
  const named = projects.filter((p) => !p.unassigned)
  const un = projects.find((p) => p.unassigned)
  const buckets = new Map<string, BoardItem[]>()
  for (const it of items) {
    const n = of(it.repo)
    buckets.set(n, [...(buckets.get(n) ?? []), it])
  }
  const make = (p: ProjectSummary | undefined, name: string): ProjectGroup => {
    const its = buckets.get(name) ?? []
    return {
      name, title: name || UNASSIGNED, unassigned: name === '', items: its,
      summary: {
        open: p?.lanes ?? new Set(its.filter((i) => i.group !== 'done' && i.laneId !== undefined).map((i) => i.laneId)).size,
        running: its.filter((i) => i.group === 'working' || (i.group === 'broken' && i.detail.endsWith(', fixing'))).length,
        waiting: its.filter((i) => i.group === 'needs').length,
        spend: p ? p.spend_today : null,
      },
    }
  }
  const out = named.map((p) => make(p, p.name))
  const unItems = buckets.get('') ?? []
  if (unItems.length > 0 || (un && un.lanes > 0)) out.push(make(un, ''))
  return out
}

/** "3 open · 1 running · 1 waiting · $1.20 today", leaving out the zero counts after open. */
export function summaryText(s: GroupSummary, money: (n: number) => string): string {
  const parts = [`${s.open} open`]
  if (s.running > 0) parts.push(`${s.running} running`)
  if (s.waiting > 0) parts.push(`${s.waiting} waiting`)
  if (s.spend !== null) parts.push(`${money(s.spend)} today`)
  return parts.join(' · ')
}

/** How far through its budget a project is; null for no cap. */
export function budgetUse(b: ProjectBudget | undefined): { fraction: number; percent: number; tone: 'ok' | 'warn' | 'bad' } | null {
  if (!b || b.amount <= 0) return null
  const fraction = b.spent / b.amount
  const tone = fraction >= 1 ? 'bad' : fraction >= 0.8 ? 'warn' : 'ok'
  return { fraction, percent: Math.round(fraction * 100), tone }
}

/** "today" or "this month": the words for a budget's period. */
export function periodPhrase(period: string): string {
  return period === 'month' ? 'this month' : 'today'
}

/** Whether a stored choice says flat; grouped is the default when there are projects. */
export function readView(store: Pick<Storage, 'getItem'> | undefined): BoardView | null {
  try {
    const v = store?.getItem(VIEW_KEY)
    return v === 'flat' || v === 'grouped' ? v : null
  } catch {
    return null
  }
}

export function writeView(store: Pick<Storage, 'setItem'> | undefined, v: BoardView) {
  try {
    store?.setItem(VIEW_KEY, v)
  } catch {
    // A browser that refuses storage forgets the choice; nothing else depends on it.
  }
}

export function readCollapsed(store: Pick<Storage, 'getItem'> | undefined): Set<string> {
  try {
    const raw = store?.getItem(COLLAPSED_KEY)
    const v: unknown = raw ? JSON.parse(raw) : []
    return new Set(Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])
  } catch {
    return new Set()
  }
}

export function writeCollapsed(store: Pick<Storage, 'setItem'> | undefined, names: Set<string>) {
  try {
    store?.setItem(COLLAPSED_KEY, JSON.stringify([...names]))
  } catch {
    // As writeView.
  }
}

/** Collapsed groups are stored by a key that cannot clash with a project's name. */
export const collapseKey = (g: { name: string; unassigned: boolean }) => (g.unassigned ? '\u0000unassigned' : g.name)
