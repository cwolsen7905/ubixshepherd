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

  it('list: a compact table with glyph, run, lane meta, result with duration, and cost', () => {
    render(<RunsTable runs={runs} variant="list" />)
    const table = screen.getByRole('table')
    expect(table).toHaveAttribute('data-component', 'RunsTable')
    expect(table).toHaveAttribute('data-variant', 'list')
    const rows = within(table).getAllByRole('row').slice(1)
    expect(rows).toHaveLength(2)
    expect(screen.getByRole('link', { name: /Run 12/ })).toHaveAttribute('href', '#/runs/12/log')
    expect(screen.getByTitle('Run 12 failed')).toBeInTheDocument()
    expect(rows[0]?.querySelector('.sh-runs-lane')).toHaveTextContent(/claude-sonnet/)
    expect(rows[0]?.querySelector('.sh-runs-result')).toHaveTextContent(/failed/)
    expect(screen.getByText('(exit 2)')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Run 11/ })).toBeInTheDocument()
  })

  it('says so with no runs', () => {
    render(<RunsTable runs={[]} />)
    expect(screen.getByText('No runs yet.')).toBeInTheDocument()
  })
})
