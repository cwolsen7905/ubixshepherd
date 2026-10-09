import type {
  LaneView,
  RunView,
  DecisionView,
  RequestView,
  FeedItem,
  SpendToday,
  Status,
  LaneState,
  RunState,
  DecisionState
} from '../api/types';

export function lane(o?: Partial<LaneView>): LaneView {
  const base: LaneView = {
    id: 1,
    repo_id: 1,
    name: "feat/login",
    branch: "feat/login",
    base: "dev",
    worktree: "/tmp/acme-api",
    scope: ["src/auth/**"],
    state: "open" as LaneState,
    created: "2026-10-08T10:00:00Z",
    origin: { via: "cli" },
    repo: "acme-api",
  };
  return { ...base, ...o };
}

export function run(o?: Partial<RunView>): RunView {
  const base: RunView = {
    id: 10,
    lane_id: 1,
    agent: "claude",
    model: "claude-sonnet",
    prompt: "",
    state: "succeeded" as RunState,
    log: "",
    start_sha: "abc123",
    end_sha: "def456",
    commits: 1,
    started: "2026-10-08T10:00:00Z",
    ended: "2026-10-08T10:04:00Z",
    cost_usd: 0.42,
    lane: "feat/login",
    repo: "acme-api",
    worktree: "/tmp/acme-api",
  };
  return { ...base, ...o };
}

export function decision(o?: Partial<DecisionView>): DecisionView {
  const base: DecisionView = {
    id: 5,
    run_id: 10,
    question: "Should we use authentication?",
    options: ["Yes", "No"],
    recommendation: "Yes",
    why: "Because it's important for security",
    state: "open" as DecisionState,
    agent: "claude",
    lane: "feat/login",
    repo: "acme-api",
    created: "2026-10-08T10:00:00Z",
  };
  return { ...base, ...o };
}

export function request(o?: Partial<RequestView>): RequestView {
  const base: RequestView = {
    id: 3,
    from_run: 10,
    kind: "question",
    state: "needs_routing",
    message: "What should we do about authentication?",
    depth: 0,
    from_agent: "claude",
    from_lane: "feat/login",
    repo: "acme-api",
    created: "2026-10-08T10:00:00Z",
    updated: "2026-10-08T10:00:00Z",
  };
  return { ...base, ...o };
}

export function feedItem(o?: Partial<FeedItem>): FeedItem {
  const base: FeedItem = {
    id: 100,
    kind: "run_passed",
    text: "run 10: claude in lane feat/login succeeded, 1 commit(s)",
    ref: 10,
    created: "2026-10-08T10:00:00Z",
  };
  return { ...base, ...o };
}

export function spend(o?: Partial<SpendToday>): SpendToday {
  const base: SpendToday = {
    day: "2026-10-08",
    usd: 4.1,
    budget: 20,
    credit_usd: 0.04,
    by_source: {},
  };
  return { ...base, ...o };
}

export function status(o?: Partial<Status>): Status {
  const base: Status = {
    version: "0.0.0",
    pid: 1234,
    started: "2026-10-08T10:00:00Z",
    store: "/tmp/store",
    config: "/tmp/config",
    workspaces: [
      {
        id: 1,
        name: "workspace-1",
        path: "/tmp/workspace-1",
        created: "2026-10-08T10:00:00Z",
        repos: 1,
        lanes: 1,
      }
    ],
  };
  return { ...base, ...o };
}