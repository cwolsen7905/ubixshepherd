import { describe, expect, it } from 'vitest'
import { parsePrefixedId, splitPrefixedIds } from './ids'

describe('prefixed ids', () => {
  it('reads each prefix', () => {
    const kinds = ['Q-196', 'D-370', 'N-243', 'R-1674', 'L-548', 'T-87'].map((t) => parsePrefixedId(t)?.kind)
    expect(kinds).toEqual(['question', 'decision', 'next', 'run', 'lane', 'topic'])
  })

  it('rejects other letters, zero and lowercase', () => {
    for (const t of ['X-1', 'Q-0', 'q-5', 'Q-', 'Q-5a']) expect(parsePrefixedId(t)).toBeNull()
  })

  it('splits text around ids', () => {
    expect(splitPrefixedIds('see Q-196, then D-370.')).toEqual([
      'see ',
      { kind: 'question', id: 196, text: 'Q-196' },
      ', then ',
      { kind: 'decision', id: 370, text: 'D-370' },
      '.',
    ])
  })

  it('leaves ids glued to words or other numbers alone', () => {
    for (const t of ['MQ-5', 'UTF-8', 'D-1-2', 'Q-5x', 'a_Q-5', 'https://example.com/T-3', 'docs/L-5.md', 'key=Q-7', 'a/Q-5', 'Q-5/x', '#Q-5', 'me@Q-5']) expect(splitPrefixedIds(t)).toEqual([t])
  })
})

describe('prefixed ids at the end of a sentence', () => {
  it('still link before punctuation', () => {
    expect(splitPrefixedIds('(see Q-1). Then D-2.')).toHaveLength(5)
  })
})

// The trade-off: an ID is linked on its shape alone, not checked against what exists, so
// prose that happens to match ("vitamin D-2", "test T-1") becomes a link that opens an
// empty or unrelated item in the side column. The shape rules above keep the common
// glued forms out; checking each ID against known items would need a read per render.
describe('prefixed ids in ordinary prose', () => {
  it('are linked by shape alone, a known false positive', () => {
    expect(splitPrefixedIds('vitamin D-2')).toEqual(['vitamin ', { kind: 'decision', id: 2, text: 'D-2' }])
  })
})
