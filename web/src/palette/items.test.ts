import { describe, expect, it } from 'vitest'
import { decision, lane, run } from '../test/fixtures'
import { answerNextTarget, buildItems, match } from './items'

const snap = {
  lanes: [
    lane({ id: 12, branch: 'feat/export-csv', repo: 'api' }),
    lane({ id: 3, branch: 'fix/login', repo: 'web' }),
    lane({ id: 9, branch: 'old', state: 'closed' }),
  ],
  runs: [run({ id: 12, lane: 'feat/export-csv' })],
  decisions: [
    decision({
      id: 7,
      question: 'Which format should export use?',
      state: 'open',
    }),
    decision({ id: 8, state: 'answered' }),
  ],
  waiting: null,
}
const items = buildItems(snap, [{ id: 4, title: 'Billing rework', status: 'open' }])

describe('palette matcher', () => {
  it('lists actions and pages first with an empty query', () => {
    const r = match(items, '')
    expect(r[0]?.label).toBe('Answer next')
    expect(r.map((i) => i.group).slice(0, 3)).toEqual(['Actions', 'Actions', 'Pages'])
  })

  it('finds a page by name', () => {
    expect(match(items, 'timel')[0]?.target).toEqual({ to: '#/timeline' })
  })

  it('finds a lane by name or branch word', () => {
    expect(match(items, 'csv')[0]?.target).toEqual({ to: '#/lanes/12' })
    expect(match(items, 'login').map((i) => i.id)).toContain('lane:3')
  })

  it('finds by number, with or without #', () => {
    expect(
      match(items, '12')
        .map((i) => i.id)
        .sort(),
    ).toEqual(['lane:12', 'run:12'])
    expect(match(items, '#7').map((i) => i.id)).toEqual(['decision:7'])
    expect(match(items, 'run 12').map((i) => i.id)).toEqual(['run:12'])
  })

  it('leaves out closed lanes and answered decisions', () => {
    expect(items.some((i) => i.id === 'lane:9' || i.id === 'decision:8')).toBe(false)
  })

  it('finds a topic and requires every word', () => {
    expect(match(items, 'billing')[0]?.id).toBe('topic:4')
    expect(match(items, 'billing zzz')).toEqual([])
  })

  it('points answer next at the oldest waiting decision', () => {
    expect(answerNextTarget(snap)).toBe('#/decisions?d=7')
    const waiting = {
      count: 1,
      counts: { decisions: 1, mrs: 0, ship: 0, next: 0 },
      items: [
        {
          kind: 'decision' as const,
          action: 'answer' as const,
          decision_id: 5,
          title: 't',
        },
      ],
    }
    expect(answerNextTarget({ ...snap, waiting })).toBe('#/decisions?d=5')
    expect(answerNextTarget({ ...snap, decisions: [], waiting: null })).toBe('#/decisions')
  })
})
