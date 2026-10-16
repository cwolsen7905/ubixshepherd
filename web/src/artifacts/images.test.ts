import { resolveImage, splitImages } from './images'

describe('resolveImage', () => {
  it('resolves against the markdown file’s directory', () => {
    expect(resolveImage('docs/design/a.md', 'img/x.png')).toBe('docs/design/img/x.png')
    expect(resolveImage('docs/design/a.md', './x.png')).toBe('docs/design/x.png')
    expect(resolveImage('docs/design/a.md', '../shots/x.png?raw=1#top')).toBe('docs/shots/x.png')
    expect(resolveImage('README.md', 'x%20y.png')).toBe('x y.png')
  })
  it('takes a leading slash as the repo root', () => {
    expect(resolveImage('docs/a.md', '/assets/x.png')).toBe('assets/x.png')
  })
  it('refuses remote, data and escaping references', () => {
    for (const src of ['https://evil.example/x.png', 'http://a/b.png', 'data:image/png;base64,AA', '//evil/x.png', '../../x.png', 'a\\b.png', ''])
      expect(resolveImage('docs/a.md', src)).toBeNull()
  })
})

describe('splitImages', () => {
  it('splits at an image and leaves fenced code alone', () => {
    const p = splitImages('# T\n\n![a b](x.png)\n\ntext\n```\n![no](y.png)\n```\n')
    expect(p.filter((x) => x.t === 'img')).toEqual([{ t: 'img', alt: 'a b', src: 'x.png' }])
    expect(p.map((x) => (x.t === 'md' ? x.text : '')).join('')).toContain('![no](y.png)')
  })

  it('leaves inline images and tilde-fenced code in the markdown', () => {
    const src = 'See ![a](x.png) for the flow\n- ![b](y.png)\n~~~\n![c](z.png)\n~~~\n'
    expect(splitImages(src)).toEqual([{ t: 'md', text: src.replace(/\n$/, '\n') }])
  })
})
