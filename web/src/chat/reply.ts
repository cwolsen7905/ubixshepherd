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
