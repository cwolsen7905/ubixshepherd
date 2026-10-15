import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, CSRF_HEADER, deskQuestions, history, NetworkError, nextUp, projects, REQUEST_TIMEOUT, topics, waitingOnYou } from './client'

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

afterEach(() => vi.unstubAllGlobals())

describe('the API client', () => {
  it("sends a cookie session's CSRF token on writes only", async () => {
    const calls: [string, RequestInit | undefined][] = []
    vi.stubGlobal('fetch', vi.fn(async (path: string, init?: RequestInit) => {
      calls.push([path, init])
      if (path === '/v1/web/session') return json({ role: 'web', csrf: 'tok' })
      return json({})
    }))
    await api.decisions('open')
    await api.answer(1, 'yes')
    await api.answer(2, 'no')
    const header = (init?: RequestInit) => (init?.headers as Record<string, string> | undefined)?.[CSRF_HEADER]
    expect(calls.map(([p]) => p)).toEqual([
      '/v1/decisions?state=open', '/v1/web/session', '/v1/decisions/1/answer', '/v1/decisions/2/answer',
    ])
    expect(header(calls[0]?.[1])).toBeUndefined()
    expect(header(calls[2]?.[1])).toBe('tok')
    expect(header(calls[3]?.[1])).toBe('tok')
  })

  it('counts a request that hangs as lost, by aborting it after the timeout', async () => {
    vi.useFakeTimers()
    try {
      vi.stubGlobal('fetch', vi.fn((_p: string, init?: RequestInit) => new Promise((_, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
      })))
      const out = api.status().then(() => 'answered', (e: unknown) => e)
      await vi.advanceTimersByTimeAsync(REQUEST_TIMEOUT)
      const e = await out
      expect(e).toBeInstanceOf(NetworkError)
      expect((e as Error).message).toContain('did not answer')
    } finally {
      vi.useRealTimers()
    }
  })

  it('reports an unreachable daemon as a network error and a refusal as an API error', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => {
      throw new TypeError('Failed to fetch')
    }))
    await expect(api.status()).rejects.toBeInstanceOf(NetworkError)
    vi.stubGlobal('fetch', vi.fn(async () => json({ error: 'nope' }, 401)))
    await expect(api.status()).rejects.toBeInstanceOf(ApiError)
  })

  it('asks the run history for as many runs as it is told to, and for the daemon’s default otherwise', async () => {
    const paths: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      paths.push(path)
      return json({})
    }))
    await history.runs(1, '2026-10-10T00:00:00Z')
    await history.runs(1, '2026-10-10T00:00:00Z', 'claude', 7, 1000)
    expect(paths[0]).not.toContain('limit')
    const url = new URL(paths[1] as string, 'http://x')
    expect(url.searchParams.get('limit')).toBe('1000')
    expect(url.searchParams.get('agent')).toBe('claude')
    expect(url.searchParams.get('lane_id')).toBe('7')
  })
})

describe('tolerant list reads', () => {
  it('reads a missing, null or wrong-typed list as empty', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({})))
    expect(await api.next()).toMatchObject({ items: [] })
    expect(await api.feed(0)).toMatchObject({ items: [], last: 0 })
    expect(await waitingOnYou.list()).toMatchObject({ items: [], count: 0 })
    expect(await deskQuestions.list()).toEqual([])
    expect(await topics.list()).toEqual({ topics: [] })
    expect(await projects.list()).toEqual({ projects: [] })
    expect(await history.runs(1, 'x')).toMatchObject({ runs: [], agents: [], count: 0 })
    vi.stubGlobal('fetch', vi.fn(async () => json(null)))
    expect(await api.lanes(1)).toEqual([])
    expect(await api.runs()).toEqual([])
    expect(await api.decisions('open')).toEqual([])
    expect(await deskQuestions.list()).toEqual([])
    expect(await waitingOnYou.list()).toMatchObject({ items: [] })
    vi.stubGlobal('fetch', vi.fn(async () => json({ items: 'oops', topics: 3 })))
    expect(await nextUp.list()).toMatchObject({ items: [] })
    expect(await topics.list()).toMatchObject({ topics: [] })
  })

  it('keeps what a well-formed answer carries', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ items: [{ id: 1 }], next_up: 1 })))
    expect(await api.next()).toMatchObject({ items: [{ id: 1 }], next_up: 1 })
  })
})
