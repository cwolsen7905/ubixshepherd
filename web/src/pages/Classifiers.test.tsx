import { render, screen } from '@testing-library/react'
import { ClassifiersPage } from './Classifiers'
import { GuardrailsPage } from '../pages/Guardrails'

describe('placeholder pages', () => {
  it('says Classifiers is not built yet', () => {
    render(<ClassifiersPage />)
    expect(screen.getByRole('heading', { name: 'Classifiers' })).toBeInTheDocument()
    expect(screen.getByText('Classifiers are not built yet')).toBeInTheDocument()
  })

  it('says Guardrails is not built yet', () => {
    render(<GuardrailsPage />)
    expect(screen.getByRole('heading', { name: 'Guardrails' })).toBeInTheDocument()
    expect(screen.getByText('Guardrails are not built yet')).toBeInTheDocument()
  })
})
