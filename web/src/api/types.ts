// The daemon's JSON, from internal/api and internal/store. Times are RFC 3339 strings.
// Fields Go marks omitempty are optional here: the daemon leaves them out, not null.

export type LaneState = 'opening' | 'open' | 'closed'
export type RunState = 'running' | 'succeeded' | 'failed' | 'stopped' | 'interrupted'
export type DecisionState = 'open' | 'answered'
export type MRState = 'open' | 'merged' | 'closed' | 'unknown'
export type PipelineStatus = 'pending' | 'running' | 'passed' | 'failed' | 'canceled' | 'skipped' | 'unknown'

export interface Workspace {
  id: number
  name: string
  path: string
  created: string
}

export interface WorkspaceSummary extends Workspace {
  repos: number
  lanes: number
}

export interface Status {
  version: string
  pid: number
  started: string
  store: string
  config: string
  workspaces: WorkspaceSummary[]
  /** Only from GET /v1/status?detail=1. */
  detail?: StatusDetail
}

/** What the daemon popover shows; fields the daemon does not report are absent. */
export interface StatusDetail {
  commit?: string
  built_at?: string
  os: string
  arch: string
  host?: string
  /** The daemon's clock. */
  now: string
  config_loaded: string
  reload_error?: string
  reload_failed_at?: string
  running: number
  open_lanes: number
  decisions: number
  /** Absent when this daemon does not report quota holds. */
  quota_holds?: { agent: string; until: string }[]
  spend_usd: number
  budget_usd?: number
  forge?: { last_poll?: string; error?: string }
}

/** GET /v1/web/session: this browser's session. */
export interface WebSession {
  role: string
  expires?: string
  allow_settings?: boolean
}

export interface Origin {
  via?: string
  agent?: string
  session?: string
  run?: number
  pid?: number
  dir?: string
  detail?: string
}

export interface Lane {
  id: number
  repo_id: number
  name: string
  branch: string
  base: string
  worktree: string
  scope: string[]
  state: LaneState
  created: string
  closed?: string
  origin: Origin
}

/** Where a lane's last activity came from (internal/api Activity*, internal/store Activity*). */
export type ActivityKind = 'run' | 'decision' | 'request' | 'commit' | 'mr' | 'pipeline' | 'created'

export type GateState = 'passed' | 'failed' | 'stale' | 'not_run'

/** Whether Shepherd's own gate ran on a commit; mirrors store.GateBadge. */
export interface GateBadge {
  state: GateState
  /** The commit the newest record is for. */
  sha?: string
  at?: string
  command?: string
  /** `ship`: Shepherd's own run before a push. */
  source?: string
}

/** A lane with its repo's name and the forge's last word on its branch. */
export interface LaneView extends Lane {
  repo: string
  /** The gate record against the branch tip; unset from a daemon that predates it. */
  gate?: GateBadge
  mr?: number
  mr_state?: MRState
  mr_url?: string
  pipeline?: number
  pipeline_status?: PipelineStatus
  /** The open merge request has an approval, as of the last forge poll. */
  approved?: boolean
  /** The status of the head pipeline's require-approval job; absent when it has none. */
  require_approval?: string
  /**
   * The latest of the lane's run, commit, decision, request, merge request and pipeline
   * times, with its creation as the floor; last_activity_by says which. The forge's times
   * are as of the daemon's last poll.
   */
  last_activity?: string
  last_activity_by?: ActivityKind
  /** The forge's times for the merge request and its head pipeline; each unset when unknown. */
  mr_created?: string
  mr_updated?: string
  pipeline_started?: string
  /** Unset while the pipeline runs. */
  pipeline_finished?: string
  /** The newest diagnosis of the lane's latest run. */
  diagnosis?: Diagnosis
  /** What the lane's latest run is doing now, while it runs. */
  progress?: Progress
}

