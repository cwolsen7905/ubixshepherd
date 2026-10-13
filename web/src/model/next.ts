// The next-up list: work that is planned but not being acted on (docs/backlog-next.md).
// The daemon orders it and derives started and done from the lane; this holds how the
// page groups it, which moves are possible, and the words the page prefills for the desk.
import type { FeedItem, MatrixRow, NextChosen, NextItem, NextList, NextState, Workspace } from '../api/types'
import type { Tone } from './marks'

/** The sections of the page, in order; done and dropped sit behind a toggle. */
export const NEXT_SECTIONS: readonly NextChosen[] = ['approved', 'planned', 'deferred', 'done', 'dropped']

export const NEXT_SECTION_NAMES: Record<NextChosen, string> = {
  approved: 'Approved',
  planned: 'Planned',
  deferred: 'Deferred',
  done: 'Done',
  dropped: 'Dropped',
}

/** What each state says about the item, in words and a glyph that carries it without colour. */
export const NEXT_MARKS: Record<NextState, { glyph: string; tone: Tone; label: string }> = {
  approved: { glyph: '◇', tone: 'accent', label: 'approved' },
  planned: { glyph: '○', tone: 'muted', label: 'planned, not yet approved' },
  deferred: { glyph: '⏸', tone: 'muted', label: 'deferred' },
  started: { glyph: '●', tone: 'accent', label: 'started' },
  done: { glyph: '✓', tone: 'ok', label: 'done' },
  dropped: { glyph: '✗', tone: 'muted', label: 'dropped' },
}

/** The marker on the first approved item with no lane: what the list says to do next. */
export const NEXT_MARKER = { glyph: '▶', tone: 'accent' as Tone, label: 'next up' }

/** The section an item sits in: its chosen state, so a started item stays among the approved. */
export const sectionOf = (it: NextItem): NextChosen => it.stored_state

/** The list as sections in page order; closed ones only when `closed` is set. */
export function nextSections(items: NextItem[], closed: boolean): { state: NextChosen; items: NextItem[] }[] {
  return NEXT_SECTIONS.filter((s) => closed || (s !== 'done' && s !== 'dropped')).map((state) => ({
    state,
    items: items.filter((it) => sectionOf(it) === state),
  }))
}

/** How many done or dropped items the page holds back. */
export function closedCount(items: NextItem[]): number {
  return items.filter((it) => it.stored_state === 'done' || it.stored_state === 'dropped').length
}

/** Whether an item can move up or down: within its section, so the first cannot go up. */
export function canMove(items: NextItem[], it: NextItem, dir: 'up' | 'down'): boolean {
  const peers = items.filter((x) => sectionOf(x) === sectionOf(it))
  const at = peers.findIndex((x) => x.id === it.id)
  return dir === 'up' ? at > 0 : at >= 0 && at < peers.length - 1
}

/** The person's moves of an item between states, as the buttons the page shows. */
export function stateActions(it: NextItem): { label: string; state: NextChosen }[] {
  switch (it.stored_state) {
    case 'planned':
      return [{ label: 'Approve', state: 'approved' }, { label: 'Defer', state: 'deferred' }, { label: 'Drop', state: 'dropped' }]
    case 'approved':
      return [{ label: 'Defer', state: 'deferred' }, { label: 'Drop', state: 'dropped' }]
    case 'deferred':
      return [{ label: 'Approve', state: 'approved' }, { label: 'Drop', state: 'dropped' }]
    case 'dropped':
      return [{ label: 'Restore', state: 'planned' }]
    default:
      return []
  }
}

/** The open items for the rail, in the person's order: approved (started ones in place), then planned, then deferred. */
export function railItems(list: NextList): NextItem[] {
  const open = (it: NextItem) => it.state !== 'done' && it.state !== 'dropped' && it.stored_state !== 'done' && it.stored_state !== 'dropped'
  return NEXT_SECTIONS.flatMap((s) => list.items.filter((it) => open(it) && sectionOf(it) === s))
}

/** What a row says about its item: the state's glyph, tone and word; a planned item an agent proposed reads as proposed. */
export function rowState(it: NextItem): { glyph: string; tone: Tone; label: string } {
  if (it.state === 'planned' && it.proposed) return { ...NEXT_MARKS.planned, label: 'proposed, not yet approved' }
  return NEXT_MARKS[it.state]
}

/** The one word the rail shows beside a row's title. */
export function rowStateWord(it: NextItem): string {
  return it.state === 'planned' && it.proposed ? 'proposed' : it.state
}

/** How many approved items have no lane yet. */
export function waitingCount(list: NextList): number {
  return list.items.filter((it) => it.stored_state === 'approved' && it.state === 'approved').length
}

/** The words Start puts in the chat draft: the person sends them, the desk opens the lane. */
export function startText(it: Pick<NextItem, 'id' | 'title'>): string {
  return `Open a lane for next-up item ${it.id}: ${it.title}`
}

/** Whether Start applies: approved or planned work that no lane has taken. */
export function canStart(it: NextItem): boolean {
  return it.state === 'approved'
}

/** The workspace the chat page shows when the person has not picked one (see ChatPage). */
export function deskWorkspace(workspaces: Workspace[]): number {
  return workspaces.length > 1 ? (workspaces[0]?.id ?? 0) : 0
}

const VIEW_KEY = 'shepherd.chat.rail'

export type RailView = 'now' | 'next'

/** The rail's stored view; Now when none was chosen. */
export function readRailView(store: Pick<Storage, 'getItem'>): RailView {
  try {
    return store.getItem(VIEW_KEY) === 'next' ? 'next' : 'now'
  } catch {
    return 'now'
  }
}

export function writeRailView(store: Pick<Storage, 'setItem'>, view: RailView) {
  try {
    store.setItem(VIEW_KEY, view)
  } catch {
    // no storage: the choice lasts until the page reloads
  }
}

/** The plan doc's name for the row: its path, shortened to the last two parts. */
export function planLabel(plan: string): string {
  const parts = plan.split('/').filter(Boolean)
  return parts.slice(-2).join('/')
}

/** The matrix rows of a list, grouped by repo in the order the daemon sent them; blocked rows first within a repo. */
export function matrixGroups(list: NextList | null): { repo: string; rows: MatrixRow[] }[] {
  const out: { repo: string; rows: MatrixRow[] }[] = []
  for (const row of list?.matrix ?? []) {
    const repo = row.repo ?? ''
    let g = out.find((x) => x.repo === repo)
    if (!g) out.push((g = { repo, rows: [] }))
    g.rows.push(row)
  }
  for (const g of out) g.rows.sort((a, b) => Number(b.state === 'blocked') - Number(a.state === 'blocked'))
  return out
}

/** The id of the newest plan item in the feed, 0 when there is none: when it moves, the list is read again. */
export function lastPlanId(feed: Pick<FeedItem, 'id' | 'kind'>[]): number {
  let last = 0
  for (const f of feed) if (f.kind === 'plan' && f.id > last) last = f.id
  return last
}
