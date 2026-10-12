import { describe, expect, it } from 'vitest'
import type { ThreadItem } from '../state/desk'
import { ACTIVE_WINDOW_MS, activeTopics, isActive, splitTopics, feedRefKind, inTopic, memberKeys, readTopicFilter, topicState, withTopicFilter, type TopicView } from './topic'

const topic = (over: Partial<TopicView> = {}): TopicView => ({
  id: 1, title: 'release', status: 'open', source: 'desk', created: '', updated: '', activity: '', waits: 0, links: [], ...over,
})

describe('the topic filter in the hash', () => {
  it('reads and writes topic=N and keeps the rest', () => {
    expect(readTopicFilter('#/chat?topic=7')).toBe(7)
    expect(readTopicFilter('#/chat')).toBeNull()
    expect(readTopicFilter('#/chat?topic=x')).toBeNull()
    expect(withTopicFilter('#/chat', 3)).toBe('#/chat?topic=3')
    expect(withTopicFilter('#/chat?topic=3', null)).toBe('#/chat')
    expect(withTopicFilter('#/chat?a=1&topic=3', null)).toBe('#/chat?a=1')
  })
})

describe('which rows belong to a topic', () => {
  const t = topic({
    links: [
      { kind: 'lane', ref: '5', by: 'desk', time: '' },
      { kind: 'turn', ref: '40', by: 'person', time: '' },
    ],
  })
  const members = memberKeys(t)
  const feed = (event: string, ref: number): ThreadItem =>
    ({ type: 'feed', key: 'f1', f: { id: 1, kind: event, text: '', ref, created: '', event } }) as unknown as ThreadItem

  it('takes a feed row about a held lane and a linked turn, and nothing else', () => {
    expect(inTopic(feed('lane_opened', 5), members)).toBe(true)
    expect(inTopic(feed('lane_opened', 6), members)).toBe(false)
    expect(inTopic(feed('decision_asked', 5), members)).toBe(false)
    expect(inTopic({ type: 'desk', key: 'd1', e: { seq: 41, workspace_id: 1, kind: 'user', turn: 40, created: '' } }, members)).toBe(true)
    expect(inTopic({ type: 'desk', key: 'd2', e: { seq: 50, workspace_id: 1, kind: 'user', created: '' } }, members)).toBe(false)
  })
  it('maps an event to what its ref names', () => {
    expect(feedRefKind('run_failed')).toBe('run')
    expect(feedRefKind('request_routed')).toBe('request')
    expect(feedRefKind('gate')).toBeNull()
  })
})

describe('a topic row state', () => {
  it('says what waits, what is settled and what to reopen', () => {
    expect(topicState(topic({ waits: 2 }))).toBe('waits on you: 2')
    expect(topicState(topic({ settled: true }))).toBe('settled?')
    expect(topicState(topic({ status: 'cleared', reopen: true }))).toContain('needs you')
    expect(topicState(topic())).toBe('')
  })
})

describe('activeTopics', () => {
  it('keeps open topics in order and drops cleared ones', () => {
    const list = [topic({ id: 1 }), topic({ id: 2, status: 'cleared' }), topic({ id: 3 })]
    expect(activeTopics(list).map((x) => x.id)).toEqual([1, 3])
  })
})

describe('active and idle', () => {
  const now = Date.parse('2026-10-11T12:00:00Z')
  const ago = (ms: number) => new Date(now - ms).toISOString()

  it('is active inside the window and idle at exactly its age', () => {
    expect(isActive(topic({ activity: ago(ACTIVE_WINDOW_MS - 1) }), now)).toBe(true)
    expect(isActive(topic({ activity: ago(ACTIVE_WINDOW_MS) }), now)).toBe(false)
    expect(isActive(topic({ activity: ago(ACTIVE_WINDOW_MS + 1) }), now)).toBe(false)
  })

  it('is active when something waits, however old', () => {
    expect(isActive(topic({ activity: ago(ACTIVE_WINDOW_MS * 10), waits: 1 }), now)).toBe(true)
  })

  it('is active with a running run, but not with a finished one', () => {
    const old = ago(ACTIVE_WINDOW_MS * 10)
    const run = (state: string) => [{ kind: 'run' as const, ref: '9', by: 'shepherd' as const, state, time: old }]
    expect(isActive(topic({ activity: old, links: run('running') }), now)).toBe(true)
    expect(isActive(topic({ activity: old, links: run('succeeded') }), now)).toBe(false)
  })

  it('treats an undated topic as active', () => {
    expect(isActive(topic({ activity: '' }), now)).toBe(true)
  })

  it('splits open topics in order, keeps the selected idle one active, and drops cleared', () => {
    const old = ago(ACTIVE_WINDOW_MS * 2)
    const list = [
      topic({ id: 1, activity: ago(1000) }),
      topic({ id: 2, activity: old }),
      topic({ id: 3, activity: old }),
      topic({ id: 4, activity: ago(1000), status: 'cleared' }),
      topic({ id: 5, activity: old, waits: 1 }),
    ]
    const plain = splitTopics(list, now, null)
    expect(plain.active.map((x) => x.id)).toEqual([1, 5])
    expect(plain.idle.map((x) => x.id)).toEqual([2, 3])
    const sel = splitTopics(list, now, 3)
    expect(sel.active.map((x) => x.id)).toEqual([1, 3, 5])
    expect(sel.idle.map((x) => x.id)).toEqual([2])
  })
})
