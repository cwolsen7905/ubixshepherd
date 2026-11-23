import { href, type Route } from '../router'
import { attentionOf, inboxCount } from '../state/attention'
import type { Waiting } from '../api/types'
import type { IconName } from '../ui/icons'

export type NavKey = 'inbox' | 'chat' | 'board' | 'next' | 'artifacts' | 'runs' | 'timeline' | 'history' | 'files' | 'rules' | 'classifiers' | 'guardrails' | 'projects' | 'reviewbots' | 'usage' | 'settings' | 'status'

export interface NavItem {
  key: NavKey
  label: string
  /** The view's icon, in the sidebar and the palette. Icons name views; statuses keep their glyphs. */
  icon: IconName
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

/**
 * The sidebar's entries, in the plan's groups. Pages that do not exist yet are left out, except
 * Classifiers and Guardrails: they are deliberate placeholders, listed so their place is known.
 */
export const NAV: NavGroup[] = [
  {
    name: 'Primary',
    items: [
      { key: 'inbox', label: 'Inbox', icon: 'inbox', to: () => href.inbox(), current: (r) => r.page === 'inbox' },
      { key: 'chat', label: 'Conversation', icon: 'chat', to: () => href.chat(), current: (r) => r.page === 'chat' },
    ],
  },
  {
    name: 'Work',
    items: [
      { key: 'board', label: 'Board', icon: 'board', to: () => href.board(), current: (r) => r.page === 'board' || r.page === 'lane' },
      { key: 'next', label: 'Next up', icon: 'next', to: () => href.next(), current: (r) => r.page === 'next' },
      { key: 'artifacts', label: 'Artifacts', icon: 'artifacts', to: () => href.artifacts(), current: (r) => r.page === 'artifacts' },
      { key: 'files', label: 'Files', icon: 'files', to: () => href.files(), current: (r) => r.page === 'files' },
      { key: 'runs', label: 'Runs', icon: 'runs', to: () => href.runs(), current: (r) => r.page === 'runs' || r.page === 'log' },
      { key: 'timeline', label: 'Timeline', icon: 'timeline', to: () => href.timeline(), current: (r) => r.page === 'timeline' },
      { key: 'history', label: 'History', icon: 'history', to: () => href.history(), current: (r) => r.page === 'history' },
    ],
  },
  {
    name: 'Setup',
    items: [
      {
        key: 'projects',
        label: 'Projects',
        icon: 'projects',
        to: () => href.projects(),
        current: (r) => r.page === 'projects' || r.page === 'project' || r.page === 'unassigned',
      },
      { key: 'rules', label: 'Rules', icon: 'rules', to: () => href.rules(), current: (r) => r.page === 'rules' && !r.view },
      { key: 'classifiers', label: 'Classifiers', icon: 'classifiers', to: () => href.classifiers(), current: (r) => r.page === 'rules' && r.view === 'classifiers' },
      { key: 'guardrails', label: 'Guardrails', icon: 'guardrails', to: () => href.guardrails(), current: (r) => r.page === 'rules' && r.view === 'guardrails' },
      { key: 'reviewbots', label: 'Review bots', icon: 'reviewbots', to: () => href.reviewbots(), current: (r) => r.page === 'usage' && r.view === 'review-bots' },
      { key: 'usage', label: 'Usage', icon: 'usage', to: () => href.usage(), current: (r) => r.page === 'usage' && r.view !== 'review-bots' },
      { key: 'settings', label: 'Settings', icon: 'settings', to: () => href.settings(), current: (r) => r.page === 'settings' },
      { key: 'status', label: 'Status', icon: 'status', to: () => href.status(), current: (r) => r.page === 'status' },
    ],
  },
]

/** The count beside each entry, all read from the one attention list. */
export function navCounts(w: Waiting | null): Partial<Record<NavKey, number>> {
  return { inbox: inboxCount(w), next: attentionOf(w, 'next').length, board: 0 }
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
