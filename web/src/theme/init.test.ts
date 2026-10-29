import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { THEME_KEY } from './theme'

const root = resolve(__dirname, '../..')
const html = readFileSync(resolve(root, 'index.html'), 'utf8')
const init = readFileSync(resolve(root, 'public/theme-init.js'), 'utf8')

describe('pre-paint theme script', () => {
  it('is a same-origin classic script in the head, never inline (the daemon CSP is script-src self)', () => {
    const head = html.slice(html.indexOf('<head>'), html.indexOf('</head>'))
    expect(head).toMatch(/<script src="\/theme-init\.js"><\/script>/)
    for (const m of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)) {
      expect(m[1]).toMatch(/\bsrc="\//)
      expect((m[2] ?? '').trim()).toBe('')
    }
    expect(head).not.toMatch(/<script[^>]*\b(defer|async|type="module")/)
  })

  it('forces a stored light or dark and leaves system alone', () => {
    const run = () => new Function(init)()
    document.documentElement.removeAttribute('data-theme')
    window.localStorage.clear()
    run()
    expect(document.documentElement).not.toHaveAttribute('data-theme')
    window.localStorage.setItem(THEME_KEY, 'dark')
    run()
    expect(document.documentElement).toHaveAttribute('data-theme', 'dark')
  })

  it('does not throw when storage is unavailable', () => {
    const g = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(() => new Function(init)()).not.toThrow()
    g.mockRestore()
  })
})
