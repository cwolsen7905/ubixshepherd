// The tab icon: the uBix mark (as public/favicon.svg), with a lantern dot in the corner
// while something waits on the person, so a pinned or background tab shows it.
const MARK =
  '<rect width="32" height="32" fill="#0f62fe"/>' +
  '<rect x="6" y="6" width="9" height="9" fill="#fff"/>' +
  '<rect x="17" y="6" width="9" height="9" fill="#fff" opacity=".55"/>' +
  '<rect x="6" y="17" width="9" height="9" fill="#fff" opacity=".55"/>' +
  '<rect x="17" y="17" width="9" height="9" fill="#fff"/>'

const LANTERN = '<circle cx="25" cy="7" r="7" fill="#e3aa4f" stroke="#1c242b" stroke-width="1.5"/>'

/** The icon's href: the static file when nothing waits, a data URL with the lantern dot when something does. */
export function faviconHref(waiting: boolean): string {
  if (!waiting) return '/favicon.svg'
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">${MARK}${LANTERN}</svg>`
  return `data:image/svg+xml,${encodeURIComponent(svg)}`
}
