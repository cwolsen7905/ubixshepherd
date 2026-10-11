import { describe, expect, it } from 'vitest'
import { faviconHref } from './favicon'

describe('faviconHref', () => {
  it('is the plain file when nothing waits', () => {
    expect(faviconHref(false)).toBe('/favicon.svg')
  })

  it('adds the lantern dot when something waits', () => {
    const href = faviconHref(true)
    expect(href.startsWith('data:image/svg+xml,')).toBe(true)
    expect(decodeURIComponent(href)).toContain('<circle')
  })
})
