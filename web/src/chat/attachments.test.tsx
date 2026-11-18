import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { DeskSnapshot, DeskThread } from '../state/desk'
import { AttachmentList, MAX_FILE_BYTES, refusal, sizeText } from './attachments'
import { Composer } from './composer'
import { Thread } from '../ui/Thread'

function rig(over: Partial<Record<'attachFile' | 'send', unknown>> = {}) {
  const thread = {
    attachFile: vi.fn(async (f: File) => ({ id: 41, name: f.name, mime: f.type, kind: f.type.startsWith('image/') ? 'image' : 'text', size: f.size })),
    send: vi.fn(async () => true),
    interrupt: vi.fn(),
    ...over,
  }
  const d = { sending: false, busy: false, problem: null, notice: null } as unknown as DeskSnapshot
  render(<Composer thread={thread as unknown as DeskThread} workspaceId={1} d={d} disabled={false} topicId={null} onSend={() => {}} />)
  return thread
}

const file = (name: string, type: string, body = 'hello') => new File([body], name, { type })

describe('refusal', () => {
  it('names why a file cannot go', () => {
    const none = { count: 0, bytes: 0 }
    expect(refusal({ name: 'a.log', size: 10 }, none)).toBeNull()
    expect(refusal({ name: 'setup.exe', size: 10 }, none)).toMatch(/program, script or archive/)
    expect(refusal({ name: 'big.log', size: MAX_FILE_BYTES + 1 }, none)).toMatch(/10 MiB/)
    expect(refusal({ name: 'a.log', size: 10 }, { count: 5, bytes: 0 })).toMatch(/at most 5/)
    expect(refusal({ name: 'e.txt', size: 0 }, none)).toMatch(/empty/)
    expect(sizeText(2048)).toBe('2 KiB')
  })
})

describe('Composer attachments', () => {
  it('uploads a pasted file, shows it, and sends its id with the words', async () => {
    const thread = rig()
    const box = screen.getByLabelText('Message to the front desk')
    fireEvent.paste(box, { clipboardData: { files: [file('build.log', 'text/plain')] } })
    await screen.findByText(/build\.log/)
    await waitFor(() => expect((screen.getByRole('button', { name: 'Send' }) as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(thread.send).toHaveBeenCalledWith('', null, undefined, undefined, [41]))
    await waitFor(() => expect(screen.queryByText(/build\.log/)).toBeNull())
  })

  it('takes a dropped file, and holds Send while it uploads', async () => {
    let finish: (v: unknown) => void = () => {}
    const thread = rig({ attachFile: vi.fn(() => new Promise((r) => (finish = r))) })
    const form = screen.getByLabelText('Message to the front desk').closest('form') as HTMLElement
    fireEvent.drop(form, { dataTransfer: { files: [file('shot.png', 'image/png')], types: ['Files'] } })
    await screen.findByText(/uploading/)
    fireEvent.change(screen.getByLabelText('Message to the front desk'), { target: { value: 'look' } })
    expect((screen.getByRole('button', { name: 'Send' }) as HTMLButtonElement).disabled).toBe(true)
    finish({ id: 3, name: 'shot.png', mime: 'image/png', kind: 'image', size: 5 })
    await waitFor(() => expect((screen.getByRole('button', { name: 'Send' }) as HTMLButtonElement).disabled).toBe(false))
    expect(thread.attachFile).toHaveBeenCalledTimes(1)
  })

  it('refuses an executable in words, without uploading, and lets it be removed', async () => {
    const thread = rig()
    const form = screen.getByLabelText('Message to the front desk').closest('form') as HTMLElement
    fireEvent.drop(form, { dataTransfer: { files: [file('setup.exe', 'application/octet-stream')], types: ['Files'] } })
    expect((await screen.findByRole('alert')).textContent).toMatch(/program, script or archive/)
    expect(thread.attachFile).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
    expect(screen.queryByText(/setup\.exe/)).toBeNull()
  })

  it('shows the daemon\'s refusal of an upload', async () => {
    rig({ attachFile: vi.fn(async () => { throw new Error('a.log is over the limit') }) })
    fireEvent.paste(screen.getByLabelText('Message to the front desk'), { clipboardData: { files: [file('a.log', 'text/plain')] } })
    expect((await screen.findByRole('alert')).textContent).toContain('over the limit')
  })
})

describe('AttachmentList', () => {
  it('shows an image inline and any other file as a chip with name and size', () => {
    render(
      <AttachmentList files={[
        { id: 1, name: 'shot.png', mime: 'image/png', kind: 'image', size: 2048 },
        { id: 2, name: 'ci.log', mime: 'text/plain', kind: 'text', size: 3000 },
      ]} />,
    )
    expect(screen.getByRole('img', { name: 'shot.png' }).getAttribute('src')).toBe('/v1/desk/attachments/1')
    const chip = screen.getByText('ci.log').closest('a') as HTMLAnchorElement
    expect(chip.getAttribute('href')).toBe('/v1/desk/attachments/2')
    expect(chip.textContent).toContain('3 KiB')
  })

  it('appears under a person\'s message in the thread', () => {
    render(
      <Thread items={[{ kind: 'you', key: 'k', age: 'now', children: 'see this', attachments: <AttachmentList files={[{ id: 2, name: 'ci.log', mime: 'text/plain', kind: 'text', size: 10 }]} /> }]} />,
    )
    expect(screen.getByText('see this')).toBeTruthy()
    expect(screen.getByText('ci.log')).toBeTruthy()
  })
})