/** One worktree's verdict from the daemon's review (internal/fold Review). */
export interface LaneReview {
  branch: string
  worktree: string
  lane?: string
  verdict: 'finished' | 'live' | 'unclear'
  why: string
  evidence?: string[]
  unlanded?: string[]
  /** Clean, and every commit's change is in the base: what lets Shepherd close it by itself. */
  in_base?: boolean
}

/** A lane's merge request as the approval control shows it (internal/daemon approve.go). */
export interface LaneApproval {
  /** False when the lane's forge cannot approve from here; `reason` says why. */
  supported: boolean
  reason?: string
  /** 0 or absent: the lane has no merge request. */
  mr?: number
  mr_url?: string
  mr_state?: string
  /** The merge request's head now: what an approval is for. */
  sha?: string
  approved: boolean
  approved_by?: string[]
  pipeline?: number
  pipeline_status?: string
  pipeline_url?: string
  /** The status of the pipeline's require-approval job; absent when it has none. */
  require_approval?: string
}

/** What an approval did: `retried` says the require-approval job was started again. */
export interface LaneApproved extends LaneApproval {
  retried: boolean
  retry_note?: string
}

/** What closing a lane did (internal/fold CloseResult). */
export interface LaneClosed {
  lane: Lane
  branch_deleted: boolean
  notes?: string[]
}

export interface Run {
  id: number
  lane_id: number
  agent: string
  model?: string
  prompt: string
  state: RunState
  pid?: number
  log: string
  start_sha: string
  end_sha?: string
  commits: number
  outside?: string[]
  exit_code?: number
  error?: string
  started: string
  ended?: string
  session?: string
  parent?: number
  cost_usd?: number
  credits?: number
  /** cost_usd is the agent's own estimate, not a bill (a hosted OpenCode model). */
  cost_estimated?: boolean
  session_usd?: number
  session_credits?: number
  /** When the person dismissed this failed or stopped run from the board; the run stays in history. */
  dismissed?: string
}

/** One suggested action of a diagnosis: a button a client may offer. */
export interface Fix {
  id: string
  label: string
  /** continue, retry, retry_other, wait_quota, ship, widen_scope, raise_budget, open_mr, fix_config_text or open_log. */
  op: string
  args?: Record<string, string>
  /** Only the person chooses it: it widens what an agent may touch, spends beyond a cap or changes rules. */
  needs_person: boolean
  desk_ok: boolean
}

/** Why a run, or the push of its work, went wrong; the output is redacted. */
export interface Diagnosis {
  id: number
  run_id: number
  kind: string
  summary: string
  evidence?: string
  /** A list leaves the evidence out; GET /v1/runs/{id}/diagnoses or one run's view has it. */
  evidence_omitted?: boolean
  command?: string
  exit_code?: number
  files?: string[]
  log?: string
  reset_at?: string
  fixes?: Fix[]
  created: string
}

/** What a running run is doing now, worked out by the daemon from its log and commits. */
export interface Progress {
  /** The whole phrase: "running make check (3 commits, 12m)". */
  line: string
  /** The part from the log; absent when the log shows nothing yet. */
  step?: string
  commits: number
  elapsed_seconds: number
}

/** What taking a fix did. */
export interface FixResult {
  fix: Fix
  run?: Run
  message?: string
  done: boolean
}

export interface RunView extends Run {
  lane: string
  repo: string
  worktree: string
  /** The run's newest diagnosis. */
  diagnosis?: Diagnosis
  /** What the run is doing now; only while it runs. */
  progress?: Progress
}

export interface RunLog {
  data: string
  offset: number
  /** The run has ended and data reaches the end of its log. */
  done: boolean
}

export interface Decision {
  id: number
  run_id: number
  question: string
  options?: string[]
  recommendation?: string
  why?: string
  state: DecisionState
  answer?: string
  answer_run?: number
  created: string
  answered?: string
}

export interface DecisionView extends Decision {
  agent: string
  lane: string
  repo: string
}

