// The stored artifacts (GET /v1/artifacts and friends). The lane's discovery read lives in
// `api/artifacts.ts`; these are the rows Shepherd keeps, with versions. Content is not JSON,
// so it is read here with fetch, as the lane's is.
import { ApiError, NetworkError, REQUEST_TIMEOUT, asList, get, patch, post, qs } from '../api/client'
import type { ArtifactKind, ArtifactText } from '../api/artifacts'
import { DeskThread } from '../state/desk'

export type Intent = 'commit' | 'keep' | 'scratch' | 'undecided'
export type ArtifactState = 'live' | 'kept' | 'committed' | 'discarded' | 'gone'

export interface ArtifactVersion {
  artifact_id: number
  sha256: string
  size: number
  /** 0: the change was seen with no run, so it was made outside one. */
  run_id: number
  seen: string
  snapshotted?: string
}

export interface ArtifactLink {
  artifact_id: number
  kind: string
  ref: string
  created: string
}

export interface StoredArtifact {
  id: number
  workspace_id: number
  repo_id: number
  lane_id: number
  run_id: number
  path: string
  title?: string
  kind: ArtifactKind
  source: string
  intent: Intent
  intent_by: string
  state: ArtifactState
  sha256?: string
  size: number
  pinned?: boolean
  created: string
  updated: string
  closed?: string
  versions?: ArtifactVersion[]
  links?: ArtifactLink[]
}

export interface ArtifactFilter {
  lane_id?: number
  run_id?: number
  topic_id?: number
  decision_id?: number
  state?: string
  kind?: string
  limit?: number
}

export const LIST_LIMIT = 500

export const storedArtifacts = {
  list: (f: ArtifactFilter = {}) => {
    const p: Record<string, string | number> = { limit: f.limit ?? LIST_LIMIT }
    for (const [k, v] of Object.entries(f)) if (v && k !== 'limit') p[k] = v
    return get<unknown>(`/v1/artifacts?${qs(p)}`).then((v) => asList<StoredArtifact>(v))
  },
  one: (id: number) => get<StoredArtifact>(`/v1/artifacts/${id}`),
  /** The person's own changes: intent, pin, title, or `move: 'keep'`. Web only. */
  patch: (id: number, body: { intent?: Intent; pinned?: boolean; title?: string; move?: 'keep' }) =>
    patch<StoredArtifact>(`/v1/artifacts/${id}`, body),
  /** Deletes the file and its snapshots: the person's call, after a confirmation. */
  discard: (id: number) => post<StoredArtifact>(`/v1/artifacts/${id}/discard`, {}),
}

/** `version` is a version's sha-256; empty reads the newest. */
export const storedContentPath = (id: number, version = '') =>
  `/v1/artifacts/${id}/content${version ? `?version=${encodeURIComponent(version)}` : ''}`

export async function storedText(id: number, version = ''): Promise<ArtifactText> {
  const ctl = new AbortController()
  const timer = setTimeout(() => ctl.abort(), REQUEST_TIMEOUT)
  try {
    const res = await fetch(storedContentPath(id, version), { signal: ctl.signal })
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
    return { text: await res.text(), redacted: Number(res.headers.get('X-Shepherd-Redacted') ?? 0) || 0 }
  } catch (e) {
    if (e instanceof ApiError) throw e
    throw new NetworkError(`the daemon could not be reached (${e instanceof Error ? e.message : String(e)})`)
  } finally {
    clearTimeout(timer)
  }
}

/** The words for an intent and state, as one short tag: "kept local", "commit", "undecided". */
export function intentTag(a: Pick<StoredArtifact, 'intent' | 'state'>): string {
  if (a.state === 'committed') return 'committed'
  if (a.state === 'discarded') return 'discarded'
  if (a.state === 'gone') return 'gone'
  if (a.intent === 'keep') return 'kept local'
  return a.intent
}

/** Tone for the intent tag, shared by the list and the page: keep is lichen, undecided is lantern, the rest plain. */
export function intentTone(a: Pick<StoredArtifact, 'intent' | 'state'>): 'local' | 'warn' | 'plain' {
  if (a.state === 'committed') return 'plain'
  if (a.intent === 'keep') return 'local'
  if (a.intent === 'undecided') return 'warn'
  return 'plain'
}

/**
 * Asks the front desk to have the lane's agent commit the file. Resolves once the desk took
 * the turn; the caller records the intent after, so a failed send never marks it.
 */
export async function askToCommit(a: Pick<StoredArtifact, 'id' | 'path' | 'workspace_id'>): Promise<void> {
  const thread = new DeskThread(a.workspace_id)
  const ok = await thread.send(`Commit ${a.path} in the lane's branch, in scope and under the repo's gate.`, undefined, undefined, {
    kind: 'artifact',
    ids: [a.id],
    label: a.path.slice(0, 200),
  })
  if (!ok) throw new Error(thread.getSnapshot().problem?.message ?? 'The front desk did not take that.')
}
