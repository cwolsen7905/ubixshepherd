import { render } from '@testing-library/react'
import { ContextChart } from '../components/ContextChart'

describe('ContextChart', () => {
  it('prints no count, no NaN and no undefined for a row without over_200k', () => {
    const { container } = render(
      <ContextChart sections={[{ title: 'Agent runs, by agent and model', rows: [{ key: 'claude sonnet', label: 'claude sonnet', peak: 120_000, window: 0, measured: 0 }] }]} />,
    )
    expect(container.textContent).toContain('120k')
    expect(container.textContent).not.toMatch(/NaN|undefined|over 200k/)
  })

  it('still says how many were over 200k when it knows', () => {
    const { container } = render(
      <ContextChart sections={[{ title: 't', rows: [{ key: 'a', label: 'a', peak: 250_000, window: 1_000_000, over: 2, measured: 3 }] }]} />,
    )
    expect(container.textContent).toContain('2 over 200k')
  })
})
