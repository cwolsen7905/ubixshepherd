// The daemon's HTTP API, by relative URL so the app works unchanged wherever the daemon
// serves it. Authentication is the server's business: the dev proxy adds the token, and
// when the daemon serves the app the browser holds a session cookie (from the link
// `shepherd web` opens). Nothing here ever holds a token. A cookie session's writes carry
// its CSRF token, read once from /v1/web/session.
import type { TopicList, TopicUpdateBody, TopicView } from '../model/topic'
import type {
  ConfigChange, ConfigDoc, NextAddBody, NextItem, NextList, NextUpdateBody, ConfigPreview, ConfigSaved, Decision, DecisionView, Diagnosis, Feed, FixResult, LaneApproval, LaneApproved, LaneClosed, LaneDiff, LaneRecord, LaneReview, LaneView, ProjectDetail, ProjectList, ProjectWork, RequestView, RunEvents, RunHistory, RunLog, RunView, SpendToday, Status, UpdateStatus, UsageStats, WebSession,
} from './types'

export class ApiError extends Error {
  readonly status: number
  /** The error response's whole body, for the ones that carry more than a line (config refusals). */
  readonly body: unknown
  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.status = status
    this.body = body
  }
}

export const CSRF_HEADER = 'X-Shepherd-CSRF'

/** What to do about a signed-out session; a daemon restart ends every browser session. */
export const SIGN_IN_HINT = 'Run `shepherd web` in a terminal to open a new sign-in link.'

/** How long a request may take before it counts as lost: a hung daemon is an offline one. */
export const REQUEST_TIMEOUT = 6000

/** The daemon did not answer in time, or could not be reached at all. */
export class NetworkError extends Error {}

let csrf: Promise<string> | null = null

/** The session's CSRF token: "" through the dev proxy, which uses a token instead. */
function csrfToken(): Promise<string> {
  csrf ??= call<{ csrf?: string }>('/v1/web/session').then(
    (s) => s.csrf ?? '',
    () => {
      csrf = null
      return ''
    },
  )
  return csrf
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const write = init?.method !== undefined && init.method !== 'GET'
  const token = write ? await csrfToken() : ''
  // The timer covers the body too: a daemon that sends headers and stalls is as lost.
  const ctl = new AbortController()
  let timedOut = false
  const timer = setTimeout(() => {
    timedOut = true
    ctl.abort()
  }, REQUEST_TIMEOUT)
  try {
    return await exchange<T>(path, init, token, ctl.signal)
  } catch (e) {
    if (e instanceof ApiError) throw e
    throw new NetworkError(timedOut ? 'the daemon did not answer in time' : `the daemon could not be reached (${e instanceof Error ? e.message : String(e)})`)
  } finally {
    clearTimeout(timer)
  }
}