export interface RunEvent {
  id: number
  run_id: number
  kind: string
  status?: string
  text: string
  created: string
}

export interface RunEvents {
  events: RunEvent[]
  decisions: Decision[]
}

export interface Request {
  id: number
  from_run: number
  kind: string
  lane?: string
  message: string
  state: string
  agent?: string
  target_run?: number
  reply?: string
  reply_run?: number
  depth: number
  note?: string
  created: string
  updated: string
}

export interface RequestView extends Request {
  from_agent: string
  from_lane: string
  repo: string
}

export interface FeedItem {
  id: number
  /** The store's raw kind; it may grow. Draw from the parallel event instead. */
  kind: string
  text: string
  /** The run, lane, decision or request the item is about, depending on its kind. */
  ref?: number
  created: string
}

export interface Feed {
  items: FeedItem[]
  /** Parallel to items: the event of each, from a closed set. Older daemons omit it. */
  events?: string[]
  last: number
}

export interface Spend {
  day: string
  source: string
  ref?: number
  usd: number
  credits?: number
}

export interface SpendToday {
  day: string
  usd: number
  budget: number
  credit_usd: number
  by_source: Record<string, Spend> | null
}

/** The front desk's stored event kinds (store.Desk*); a newer daemon may add more. */
export type DeskKind = 'user' | 'system' | 'turn_start' | 'assistant' | 'tool' | 'cost' | 'error' | 'turn_end' | 'new'

/** One entry of the desk conversation. A streamed "partial" piece has no seq and is never stored. */
export interface DeskEvent {
  seq: number
  workspace_id: number
  kind: string
  text?: string
  /** The seq of the message that started the turn this belongs to. */
  turn?: number
  /** Who started the turn: "human" or "system". */
  origin?: string
  created: string
}

export interface DeskHistory {
  /** Oldest first. */
  events: DeskEvent[]
  /** Older events remain: ask again with before set to the first event's seq. */
  more: boolean
}

export interface DeskStatus {
  workspace_id: number
  busy: boolean
  queued: number
  attached: number
  session: string
  model: string
  wake: string
}

/** The entity a desk turn is about (internal/api DeskAbout). */
export interface DeskAbout {
  kind: string
  ids: number[]
  label?: string
}

export interface DeskTurnAccepted {
  workspace_id: number
  turn: number
  /** How many turns run before this one, the one in progress included. */
  ahead: number
}

export interface ApiError {
  error: string
}

/** How a closed lane ended: what the forge last said about its merge request. */
export type LaneOutcome = 'merged' | 'mr_closed' | 'dropped' | 'no_mr'

/** A lane in the history (GET /v1/history/lanes): its runs' count and cost, and how a closed one ended. */
export interface LaneRecord extends LaneView {
  outcome?: LaneOutcome
  runs: number
  cost_usd: number
  credits?: number
}

/** A run in the history, without its prompt; task is the prompt's first line. */
export interface RunRecord {
  id: number
  lane_id: number
  repo: string
  lane: string
  lane_state: LaneState
  agent: string
  model?: string
  state: RunState
  commits: number
  cost_usd?: number
  credits?: number
  cost_estimated?: boolean
  started: string
  ended?: string
  task: string
}

/** GET /v1/history/runs: count and cost cover every matching run, runs may be cut at the limit. */
export interface RunHistory {
  runs: RunRecord[]
  count: number
  cost_usd: number
  credits: number
  /** The part of cost_usd that is an agent's estimate, not a bill. */
  estimated_usd?: number
  /** What the range holds before the agent and lane filters, to offer as choices. */
  agents: string[]
  lanes: { id: number; repo: string; name: string }[]
}

/** What a set of usage records add up to (store.UsageTotals). Tokens, peak and over_200k cover the measured records only. */
export interface UsageTotals {
  count: number
  measured: number
  input_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  output_tokens: number
  /** The largest single request's prompt of any record. */
  peak_context: number
  /** Records whose peak went past 200,000 tokens. */
  over_200k: number
  /** The largest window reported in the set; absent when none said. */
  context_window?: number
  compactions: number
  cost_usd: number
  credits?: number
  /** The part of cost_usd that is an agent's estimate, not a bill. */
  estimated_usd?: number
}

