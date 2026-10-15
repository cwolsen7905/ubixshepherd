// A run's task as the person wrote it. The daemon may hold the whole prompt it sent the
// agent (a preamble about the lane, its scope and the tools, then "Task:" and the task),
// and a row or a tooltip should show the task, not the boilerplate.

const MARK = '\nTask:\n'
/** The daemon's prompt puts a blank line before its marker; repo standards before it may quote "Task:" too. */
const PROMPT_MARK = '\n\nTask:\n'

/** The first line of the prompt's preamble: a task read from the first line alone is only this. */
const PREAMBLE = 'You are working for uBixShepherd in lane '

/**
 * The task text of a run: what follows the prompt's first blank-line-led "Task:" marker (the
 * daemon's own, so a standards digest or task that quotes "Task:" does not cut it), else the first marker, else the text as given.
 * A lone preamble line, which is all a first-line read of the prompt keeps, is no task.
 */
export function taskText(s: string): string {
  const p = s.indexOf(PROMPT_MARK)
  const at = p >= 0 ? p + 1 : s.indexOf(MARK)
  const body = at >= 0 ? s.slice(at + MARK.length) : s.startsWith('Task:\n') ? s.slice('Task:\n'.length) : s
  const text = body.trim()
  return at < 0 && text.startsWith(PREAMBLE) && !text.includes('\n') ? '' : text
}

/** The first line of the task, for a row. */
export function taskLine(s: string): string {
  return taskText(s).split('\n').find((l) => l.trim() !== '')?.trim() ?? ''
}
