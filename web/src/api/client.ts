// The daemon's HTTP API, by relative URL so the app works unchanged wherever the daemon
// serves it. Authentication is the server's business: the dev proxy adds the token, and
// the daemon will when it serves the app. Nothing here ever holds a token.
import type {
  Decision, DecisionView, Feed, LaneView, RequestView, RunEvents, RunLog, RunView, SpendToday, Status,
} from './types'

export class ApiError extends Error {
  readonly status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { Accept: 'application/json', ...(init?.body ? { 'Content-Type': 'application/json' } : {}) },
  })
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) msg = body.error
    } catch {
      // not JSON: keep the status line
    }
    throw new ApiError(res.status, msg)
  }
  return (await res.json()) as T
}

const get = <T>(path: string) => call<T>(path)
const post = <T>(path: string, body: unknown) => call<T>(path, { method: 'POST', body: JSON.stringify(body) })
const qs = (params: Record<string, string | number>) => new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)])).toString()

/** How many runs the board reads: enough to know every open lane's latest run. */
export const RECENT_RUNS = 200

export const api = {
  status: () => get<Status>('/v1/status'),
  lanes: (workspaceId: number) => get<LaneView[]>(`/v1/lanes?${qs({ workspace_id: workspaceId })}`),
  runs: (laneId = 0, limit = RECENT_RUNS) => get<RunView[]>(`/v1/runs?${qs({ lane_id: laneId, limit })}`),
  run: (id: number) => get<RunView>(`/v1/runs/${id}`),
  runLog: (id: number, offset: number) => get<RunLog>(`/v1/runs/${id}/log?${qs({ offset })}`),
  runEvents: (id: number) => get<RunEvents>(`/v1/runs/${id}/events`),
  /** state "" lists every decision. */
  decisions: (state: '' | 'open' | 'answered') => get<DecisionView[]>(`/v1/decisions?${qs({ state })}`),
  requests: (states: string) => get<RequestView[]>(`/v1/requests?${qs({ state: states })}`),
  spend: () => get<SpendToday>('/v1/spend'),
  /** The newest feed id only. */
  feedLatest: () => get<Feed>('/v1/feed?after=latest'),
  feed: (after: number) => get<Feed>(`/v1/feed?${qs({ after })}`),
  /** The person's words, sent only from their own click. */
  answer: (decisionId: number, answer: string) => post<Decision>(`/v1/decisions/${decisionId}/answer`, { answer }),
}