/** One agent and model, or one lane. */
export interface UsageGroup extends UsageTotals {
  key: string
  agent?: string
  model?: string
  repo?: string
  lane?: string
}

/** GET /v1/usage: a range of local days, inclusive. */
export interface UsageStats {
  from: string
  to: string
  total: UsageTotals
  runs: UsageTotals
  desk: UsageTotals
  by_model?: UsageGroup[]
  by_lane?: UsageGroup[]
  desk_by_model?: UsageGroup[]
}

// The config editor (api/config.go).

/** One change: set `path` (keys from the file's top) to `value`, or `unset` it to inherit again. */
export interface ConfigOp {
  path: string[]
  value?: unknown
  unset?: boolean
}

export interface ConfigChange {
  etag: string
  ops: ConfigOp[]
  confirm?: string[]
}

/** A change that gives agents more power or removes a guard; a save needs its path confirmed. */
export interface ConfigRisk {
  path: string
  from: string
  to: string
  why: string
}

export interface ConfigPreview {
  etag: string
  diff: string
  errors?: string[]
  risky?: ConfigRisk[]
}

/** The body of a 409, 422 or 428 from a save. */
export interface ConfigRefusal {
  error: string
  problems?: string[]
  risky?: ConfigRisk[]
}

export interface ConfigSaved {
  etag: string
  diff: string
  reloaded: boolean
  reload_error?: string
}

export interface ConfigProfile {
  raw: Record<string, unknown>
  effective: Record<string, unknown>
  overridden: string[]
}

export type ConfigFieldKind = 'string' | 'text' | 'number' | 'bool' | 'enum' | 'list' | 'map' | 'json'

export interface ConfigField {
  key: string[]
  kind: ConfigFieldKind
  label: string
  help?: string
  group?: string
  choices?: string[]
  nullable?: boolean
  risk?: boolean
  restart?: boolean
}

export interface ConfigSection {
  id: string
  label: string
  root: string[]
  many?: boolean
  inherits?: string
  fields: ConfigField[]
}

export interface ConfigDoc {
  path: string
  etag: string
  missing?: boolean
  can_write: boolean
  why?: string
  invalid?: string[]
  raw: Record<string, unknown>
  effective: Record<string, unknown>
  defaults: ConfigProfile
  repos: Record<string, ConfigProfile>
  projects: Record<string, { raw: Record<string, unknown>; effective: Record<string, unknown> }>
  sections: ConfigSection[]
}

/** A project's brief text with its author and approval; GET /v1/projects/{name}. */
export interface ProjectBrief {
  id: number
  project: string
  text: string
  state: 'draft' | 'approved' | 'superseded' | string
  drafted_by: string
  approved_by?: string
  created: string
  approved?: string
}

export interface BriefView {
  project: string
  approved?: ProjectBrief
  /** Nanoseconds as a Go duration. */
  age?: number
  age_text?: string
  stale?: boolean
  max_age: number
  pending?: ProjectBrief
  caches?: { path: string; repo: string; recipe: string; note?: string }[]
}

export interface ProjectBudget {
  /** Dollars per period; 0 is no cap. */
  amount: number
  cap: 'soft' | 'hard' | string
  period: 'day' | 'month' | string
  since: string
  spent: number
}

/** A project, or the Unassigned group (unassigned, no name, no budget). */
export interface ProjectSummary {
  name: string
  unassigned?: boolean
  repos: string[]
  lanes: number
  running: number
  decisions: number
  requests: number
  waiting: number
  spend_today: number
  budget?: ProjectBudget
}

export interface ProjectList {
  projects: ProjectSummary[]
}