async function exchange<T>(path: string, init: RequestInit | undefined, token: string, signal: AbortSignal): Promise<T> {
  const res = await fetch(path, {
    ...init,
    signal,
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { [CSRF_HEADER]: token } : {}),
    },
  })
  // A session that ended (signed out, expired, a daemon restart) has a new token next time.
  if (res.status === 401 || res.status === 403) csrf = null
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`
    let parsed: unknown
    try {
      const body = (await res.json()) as { error?: string }
      parsed = body
      if (body.error) msg = body.error
    } catch {
      // not JSON: keep the status line
    }
    throw new ApiError(res.status, msg, parsed)
  }
  return (await res.json()) as T
}

export const get = <T>(path: string) => call<T>(path)
export const post = <T>(path: string, body: unknown) => call<T>(path, { method: 'POST', body: JSON.stringify(body) })
export const put = <T>(path: string, body: unknown) => call<T>(path, { method: 'PUT', body: JSON.stringify(body) })
export const patch = <T>(path: string, body: unknown) => call<T>(path, { method: 'PATCH', body: JSON.stringify(body) })
export const qs = (params: Record<string, string | number>) => new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)])).toString()

/** How many runs the board reads: enough to know every open lane's latest run. */
export const RECENT_RUNS = 200

export const api = {
  status: () => get<Status>('/v1/status'),
  lanes: (workspaceId: number) => get<LaneView[]>(`/v1/lanes?${qs({ workspace_id: workspaceId })}`),
  runs: (laneId = 0, limit = RECENT_RUNS) => get<RunView[]>(`/v1/runs?${qs({ lane_id: laneId, limit })}`),
  run: (id: number) => get<RunView>(`/v1/runs/${id}`),
  /** Clears a failed or stopped run from the board's attention group; nothing is deleted. */
  dismissRun: (id: number) => post<RunView>(`/v1/runs/${id}/dismiss`, {}),
  undismissRun: (id: number) => post<RunView>(`/v1/runs/${id}/undismiss`, {}),
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

/** What a lane holds against its base (read-only; each read runs git in the worktree). */
export const laneChanges = {
  /** The file list; with `file`, that file's patch; with `run`, narrowed to that run's range. */
  diff: (laneId: number, file?: string, run?: number) =>
    get<LaneDiff>(`/v1/lanes/${laneId}/diff?${qs({ ...(file === undefined ? { mode: 'stat' } : { file }), ...(run ? { run } : {}) })}`),
}

/** Closing a lane, and judging whether its work is done. */
export const laneClose = {
  /**
   * Closes the lane through the daemon's own checks. Without `force` it refuses uncommitted
   * changes and a branch git cannot see merged (an ApiError with status 409 and the reason);
   * with it, the worktree goes whatever it holds.
   */
  close: (laneId: number, force: boolean) => post<LaneClosed>(`/v1/lanes/${laneId}/close`, { force }),
  /** Every worktree of the repo, judged finished, live or unclear. The daemon refreshes the remote first. */
  review: (repoId: number) => post<LaneReview[]>('/v1/fold/review', { repo_id: repoId, apply: false }),
}

/** A lane's merge request approval: read, and given by the person's own click only. */
export const laneApprove = {
  read: (laneId: number) => get<LaneApproval>(`/v1/lanes/${laneId}/approval`),
  /** `sha` is the head the person was looking at; a moved head is refused with a 409. */
  approve: (laneId: number, sha: string) => post<LaneApproved>(`/v1/lanes/${laneId}/approve`, { sha }),
}

/** A failed run's diagnoses and its suggested fixes. */
export const runFailures = {
  /** Newest first, evidence included. */
  diagnoses: (runId: number) => get<Diagnosis[]>(`/v1/runs/${runId}/diagnoses`),
  /** Takes one of the fixes; the person's click only. */
  fix: (runId: number, action: string, overrides?: Record<string, string>) =>
    post<FixResult>(`/v1/runs/${runId}/fix`, { action, ...(overrides ? { overrides } : {}) }),
}

/** What the daemon popover reads, outside `api` so the poller's fakes need not carry it. */
export const daemonInfo = {
  /** The status with its detail. */
  status: () => get<Status>('/v1/status?detail=1'),
  /** This browser's session. */
  session: () => get<WebSession>('/v1/web/session'),
}

/** The daemon's release check: a cached answer, never a call to GitHub from the browser. */
export const updateCheck = {
  status: () => get<UpdateStatus>('/v1/update'),
  /** Hides the notice for `version` (the one offered) until a newer release appears. */
  dismiss: (version: string) => post<UpdateStatus>('/v1/update/dismiss', { version }),
}

/** What ran and how it ended, closed lanes included. `since` is an RFC 3339 time. */
export const history = {
  /** Closed lanes by default, newest closed first; one lane by id whatever its state. */
  lanes: (workspaceId: number, since: string, state: 'closed' | 'open' | 'all' = 'closed') =>
    get<LaneRecord[]>(`/v1/history/lanes?${qs({ workspace_id: workspaceId, state, since })}`),
  lane: (laneId: number) => get<LaneRecord[]>(`/v1/history/lanes?${qs({ lane_id: laneId })}`),
  /** `limit` is how many runs are listed (the daemon's default is 500, its most 1000); the totals cover them all. */
  runs: (workspaceId: number, since: string, agent = '', laneId = 0, limit = 0) =>
    get<RunHistory>(
      `/v1/history/runs?${qs({
        workspace_id: workspaceId, since, ...(agent ? { agent } : {}), ...(laneId ? { lane_id: laneId } : {}),
        ...(limit ? { limit } : {}),
      })}`,
    ),
}

/** Projects with their lanes, runs, spend and brief; read-only. */
export const projects = {
  list: () => get<ProjectList>('/v1/projects'),
  one: (name: string) => get<ProjectDetail>(`/v1/projects/${encodeURIComponent(name)}`),
  /** The repos that are in no project, as a project's detail. */
  unassigned: () => get<ProjectDetail>('/v1/unassigned'),
  /** A project's work matrices and drift; name null is the Unassigned group. */
  work: (name: string | null) =>
    get<ProjectWork>(name === null ? '/v1/unassigned/work' : `/v1/projects/${encodeURIComponent(name)}/work`),
}

/** Tokens and context sizes of the daemon's runs and desk turns over a range of local days ("2006-01-02", inclusive). */
export const usage = (from: string, to: string) => get<UsageStats>(`/v1/usage?${qs({ from, to })}`)

/** The config editor: read the settings, preview a change, save it. */
export const config = {
  get: () => get<ConfigDoc>('/v1/config'),
  preview: (change: ConfigChange) => post<ConfigPreview>('/v1/config/preview', change),
  save: (change: ConfigChange) => put<ConfigSaved>('/v1/config', change),
}

/** Conversation topics: the matters the thread is about. Renaming, merging and clearing are the person's click only. */
export const topics = {
  /** all adds cleared topics. */
  list: (all = false) => get<TopicList>(all ? '/v1/topics?all=1' : '/v1/topics'),
  add: (title: string) => post<TopicView>('/v1/topics', { title }),
  update: (id: number, body: TopicUpdateBody) => patch<TopicView>(`/v1/topics/${id}`, body),
}

/** The next-up list: work planned but not being acted on. Changing a state or the order is the person's click only. */
export const nextUp = {
  /** all adds dropped and done items. */
  list: (all = false) => get<NextList>(all ? '/v1/next?all=1' : '/v1/next'),
  add: (body: NextAddBody) => post<NextItem>('/v1/next', body),
  update: (id: number, body: NextUpdateBody) => patch<NextItem>(`/v1/next/${id}`, body),
}
