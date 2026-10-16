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
const ONLY_IMAGES = /^\s*(?:!\[[^\]\n]*\]\([^)\s]+(?:\s+"[^"]*")?\)\s*)+$/

/**
 * Splits markdown at the lines that hold only images (outside fenced code, ``` or ~~~) so
 * those can be rendered as images. An image inside a sentence or a list item stays in the
 * markdown, which shows it as text: splitting there would break the paragraph or the list.
 */
export function splitImages(text: string): Piece[] {
  const out: Piece[] = []
  let buf: string[] = []
  let fence = ''
  const flush = () => {
    if (buf.length) out.push({ t: 'md', text: buf.join('\n') })
    buf = []
  }
  for (const line of text.replace(/\r\n?/g, '\n').split('\n')) {
    const f = /^\s*(```|~~~)/.exec(line)?.[1]
    if (f && (!fence || fence === f)) fence = fence ? '' : f
    else if (!fence && ONLY_IMAGES.test(line)) {
      flush()
      for (const m of line.matchAll(IMAGE)) out.push({ t: 'img', alt: m[1] ?? '', src: m[2] ?? '' })
      continue
    }
    buf.push(line)
  }
  flush()
  return out
}
