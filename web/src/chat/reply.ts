import type { DeskEvent, DeskQuestion } from '../api/types'

/**
 * The question a person's message answers, read from the line the daemon puts in front of
 * it ("[Replying to your question N: ...]"). It may follow an "About ..." line.
 */
export function replyRef(text: string): number | null {
  const m = /^\[Replying to your question (\d+):.*\]$/m.exec(text)
  return m ? Number(m[1]) : null
}

/**
 * Each question under the last desk message of the turn it was asked in, answered and
 * skipped ones included: the row a reply refers to must stay findable once the question
 * is no longer open.
 */
export function questionsByMessage(events: readonly DeskEvent[], questions: readonly DeskQuestion[]): Map<number, DeskQuestion[]> {
  const lastOfTurn = new Map<number, number>()
  for (const e of events) if (e.kind === 'assistant' && e.turn !== undefined) lastOfTurn.set(e.turn, e.seq)
  const out = new Map<number, DeskQuestion[]>()
  for (const q of questions) {
    const seq = lastOfTurn.get(q.turn)
    if (seq !== undefined) out.set(seq, [...(out.get(seq) ?? []), q])
  }
  return out
}

/** What to tell the person after a jump to a question's row: nothing when it landed. */
export function replyJumpNote(id: number, landed: boolean): string {
  return landed ? '' : `Question ${id} is not in the loaded thread. Load earlier messages to see it.`
}

/** The answered and skipped questions, for a reply that names one no longer open. */
export async function closedQuestions(list: (state: 'answered' | 'skipped') => Promise<DeskQuestion[]>): Promise<DeskQuestion[]> {
  const [answered, skipped] = await Promise.all([list('answered'), list('skipped')])
  return [...answered, ...skipped]
}

const yesNoStart = /^(?:do|does|did|is|are|was|were|can|could|should|shall|will|would|have|has|had|may|must|ok to|okay to|want me to|shall i|ready to)\b/i

/**
 * Whether a question reads as a plain yes-or-no one. The daemon keeps no flag for it, so this
 * is deliberately conservative: one short sentence that opens with an auxiliary verb, ends in a
 * question mark and offers no alternative ("or"). A miss only means no buttons.
 */
export function isYesNo(text: string): boolean {
  const t = text.trim()
  if (!t || t.length > 200 || /\n/.test(t)) return false
  if (!t.endsWith('?') || t.indexOf('?') !== t.length - 1) return false
  if (/\bor\b/i.test(t)) return false
  return yesNoStart.test(t)
}
