import { act, renderHook } from '@testing-library/react'
import { useLaneCounts } from './laneCounts'

const diff = vi.fn(async () => ({ committed: [{}, {}], uncommitted: [], untracked: [] }))
const artifacts = vi.fn(async () => ({ artifacts: [{}] }))
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), laneChanges: { diff: (...a: unknown[]) => (diff as (...a: unknown[]) => unknown)(...a) } }))
vi.mock('../api/artifacts', async (orig) => ({ ...(await orig<typeof import('../api/artifacts')>()), laneArtifacts: () => artifacts() }))

describe('useLaneCounts', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    diff.mockClear()
    artifacts.mockClear()
  })
  afterEach(() => vi.useRealTimers())

  it('reads once for feed events inside 15 s, then once more when the window ends', async () => {
    const { result, rerender } = renderHook(({ key }) => useLaneCounts(4, key), { initialProps: { key: 1 } })
    await act(async () => {})
    expect(result.current).toMatchObject({ changes: 2, artifacts: 1 })
    expect(diff).toHaveBeenCalledTimes(1)
    await act(async () => {
      vi.advanceTimersByTime(5_000)
    })
    rerender({ key: 2 })
    await act(async () => {
      vi.advanceTimersByTime(5_000)
    })
    rerender({ key: 3 })
    await act(async () => {})
    expect(diff).toHaveBeenCalledTimes(1)
    // The window ends 5 s later: the burst's last state is read once.
    await act(async () => {
      vi.advanceTimersByTime(5_000)
    })
    expect(diff).toHaveBeenCalledTimes(2)
    expect(artifacts).toHaveBeenCalledTimes(2)
    await act(async () => {
      vi.advanceTimersByTime(60_000)
    })
    expect(diff).toHaveBeenCalledTimes(2)
  })

  it('reads at once for an event after the window, and drops a pending read on unmount', async () => {
    const { rerender, unmount } = renderHook(({ key }) => useLaneCounts(4, key), { initialProps: { key: 1 } })
    await act(async () => {
      vi.advanceTimersByTime(16_000)
    })
    rerender({ key: 2 })
    await act(async () => {})
    expect(diff).toHaveBeenCalledTimes(2)
    rerender({ key: 3 })
    unmount()
    await act(async () => {
      vi.advanceTimersByTime(60_000)
    })
    expect(diff).toHaveBeenCalledTimes(2)
  })
})
