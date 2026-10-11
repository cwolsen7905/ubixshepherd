import { editorHref, joinPath } from './editorLink'

describe('editorHref', () => {
  it('fills {path} with the escaped path, keeping slashes', () => {
    expect(editorHref('vscode://file/{path}', '/Users/me/my repo/a#b.go')).toBe('vscode://file/Users/me/my%20repo/a%23b.go')
  })
  it('fills {line}, defaulting to 1', () => {
    expect(editorHref('idea://open?file={path}&line={line}', '/x/y.go', 12)).toBe('idea://open?file=/x/y.go&line=12')
    expect(editorHref('vscode://file/{path}:{line}', '/x/y.go')).toBe('vscode://file/x/y.go:1')
  })
  it('keeps a Windows drive and turns backslashes into slashes', () => {
    expect(editorHref('vscode://file/{path}', 'C:\\w\\lane\\a.go')).toBe('vscode://file/C:/w/lane/a.go')
  })
  it('gives nothing for "", a missing template or a scripting scheme', () => {
    expect(editorHref('', '/x')).toBeNull()
    expect(editorHref(undefined, '/x')).toBeNull()
    expect(editorHref(' JavaScript:alert({path})', '/x')).toBeNull()
    expect(editorHref('data:text/html,{path}', '/x')).toBeNull()
  })
})

describe('joinPath', () => {
  it('joins with one separator in the root\'s style', () => {
    expect(joinPath('/w/lane/', 'a/b.go')).toBe('/w/lane/a/b.go')
    expect(joinPath('C:\\w\\lane', 'a/b.go')).toBe('C:\\w\\lane\\a/b.go')
  })
})
