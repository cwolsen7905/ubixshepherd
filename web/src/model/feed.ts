// What a feed item is about. An item's ref means a run, a lane, a decision or a request
// depending on its kind (see internal/dispatch, internal/watch, internal/daemon), and a
// few lane kinds carry a repo id instead. The feed has no lane id of its own yet, so a
// lane's timeline is worked out here from the lists the board already holds.
import type { Feed, FeedItem, RequestView, RunView } from '../api/types'
import { EVENTS, type EventKind } from './marks'

export interface FeedEntry extends FeedItem {
  event: EventKind
}

/** The event of item i: the daemon's, or mapped from the raw kind for an older daemon. */
export function feedEvent(f: Feed, i: number): EventKind {
  const ev = f.events?.[i]
  // An older daemon calls a kind it does not know info; use the kind if it is an event here.
  if (ev && ev !== 'info' && isEvent(ev)) return ev
  const kind = f.items[i]?.kind ?? 'info'
  const mapped = KIND_EVENTS[kind] ?? kind
  return isEvent(mapped) ? mapped : 'info'
}

export function entries(f: Feed): FeedEntry[] {
  return f.items.map((it, i) => ({ ...it, event: feedEvent(f, i) }))
}

function isEvent(s: string): s is EventKind {
  return (EVENTS as readonly string[]).includes(s)
}

// The store kinds whose event differs from the kind; mirrors api.eventKinds.
const KIND_EVENTS: Record<string, string> = {
  decision: 'decision_asked',
  request_routed: 'request',
  request_replied: 'request',
  request_needs_routing: 'request_attention',
  request_failed: 'request_attention',
  session: 'info',
}

const RUN_REF = new Set(['run_started', 'run_ended', 'run_passed', 'run_failed', 'run_interrupted', 'run_quota', 'commit', 'report'])
const LANE_REF = new Set(['lane_opened', 'lane_closed', 'gate', 'mr', 'pipeline', 'release'])
const DECISION_REF = new Set(['decision', 'decision_answered'])
const REQUEST_REF = new Set(['request', 'request_routed', 'request_needs_routing', 'request_replied', 'request_failed'])

export interface LaneKey {
  id: number
  name: string
  repo: string
}

export interface Refs {
  runs: RunView[]
  /** run id of each decision, from the decisions list. */
  decisionRuns: Map<number, number>
  requests: RequestView[]
}

/**
 * Whether a feed item is about a lane. Lane kinds can carry a repo id (fold import,
 * worktree retire), so a ref that matches the lane's id also has to name the lane.
 */
export function aboutLane(it: FeedItem, lane: LaneKey, refs: Refs): boolean {
  const ref = it.ref
  if (!ref) return false
  if (RUN_REF.has(it.kind)) return refs.runs.some((r) => r.id === ref && r.lane_id === lane.id)
  if (LANE_REF.has(it.kind)) return ref === lane.id && it.text.includes(lane.name)
  if (DECISION_REF.has(it.kind)) {
    const run = refs.decisionRuns.get(ref)
    return run !== undefined && refs.runs.some((r) => r.id === run && r.lane_id === lane.id)
  }
  if (REQUEST_REF.has(it.kind)) {
    const q = refs.requests.find((r) => r.id === ref)
    return !!q && q.repo === lane.repo && (q.from_lane === lane.name || q.lane === lane.name)
  }
  return false
}
