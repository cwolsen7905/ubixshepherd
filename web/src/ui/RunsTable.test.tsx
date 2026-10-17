import { render, screen, within } from '@testing-library/react'
import { run } from '../test/fixtures'
import { RunsTable } from './RunsTable'

const runs = [
  run({ id: 12, state: 'failed', exit_code: 2, parent: 11, outside: ['x.go'], model: 'claude-sonnet' }),
  run({ id: 11 }),
]

describe('RunsTable', () => {
  it('full: every column, with the parent, exit code and outside-scope marks', () => {
    render(<RunsTable runs={runs} variant="full" />)
    const table = screen.getByRole('table')
    expect(table).toHaveAttribute('data-component', 'RunsTable')
    expect(table).toHaveAttribute('data-variant', 'full')
    for (const h of ['Model', 'Took', 'Cost']) expect(within(table).getByRole('columnheader', { name: h })).toBeInTheDocument()
    expect(screen.getByText('(exit 2)')).toBeInTheDocument()
    expect(screen.getByText('outside scope')).toBeInTheDocument()
    expect(screen.getByTitle('continues run 11')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '12' })).toHaveAttribute('href', '#/runs/12/log')
  })

  it('compact: drops the model, time taken and cost', () => {
    render(<RunsTable runs={runs} variant="compact" />)
    for (const h of ['Model', 'Took', 'Cost']) expect(screen.queryByRole('columnheader', { name: h })).toBeNull()
    expect(screen.getByRole('columnheader', { name: 'Agent' })).toBeInTheDocument()
  })

  it('list: one line per run, with its state in words', () => {
    render(<RunsTable runs={runs} variant="list" />)
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.getByRole('link', { name: /Run 12/ })).toBeInTheDocument()
    expect(screen.getByText('failed')).toBeInTheDocument()
  })

  it('says so with no runs', () => {
    render(<RunsTable runs={[]} />)
    expect(screen.getByText('No runs yet.')).toBeInTheDocument()
  })
})
