import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LoginPage } from './Login'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function reply(status: number, body: unknown, headers: Record<string, string> = {}) {
  return vi.fn(async () => new Response(JSON.stringify(body), { status, headers }))
}

async function fill(code: string) {
  const u = userEvent.setup()
  await u.type(screen.getByLabelText('Password'), 'correct horse battery')
  await u.type(screen.getByLabelText(/^(Code|Recovery code)$/), code)
  await u.click(screen.getByRole('button', { name: 'Sign in' }))
}

describe('LoginPage', () => {
  it('posts the password and code, without a CSRF header, and signs in', async () => {
    const f = reply(200, { csrf: 'x' })
    vi.stubGlobal('fetch', f)
    const done = vi.fn()
    render(<LoginPage onSignedIn={done} host="shepherd.example.com" />)
    await fill('123456')
    await waitFor(() => expect(done).toHaveBeenCalled())
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/v1/web/login')
    expect(JSON.parse(init.body as string)).toEqual({ password: 'correct horse battery', code: '123456' })
    expect(Object.keys(init.headers as object).join()).not.toMatch(/csrf/i)
  })

  it('shows a wrong code as a failure with words, and clears the code', async () => {
    vi.stubGlobal('fetch', reply(401, { error: 'wrong password or code' }))
    const done = vi.fn()
    render(<LoginPage onSignedIn={done} />)
    await fill('000000')
    expect((await screen.findByRole('alert')).textContent).toContain('✗ wrong password or code')
    expect(done).not.toHaveBeenCalled()
    expect((screen.getByLabelText('Code') as HTMLInputElement).value).toBe('')
  })

  it('says when to try again after a lock, and holds the button', async () => {
    vi.stubGlobal('fetch', reply(429, { error: 'too many attempts' }, { 'Retry-After': '120' }))
    render(<LoginPage onSignedIn={() => {}} />)
    await fill('000000')
    expect((await screen.findByRole('alert')).textContent).toContain('Try again in 120 s.')
    await userEvent.setup().type(screen.getByLabelText('Code'), '123456')
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeDisabled()
  })

  it('takes a recovery code in place of the authenticator code', async () => {
    const f = reply(200, {})
    vi.stubGlobal('fetch', f)
    render(<LoginPage onSignedIn={() => {}} />)
    await userEvent.setup().click(screen.getByRole('button', { name: 'Use a recovery code' }))
    await fill('abcde-fghij')
    await waitFor(() => expect(f).toHaveBeenCalled())
    expect(JSON.parse((f.mock.calls[0] as unknown as [string, RequestInit])[1].body as string).code).toBe('abcde-fghij')
  })

  it('reports an unreachable daemon in plain words', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('failed') }))
    render(<LoginPage onSignedIn={() => {}} />)
    await fill('123456')
    expect((await screen.findByRole('alert')).textContent).toContain('could not be reached')
  })
})
