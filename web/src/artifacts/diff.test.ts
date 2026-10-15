import { diffLines, DIFF_MAX_LINES } from './diff'

describe('diffLines', () => {
  it('marks added and removed lines and keeps the common ones', () => {
    expect(diffLines('a\nb\nc', 'a\nc\nd')).toEqual([
      { op: ' ', text: 'a' },
      { op: '-', text: 'b' },
      { op: ' ', text: 'c' },
      { op: '+', text: 'd' },
    ])
  })
  it('gives up past the cap', () => {
    expect(diffLines('x\n'.repeat(DIFF_MAX_LINES + 1), 'y')).toBeNull()
  })
})
