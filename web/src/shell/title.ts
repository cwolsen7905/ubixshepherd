import type { Route } from '../router'
import { currentItem } from './nav'

/** What the page bar names the route: the page's own subject where it has one, else its nav entry. */
export function pageTitle(route: Route, lanes: readonly { id: number; name: string }[] = []): string {
  switch (route.page) {
    case 'lane':
      return lanes.find((l) => l.id === route.id)?.name ?? `Lane ${route.id}`
    case 'log':
      return `Run ${route.run} log`
    case 'project':
      return route.name
    case 'unassigned':
      return 'Unassigned'
    case 'status':
      return 'Status'
    case 'kit':
      return 'Kit'
    case 'missing':
      return 'Not found'
    default:
      return currentItem(route)?.label ?? ''
  }
}
