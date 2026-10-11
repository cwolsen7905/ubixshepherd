// The "open in editor" links: a template from config (web.editor_url) rendered here, in the
// browser. The daemon has already refused templates with other placeholders or a scripting
// scheme; the check below is the page's own, since it builds an href.

/** The daemon's default, used when the config could not be read. */
export const DEFAULT_EDITOR_URL = 'vscode://file/{path}'

const BLOCKED = /^(javascript|data|vbscript|file|blob|about):/i

/** Fills the template for an absolute path (and a line); null when links are off or unsafe. */
export function editorHref(template: string | null | undefined, path: string, line?: number): string | null {
  if (!template || !path || BLOCKED.test(template.trim())) return null
  // The path keeps its slashes (and a drive's colon): editors read vscode://file/Users/me/x.
  const abs = path.replace(/\\/g, '/')
  const escape = (p: string) => p.split('/').map((seg) => encodeURIComponent(seg).replace(/%3A/gi, ':')).join('/')
  return template
    // "/{path}" before an absolute path would double the slash.
    .replace(/(\/?)\{path\}/g, (_, slash: string) => (slash && abs.startsWith('/') ? '/' + escape(abs.slice(1)) : slash + escape(abs)))
    .replace(/\{line\}/g, String(line ?? 1))
}

/** Joins a worktree and a repo-relative path, for either separator style. */
export function joinPath(root: string, rel: string): string {
  const sep = root.includes('\\') && !root.includes('/') ? '\\' : '/'
  return root.replace(/[\\/]+$/, '') + sep + rel.replace(/^[\\/]+/, '')
}
