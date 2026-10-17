import { describe, expect, it, vi } from 'vitest'
import type { WaitingItem } from '../api/types'
import type { FeedEntry } from '../model/feed'
import { browserStorage, announceText, awaySummary, isUnseen, KEEP_SEEN, needsLine, notifiable, quiet, routineLine, SeenStore } from './seen'

const mem = (): Storage => {
  const m = new Map<string, string>()
  return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v), removeItem: (k) => void m.delete(k), clear: () => m.clear(), key: () => null, length: 0 }
}
const dec = (id: number, since = '2026-10-16T10:00:00Z'): WaitingItem => ({ kind: 'decision', action: 'answer', decision_id: id, title: `d${id}`, lane: 'feat/x', since })
const ev = (id: number, event: FeedEntry['event'], created: string, text = ''): FeedEntry => ({ id, kind: event, event, text, created })

describe('SeenStore', () => {
  it('starts empty and quiet: notifications off', () => {
    const s = new SeenStore(mem())
    expect(s.fresh).toBe(true)
    expect(s.getSnapshot().notify).toBe(false)
    expect(isUnseen(s.getSnapshot(), dec(1))).toBe(true)
  })
  it('keeps what was marked across reloads and caps the list', () => {
    const st = mem()
    const s = new SeenStore(st)
    s.mark(['decision:1:0'])
    s.look(5)
    s.setNotify(true)
    const again = new SeenStore(st).getSnapshot()
    expect(isUnseen(again, dec(1))).toBe(false)
    expect(again.looked).toBe(5)
    expect(again.notify).toBe(true)
    s.mark(Array.from({ length: KEEP_SEEN + 10 }, (_, i) => `k${i}`))
    expect(s.getSnapshot().seen).toHaveLength(KEEP_SEEN)
  })
  it('survives a corrupt record and a blocked store', () => {
    const st = mem()
    st.setItem('shepherd.seen.v1', '{nope')
    expect(new SeenStore(st).getSnapshot().seen).toEqual([])
    expect(() => new SeenStore(null).mark(['a'])).not.toThrow()
  })
})

describe('awaySummary', () => {
  const since = Date.parse('2026-10-16T09:00:00Z')
  it('puts what waits first and folds routine news to counts', () => {
    const a = awaySummary(
      [dec(1), dec(2, '2026-10-16T08:00:00Z')],
      [ev(1, 'run_passed', '2026-10-16T09:30:00Z'), ev(2, 'run_failed', '2026-10-16T09:31:00Z'), ev(3, 'mr', '2026-10-16T09:32:00Z', '!4 merged for lane x'), ev(4, 'mr', '2026-10-16T09:33:00Z', '!5 was already merged when Shepherd first looked'), ev(5, 'run_passed', '2026-10-16T08:00:00Z')],
      since,
    )
    expect(a.needs.map((n) => n.decision_id)).toEqual([1])
    expect([a.runs, a.failed, a.merges]).toEqual([1, 1, 1])
    expect(routineLine(a)).toBe('1 run finished, 1 failed, 1 merge')
  })
  it('is quiet when nothing happened', () => {
    expect(quiet(awaySummary([], [], since))).toBe(true)
  })
})

describe('wording', () => {
  it('counts by what the person must do', () => {
    expect(needsLine([dec(1), dec(2), { kind: 'mr', action: 'approve', title: 'm' }, { kind: 'question', action: 'reply', title: 'q' }])).toBe('2 decisions, 1 question, 1 merge request to review')
  })
  it('announces a new item with its lane, and notifies only for questions and decisions', () => {
    expect(announceText(dec(1))).toBe('New decision from feat/x')
    expect(notifiable(dec(1))).toBe(true)
    expect(notifiable({ kind: 'mr', action: 'approve', title: 'm' })).toBe(false)
  })
})

describe('look', () => {
  it('is kept but does not wake subscribers', () => {
    const s = new SeenStore(mem())
    let woke = 0
    s.subscribe(() => woke++)
    s.look(9)
    expect(s.getSnapshot().looked).toBe(9)
    expect(woke).toBe(0)
    s.mark(['a'])
    expect(woke).toBe(1)
  })
})

describe('browserStorage', () => {
  it('is null, not a throw, where reading localStorage is blocked', () => {
    const spy = vi.spyOn(window, 'localStorage', 'get').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError')
    })
    expect(browserStorage()).toBeNull()
    spy.mockRestore()
  })
})
