import { answerKey, clearDraft, draftKey, readDraft, writeDraft } from './chatDraft'

function memory() {
  const m = new Map<string, string>()
  return {
    m,
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  }
}

describe('chat drafts', () => {
  it('round-trips text and clears it', () => {
    const s = memory()
    expect(readDraft(s, draftKey(0))).toBe('')
    writeDraft(s, draftKey(0), 'half a thou')
    expect(readDraft(s, draftKey(0))).toBe('half a thou')
    clearDraft(s, draftKey(0))
    expect(readDraft(s, draftKey(0))).toBe('')
  })

  it('keeps one draft per workspace and per decision', () => {
    const s = memory()
    writeDraft(s, draftKey(1), 'one')
    writeDraft(s, draftKey(2), 'two')
    writeDraft(s, answerKey(7), 'seven')
    expect(readDraft(s, draftKey(1))).toBe('one')
    expect(readDraft(s, draftKey(2))).toBe('two')
    expect(readDraft(s, answerKey(7))).toBe('seven')
    expect(readDraft(s, answerKey(8))).toBe('')
  })

  it('stores nothing for empty text', () => {
    const s = memory()
    writeDraft(s, draftKey(0), 'x')
    writeDraft(s, draftKey(0), '')
    expect(s.m.size).toBe(0)
  })

  it('survives a missing or refusing store', () => {
    const broken = {
      getItem: () => { throw new Error('denied') },
      setItem: () => { throw new Error('denied') },
      removeItem: () => { throw new Error('denied') },
    }
    expect(readDraft(undefined, 'k')).toBe('')
    expect(readDraft(broken, 'k')).toBe('')
    expect(() => writeDraft(broken, 'k', 'x')).not.toThrow()
    expect(() => clearDraft(broken, 'k')).not.toThrow()
    expect(() => writeDraft(undefined, 'k', 'x')).not.toThrow()
  })
})