export interface ProjectDetail extends ProjectSummary {
  open_lanes: LaneView[]
  runs: RunView[]
  pending_decisions: DecisionView[]
  pending_requests: RequestView[]
  brief?: BriefView
}

/** A work matrix row's status: the order here is the order the page shows them in. */
export type WorkStatus = 'doing' | 'blocked' | 'todo' | 'done' | 'dropped'

export interface WorkRow {
  id: string
  area: string
  item: string
  status: WorkStatus
  lane?: string
  /** 0 or absent when the PR cell was `-`. */
  pr?: number
  notes?: string
  repo: string
  /** The id of the row's lane when the repo has one by that name. */
  lane_id?: number
  /** The forge's page for the PR or MR; absent when the remote is unknown. */
  pr_url?: string
}

export interface WorkWarning {
  line?: number
  text?: string
  reason: string
}

export interface RepoWork {
  repo: string
  /** False when the base ref or docs/WORK.md is not there. */
  found: boolean
  ref?: string
  blob?: string
  rows: WorkRow[]
  warnings: WorkWarning[]
}

export type WorkDriftKind = 'lane_untracked' | 'doing_lane_closed' | 'doing_lane_unknown' | 'no_work_file'

export interface WorkDrift {
  kind: WorkDriftKind
  repo: string
  lane?: string
  lane_id?: number
  row_id?: string
  message: string
}

/** GET /v1/projects/{name}/work. */
export interface ProjectWork {
  project: string
  repos: RepoWork[]
  drift: WorkDrift[]
}

/** One changed file in a lane diff; mirrors fold.DiffFileInfo. */
export interface DiffFile {
  path: string
  old_path?: string
  status: 'added' | 'modified' | 'deleted' | 'renamed' | 'copied' | 'typechange' | 'untracked' | string
  added: number
  removed: number
  binary?: boolean
  size?: number
  in_scope: boolean
  hunks?: string[]
}

export interface DiffCommit {
  sha: string
  subject: string
  author: string
  date: string
  files: number
}

/** What a lane holds against its base, at one moment; GET /v1/lanes/{id}/diff. */
export interface LaneDiff {
  lane_id: number
  lane: string
  repo?: string
  branch: string
  base: string
  base_ref: string
  merge_base?: string
  head: string
  /** The gate record against the lane's HEAD. */
  gate?: GateBadge
  mode: string
  read_at: string
  commits: DiffCommit[]
  commits_total: number
  committed: DiffFile[]
  uncommitted: DiffFile[]
  untracked: DiffFile[]
  files_more?: number
  out_of_scope: number
  file?: string
  patch?: string
  truncated?: boolean
  notes?: string[]
  redacted?: number
}

/** What a next-up item is: started and done are derived from its lane, the rest are chosen. */
export type NextState = 'planned' | 'approved' | 'deferred' | 'started' | 'done' | 'dropped'

/** The states the person can set; GET /v1/next never reports `started` as chosen. */
export type NextChosen = Exclude<NextState, 'started'>

/** An item of the next-up list; GET /v1/next. */
export interface NextItem {
  id: number
  title: string
  repo?: string
  /** A path under the repo to the doc that plans it. */
  plan?: string
  /** The name a lane opened for it is expected to have. */
  lane?: string
  /** The effective state: started and done come from the lane. */
  state: NextState
  stored_state: NextChosen
  rank: number
  source: string
  /** Added by an agent or the desk and not yet approved by the person. */
  proposed?: boolean
  note?: string
  lane_id?: number
  lane_name?: string
  lane_state?: string
  lane_repo?: string
  mr?: string
  mr_url?: string
  mr_state?: MRState
  running?: boolean
  /** Why an item whose lane closed is not started. */
  why?: string
  created: string
  updated: string
  done_at?: string
  /** Set on a work-matrix row (source "matrix"): read through from docs/WORK.md, id 0, read-only. */
  key?: string
  read_only?: boolean
  area?: string
  row_id?: string
  pr_url?: string
}

