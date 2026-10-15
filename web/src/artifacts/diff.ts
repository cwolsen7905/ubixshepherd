// A line diff between two texts, for comparing a version with the latest. Longest common
// subsequence over lines; past the cap it says so rather than stalling the page.
export type DiffLine = { op: ' ' | '+' | '-'; text: string }

/** The most lines each side may have: the table is old x new. */
export const DIFF_MAX_LINES = 2000

export function diffLines(older: string, newer: string): DiffLine[] | null {
  const a = older.split('\n')
  const b = newer.split('\n')
  if (a.length > DIFF_MAX_LINES || b.length > DIFF_MAX_LINES) return null
  const n = a.length
  const m = b.length
  const w = m + 1
  // lcs[i * w + j]: the common length of a[i:] and b[j:].
  const lcs = new Uint16Array((n + 1) * w)
  const at = (i: number, j: number) => lcs[i * w + j] ?? 0
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i * w + j] = a[i] === b[j] ? at(i + 1, j + 1) + 1 : Math.max(at(i + 1, j), at(i, j + 1))
    }
  }
  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ op: ' ', text: a[i] ?? '' })
      i++
      j++
    } else if (at(i + 1, j) >= at(i, j + 1)) out.push({ op: '-', text: a[i++] ?? '' })
    else out.push({ op: '+', text: b[j++] ?? '' })
  }
  while (i < n) out.push({ op: '-', text: a[i++] ?? '' })
  while (j < m) out.push({ op: '+', text: b[j++] ?? '' })
  return out
}
