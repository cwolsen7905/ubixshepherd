import { href, type Route } from '../router'
import { attentionOf, inboxCount } from '../state/attention'
import type { Waiting } from '../api/types'

export type NavKey = 'decisions' | 'chat' | 'board' | 'next' | 'runs' | 'timeline' | 'projects' | 'usage' | 'settings'

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
      { key: 'decisions', label: 'Decisions', mark: 'D', to: () => href.decisions(), current: (r) => r.page === 'decisions' },
      { key: 'chat', label: 'Conversation', mark: 'C', to: () => href.chat(), current: (r) => r.page === 'chat' },
    ],
  },
  {
    name: 'Work',
    items: [
      { key: 'board', label: 'Board', mark: '◻', to: () => href.board(), current: (r) => r.page === 'board' || r.page === 'lane' },
      { key: 'next', label: 'Next up', mark: 'N', to: () => href.next(), current: (r) => r.page === 'next' },
      { key: 'runs', label: 'Runs', mark: 'R', to: () => href.runs(), current: (r) => r.page === 'runs' || r.page === 'log' },
      { key: 'timeline', label: 'Timeline', mark: 'T', to: () => href.timeline(), current: (r) => r.page === 'timeline' },
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
      { key: 'usage', label: 'Usage', mark: 'U', to: () => href.usage(), current: (r) => r.page === 'usage' },
      { key: 'settings', label: 'Settings', mark: 'S', to: () => href.settings(), current: (r) => r.page === 'settings' },
    ],
  },
]

/** The count beside each entry, all read from the one attention list. */
export function navCounts(w: Waiting | null): Partial<Record<NavKey, number>> {
  return { decisions: inboxCount(w), next: attentionOf(w, 'next').length, board: 0 }
}

/** What a count means, for the badge's accessible name. */
export const COUNT_LABEL: Partial<Record<NavKey, string>> = {
  decisions: 'waiting on you',
  next: 'waiting on your approval',
  board: 'lanes broken',
}

export function currentItem(r: Route): NavItem | undefined {
  return NAV.flatMap((g) => g.items).find((i) => i.current(r))
}