/** A work-matrix row as the list shows it: planned for todo, blocked for blocked; never changed here. */
export type MatrixRow = Omit<NextItem, 'state' | 'stored_state'> & { state: 'planned' | 'blocked'; stored_state: 'planned' | 'blocked' }

/** The list in order: approved, planned, deferred, then dropped and done when asked for. */
export interface NextList {
  items: NextItem[]
  /** The first approved item with no lane; unset when none. */
  next_up?: number
  /** Rows of the repos' work matrices with status todo or blocked, read through and not stored. */
  matrix?: MatrixRow[]
}

/** The body of POST /v1/next. */
export interface NextAddBody {
  title: string
  repo?: string
  plan?: string
  lane?: string
  note?: string
  /** planned unless the person says approved. */
  state?: 'planned' | 'approved'
}

/** The body of PATCH /v1/next/{id}: only what changes. */
export interface NextUpdateBody {
  state?: NextChosen
  /** A lane's id to link; 0 unlinks. */
  lane_id?: number
  move?: 'top' | 'up' | 'down'
}

/** The daemon's cached release check, GET /v1/update (internal/api/update.go). */
export interface UpdateStatus {
  enabled: boolean
  channel: string
  /** This build's version; `current_known` is false for a dev build, which is not compared. */
  current: string
  current_known: boolean
  latest?: string
  url?: string
  /** The release's changelog section, as published (markdown). */
  notes?: string
  available: boolean
  dismissed: boolean
  /** What the notice should do: shown when true. The daemon decides; the page uses it as is. */
  show: boolean
  checked_at?: string
  error?: string
  /** The upgrade step; absent from a daemon too old to have it. */
  upgrade?: UpgradeStatus
}

/** Where an upgrade stands and whether the daemon can start one (internal/api/update.go). */
export interface UpgradeStatus {
  state: 'idle' | 'installing' | 'installed' | 'restarting' | 'failed' | 'rolled_back'
  /** How this binary was installed: binary, brew, go, source or readonly. */
  method: string
  can_upgrade: boolean
  /** What to run instead when the daemon will not replace this install itself. */
  hint?: string
  target?: string
  /** Agents a restart now would interrupt. */
  active_runs: number
  error?: string
  failed_version?: string
  /** update.auto is on. */
  auto: boolean
}

/** One thing that needs the person (internal/api WaitingItem). */
export interface WaitingItem {
  kind: 'decision' | 'mr' | 'ship' | 'next' | 'question' | 'blocked' | 'scope' | 'request'
  /** answer, approve, ship, review, reply, route or open: what the person can do from here. */
  action: 'answer' | 'approve' | 'ship' | 'review' | 'reply' | 'route' | 'open'
  repo?: string
  lane?: string
  lane_id?: number
  decision_id?: number
  next_id?: number
  /** A question's id, and the desk turn it was asked in (the thread message to scroll to). */
  question_id?: number
  turn?: number
  /** A request that needs routing. */
  request_id?: number
  mr?: number
  title: string
  detail?: string
  since?: string
}

/** GET /v1/waiting: everything that waits on the person's action, oldest first. */
export interface Waiting {
  count: number
  counts: { decisions: number; mrs: number; ship: number; next: number; questions?: number; blocked?: number; scope?: number; requests?: number }
  items: WaitingItem[]
  /** Moves whenever the list changes: equal versions mean an equal list. */
  version?: string
}

/** A question the front desk asked the person (internal/store Question). */
export interface DeskQuestion {
  id: number
  workspace_id: number
  /** The desk turn it was asked in. */
  turn: number
  topic_id?: number
  text: string
  state: 'open' | 'answered' | 'skipped'
  answered_turn?: number
  created: string
  closed?: string
}

/** What an explicit lane ship did (POST /v1/lanes/{id}/ship). */
export interface LaneShipped {
  lane: string
  commits: number
  pushed: boolean
  mr?: number
  url?: string
  message: string
}
