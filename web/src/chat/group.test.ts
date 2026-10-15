import { describe, expect, it } from 'vitest'
import type { DeskEvent } from '../api/types'
import type { FeedEntry } from '../model/feed'
import type { ThreadItem } from '../state/desk'
import { groupThread } from './group'

const feed = (id: number, event = 'run_ended'): ThreadItem => ({
  type: 'feed', key: `f${id}`, f: { id, event, text: '', created: '2026-01-01T00:00:00Z' } as unknown as FeedEntry,
})
const desk = (seq: number, kind: string, turn = 1): ThreadItem => ({
  type: 'desk', key: `d${seq}`, e: { seq, workspace_id: 0, kind, text: kind, turn, created: '2026-01-01T00:00:00Z' } as DeskEvent,
})
const types = (items: ThreadItem[]) => groupThread(items).map((r) => r.type)

describe('groupThread', () => {
  it('folds three or more consecutive events, leaves two alone', () => {
    expect(types([feed(1), feed(2)])).toEqual(['item', 'item'])
    const rows = groupThread([feed(1), feed(2), feed(3)])
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ type: 'updates' })
  })

  it('keeps a decision on its own row and breaks the run there', () => {
    expect(types([feed(1), feed(2), feed(3), feed(4, 'decision_asked'), feed(5), feed(6), feed(7)])).toEqual(['updates', 'item', 'updates'])
  })

  it('gathers tool calls and wake notes under the reply of the turn', () => {
    const rows = groupThread([desk(1, 'user'), desk(2, 'system'), desk(3, 'tool'), desk(4, 'tool'), desk(5, 'assistant')])
    expect(rows.map((r) => r.type)).toEqual(['item', 'reply'])
    const reply = rows[1]
    expect(reply?.type === 'reply' && reply.work.map((w) => w.kind)).toEqual(['system', 'tool', 'tool'])
  })

  it('folds work that no reply followed into one row, and drops nothing', () => {
    const rows = groupThread([desk(1, 'user'), desk(2, 'tool'), desk(3, 'tool')])
    expect(rows.map((r) => r.type)).toEqual(['item', 'work'])
  })
})
