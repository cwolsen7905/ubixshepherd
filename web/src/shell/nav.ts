import { href, type Route } from '../router'
import { attentionCount, attentionOf } from '../state/attention'
import type { Waiting } from '../api/types'

export type NavKey = 'inbox' | 'chat' | 'board' | 'next' | 'artifacts' | 'runs' | 'timeline' | 'history' | 'files' | 'rules' | 'projects' | 'usage' | 'settings' | 'status'

export interface NavItem {
  key: NavKey
  label: string
  /** Mark shown in the sidebar: a glyph from the board's glyph language. */
  mark: string
  to: () => string
  /** True when the route belongs under this entry. */
  current: (r: Route) => boolean
  /** Optional count badge; the tone for the Badge component. */
  count?: number
  countTone?: 'neutral' | 'attention' | 'bad' | 'ok'
}

export interface NavGroup {
  name: string
  items: NavItem[]
}

/** The sidebar's entries, in the plan's groups. Entries for pages that do not exist yet are not listed. */
export const NAV: NavGroup[] = [
  {
    name: 'Primary',
    items: [
      { key: 'inbox', label: 'Inbox', mark: 'I', to: () => href.inbox(), current: (r) => r.page === 'inbox' },
      { key: 'chat', label: 'Conversation', mark: 'C', to: () => href.chat(), current: (r) => r.page === 'chat' },
    ],
  },
  {
    name: 'Work',
    items: [
      { key: 'board', label: 'Board', mark: '◻', to: () => href.board(), current: (r) => r.page === 'board' || r.page === 'lane' },
      { key: 'next', label: 'Next up', mark: 'N', to: () => href.next(), current: (r) => r.page === 'next' },
      { key: 'artifacts', label: 'Artifacts', mark: 'A', to: () => href.artifacts(), current: (r) => r.page === 'artifacts' },
      { key: 'files', label: 'Files', mark: 'F', to: () => href.files(), current: (r) => r.page === 'files' },
      { key: 'runs', label: 'Runs', mark: 'R', to: () => href.runs(), current: (r) => r.page === 'runs' || r.page === 'log' },
      { key: 'timeline', label: 'Timeline', mark: 'T', to: () => href.timeline(), current: (r) => r.page === 'timeline' },
      { key: 'history', label: 'History', mark: 'H', to: () => href.history(), current: (r) => r.page === 'history' },
    ],
  },
  {
    name: 'Setup',
    items: [
      {
        key: 'projects',
        label: 'Projects',
        mark: 'P',
        to: () => href.projects(),
        current: (r) => r.page === 'projects' || r.page === 'project' || r.page === 'unassigned',
      },
      { key: 'rules', label: 'Rules', mark: 'L', to: () => href.rules(), current: (r) => r.page === 'rules' },
      { key: 'usage', label: 'Usage', mark: 'U', to: () => href.usage(), current: (r) => r.page === 'usage' },
      { key: 'settings', label: 'Settings', mark: 'S', to: () => href.settings(), current: (r) => r.page === 'settings' },
      { key: 'status', label: 'Status', mark: 'D', to: () => href.status(), current: (r) => r.page === 'status' },
    ],
  },
]

/** The count beside each entry, all read from the one attention list. */
export function navCounts(w: Waiting | null): Partial<Record<NavKey, number>> {
  return { inbox: attentionCount(w), next: attentionOf(w, 'next').length, board: 0 }
}

/** What a count means, for the badge's accessible name. */
export const COUNT_LABEL: Partial<Record<NavKey, string>> = {
  inbox: 'waiting on you',
  next: 'waiting on your approval',
  board: 'lanes broken',
}

export function currentItem(r: Route): NavItem | undefined {
  return NAV.flatMap((g) => g.items).find((i) => i.current(r))
}
