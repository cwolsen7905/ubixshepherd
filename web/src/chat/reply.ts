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

// Only openers that ask about a state of things. "Should I", "Can I", "Want me to" and the like
// propose an action the desk would then take, so they are left to a typed reply.
const yesNoStart = /^(?:is|are|was|were|does|do|did|has|have|had)\b/i

// A proposal to act, whoever acts: first person, "to <verb>", "go ahead".
const proposal = /\b(?:i|me|we|us|my|let['’]s|to|go ahead|should|shall)\b/i

const riskyWording = /\b(?:delet\w*|remov\w*|drop\w*|force\w*|overwrit\w*|reset\w*|wip\w*|destroy\w*|push\w*|merg\w*|releas\w*|publish\w*|deploy\w*|ship\w*|tag\w*|revert\w*|approv\w*|clos\w*|rebas\w*|kill\w*|stop\w*|migrat\w*|install\w*|commit\w*|run|runs|running|start\w*|restart\w*|apply|applied|accept\w*|enabl\w*|disabl\w*|cancel\w*|abort\w*|creat\w*|updat\w*|upgrad\w*|execut\w*|continu\w*|proceed\w*|resolv\w*|sign\w*|send\w*|sent|move\w*|renam\w*|clean\w*|land\w*|hand\w*|live)\b/i

/**
 * Whether a question reads as a plain yes-or-no one. The daemon keeps no flag for it, so this
 * is deliberately conservative: one short sentence that opens with an auxiliary verb, ends in a
 * question mark and offers no alternative ("or"), second clause or negation, and names no consequential action
 * (delete, force, push, merge, approve, run and the like; the list is not exhaustive, which is why
 * only state-asking openers qualify at all). A miss only means no buttons.
 */
export function isYesNo(text: string): boolean {
  const t = text.trim()
  if (!t || t.length > 200 || /\n/.test(t)) return false
  if (!t.endsWith('?') || t.indexOf('?') !== t.length - 1) return false
  if (/\bor\b/i.test(t)) return false
  // One ask only: no second clause or sentence, so a Yes cannot approve half of it.
  if (/[,;]|\.\s|\band\b/i.test(t)) return false
  // A negative question ("Can't you...?", "Is it not...?") makes Yes ambiguous.
  if (/n['’]t\b|\bnot\b/i.test(t)) return false
  // Consequential actions get a typed or confirmed reply, never one click.
  if (riskyWording.test(t) || proposal.test(t)) return false
  return yesNoStart.test(t)
}
