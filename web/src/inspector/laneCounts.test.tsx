import { act, renderHook } from '@testing-library/react'
import { useLaneCounts } from './laneCounts'

const diff = vi.fn(async () => ({ committed: [{}, {}], uncommitted: [], untracked: [] }))
const artifacts = vi.fn(async () => ({ artifacts: [{}] }))
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), laneChanges: { diff: (...a: unknown[]) => (diff as (...a: unknown[]) => unknown)(...a) } }))
vi.mock('../api/artifacts', async (orig) => ({ ...(await orig<typeof import('../api/artifacts')>()), laneArtifacts: () => artifacts() }))

describe('useLaneCounts', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    diff.mockClear()
    artifacts.mockClear()
  })
  afterEach(() => vi.useRealTimers())

  it('reads once for feed events inside 15 s and again after', async () => {
    const { result, rerender } = renderHook(({ key }) => useLaneCounts(4, key), { initialProps: { key: 1 } })
    await act(async () => {})
    expect(result.current).toMatchObject({ changes: 2, artifacts: 1 })
    expect(diff).toHaveBeenCalledTimes(1)
    vi.setSystemTime(Date.now() + 5_000)
    rerender({ key: 2 })
    vi.setSystemTime(Date.now() + 5_000)
    rerender({ key: 3 })
    await act(async () => {})
    expect(diff).toHaveBeenCalledTimes(1)
    expect(artifacts).toHaveBeenCalledTimes(1)
    vi.setSystemTime(Date.now() + 6_000)
    rerender({ key: 4 })
    await act(async () => {})
    expect(diff).toHaveBeenCalledTimes(2)
    expect(artifacts).toHaveBeenCalledTimes(2)
  })
})
