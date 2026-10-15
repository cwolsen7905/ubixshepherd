// A lane's artifacts: the list, and one file's content. The content endpoint answers with the
// file's own type, not JSON, so it is read here rather than through the JSON client; the
// daemon sends it with a sandbox policy and nosniff, and nothing here renders it as markup.
import { ApiError, REQUEST_TIMEOUT, NetworkError, asList, get } from './client'

export type ArtifactKind = 'markdown' | 'text' | 'code' | 'image' | 'other'

export interface ArtifactInfo {
  path: string
  kind: ArtifactKind
  source: string
  status: string
  size: number
  readable: boolean
  mime?: string
}

export interface LaneArtifacts {
  lane_id: number
  lane: string
  artifacts: ArtifactInfo[]
  globs: string[]
  truncated?: boolean
}

export interface ArtifactText {
  text: string
  /** How many spans the daemon masked as secrets. */
  redacted: number
}

export const laneArtifactsPath = (id: number) => `/v1/lanes/${id}/artifacts`
export const artifactContentPath = (id: number, rel: string) => `${laneArtifactsPath(id)}/content?path=${encodeURIComponent(rel)}`

export async function laneArtifacts(id: number): Promise<LaneArtifacts> {
  const v = await get<Partial<LaneArtifacts> | null>(laneArtifactsPath(id))
  return {
    lane_id: v?.lane_id ?? id,
    lane: v?.lane ?? '',
    artifacts: asList<ArtifactInfo>(v?.artifacts),
    globs: asList<string>(v?.globs),
    ...(v?.truncated ? { truncated: true } : {}),
  }
}

/** A text artifact's bytes as a string. */
export async function artifactText(id: number, rel: string): Promise<ArtifactText> {
  const ctl = new AbortController()
  const timer = setTimeout(() => ctl.abort(), REQUEST_TIMEOUT)
  try {
    const res = await fetch(artifactContentPath(id, rel), { signal: ctl.signal })
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
