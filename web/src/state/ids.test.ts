import { parseRef, refLabel } from './ids'

describe('refLabel', () => {
  it('prefixes the id with its kind', () => {
    expect(refLabel('decision', 275)).toBe('decision 275')
    expect(refLabel('run', 311)).toBe('run 311')
  })
})

describe('parseRef', () => {
  it('reads each kind back', () => {
    expect(parseRef('decision 275')).toEqual({ kind: 'decision', id: 275 })
    expect(parseRef('question 43')).toEqual({ kind: 'question', id: 43 })
    expect(parseRef('item 82')).toEqual({ kind: 'item', id: 82 })
    expect(parseRef('run 311')).toEqual({ kind: 'run', id: 311 })
    expect(parseRef('request 7')).toEqual({ kind: 'request', id: 7 })
    expect(parseRef('topic 5')).toEqual({ kind: 'topic', id: 5 })
  })

  it('reads a bare #number as an item', () => {
    expect(parseRef('#82')).toEqual({ kind: 'item', id: 82 })
  })

  it('ignores surrounding space and case', () => {
    expect(parseRef('  Decision 275 ')).toEqual({ kind: 'decision', id: 275 })
  })

  it('rejects what is not a reference', () => {
    expect(parseRef('')).toBeNull()
    expect(parseRef('decision')).toBeNull()
    expect(parseRef('lane 3')).toBeNull()
    expect(parseRef('decision 0')).toBeNull()
    expect(parseRef('#0')).toBeNull()
    expect(parseRef('decision 275 extra')).toBeNull()
  })
})
