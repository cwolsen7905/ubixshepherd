import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Markdown } from '../conversation/Markdown'
import { CopyButton } from './CopyButton'

describe('CopyButton', () => {
  it('copies the exact text through the clipboard and says so', async () => {
    const user = userEvent.setup()
    const write = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    render(<CopyButton text="shepherd repo add ubixvault" />)
    await user.click(screen.getByRole('button', { name: 'Copy code' }))
    expect(write).toHaveBeenCalledWith('shepherd repo add ubixvault')
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()
  })

  it('falls back to execCommand outside a secure context', async () => {
    const user = userEvent.setup()
    vi.spyOn(navigator, 'clipboard', 'get').mockReturnValue(undefined as unknown as Clipboard)
    const exec = vi.fn(() => true)
    Object.defineProperty(document, 'execCommand', { value: exec, configurable: true })
    render(<CopyButton text="echo hi" />)
    await user.click(screen.getByRole('button', { name: 'Copy code' }))
    expect(exec).toHaveBeenCalledWith('copy')
    expect(document.querySelector('textarea')).toBeNull()
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()
  })
})

describe('Markdown code blocks', () => {
  it('puts a button on a fenced block, copying its raw text, and none on inline code', async () => {
    const user = userEvent.setup()
    const write = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    render(<Markdown text={'Run `inline` first:\n\n```sh\nshepherd repo add ubixvault\nshepherd status\n```\n'} />)
    expect(screen.getAllByRole('button', { name: 'Copy code' })).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: 'Copy code' }))
    expect(write).toHaveBeenCalledWith('shepherd repo add ubixvault\nshepherd status')
  })

  it('has no button without a fenced block', () => {
    render(<Markdown text="Just `inline` code." />)
    expect(screen.queryByRole('button')).toBeNull()
  })
})
