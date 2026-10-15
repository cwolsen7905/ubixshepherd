// A run's task as the person wrote it. The daemon may hold the whole prompt it sent the
// agent (a preamble about the lane, its scope and the tools, then "Task:" and the task),
// and a row or a tooltip should show the task, not the boilerplate.

const MARK = '\nTask:\n'

/** The task text of a run: what follows the prompt's "Task:" marker, else the text as given. */
export function taskText(s: string): string {
  const at = s.lastIndexOf(MARK)
  const body = at >= 0 ? s.slice(at + MARK.length) : s.startsWith('Task:\n') ? s.slice('Task:\n'.length) : s
  return body.trim()
}

/** The first line of the task, for a row. */
export function taskLine(s: string): string {
  return taskText(s).split('\n').find((l) => l.trim() !== '')?.trim() ?? ''
}
