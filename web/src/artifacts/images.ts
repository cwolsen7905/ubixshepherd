// Images a markdown artifact refers to. The renderer never fetches a remote host (the page's
// CSP allows only the daemon's own origin), so a reference is either a file of the artifact's
// lane, read through the lane content endpoint, or it is not shown as an image.

/**
 * The repo-relative path an image reference names, resolved against the markdown file's own
 * directory (`/x.png` is the repo root). Null for a remote or data URL, an absolute path
 * outside the repo, or a path that climbs out of the repo.
 */
export function resolveImage(artifactPath: string, src: string): string | null {
  const ref = src.trim().replace(/[?#].*$/, '')
  if (ref === '' || /^[a-z][a-z0-9+.-]*:/i.test(ref) || ref.startsWith('//') || ref.includes('\\')) return null
  let raw: string
  try {
    raw = decodeURIComponent(ref)
  } catch {
    return null
  }
  const parts: string[] = ref.startsWith('/') ? [] : artifactPath.split('/').slice(0, -1)
  for (const seg of raw.split('/')) {
    if (seg === '' || seg === '.') continue
    if (seg === '..') {
      if (parts.length === 0) return null
      parts.pop()
    } else parts.push(seg)
  }
  return parts.length ? parts.join('/') : null
}

export type Piece = { t: 'md'; text: string } | { t: 'img'; alt: string; src: string }

const IMAGE = /!\[([^\]\n]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g

/** Splits markdown at its image references (outside fenced code) so images can be rendered as images. */
export function splitImages(text: string): Piece[] {
  const out: Piece[] = []
  let buf: string[] = []
  let fenced = false
  const flush = () => {
    if (buf.length) out.push({ t: 'md', text: buf.join('\n') })
    buf = []
  }
  for (const line of text.replace(/\r\n?/g, '\n').split('\n')) {
    if (/^\s*```/.test(line)) fenced = !fenced
    if (fenced || !line.includes('![')) {
      buf.push(line)
      continue
    }
    let at = 0
    for (const m of line.matchAll(IMAGE)) {
      const before = line.slice(at, m.index)
      if (before.trim()) buf.push(before)
      flush()
      out.push({ t: 'img', alt: m[1] ?? '', src: m[2] ?? '' })
      at = (m.index ?? 0) + m[0].length
    }
    const rest = line.slice(at)
    if (rest.trim() || at === 0) buf.push(rest)
    else buf.push('')
  }
  flush()
  return out
}
