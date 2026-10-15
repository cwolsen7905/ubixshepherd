import { describe, expect, it } from 'vitest'
import { taskLine, taskText } from './task'

const prompt = 'You are working for uBixShepherd in lane x of repo y.\nWork only inside this directory: /w.\n\nTask:\nFix the flaky tests\nthen build the page\n'

describe('a run’s task', () => {
  it('drops the prompt’s preamble and keeps the task', () => {
    expect(taskText(prompt)).toBe('Fix the flaky tests\nthen build the page')
    expect(taskLine(prompt)).toBe('Fix the flaky tests')
  })
  it('leaves a bare task as it is', () => {
    expect(taskLine('  rename the helper \nmore')).toBe('rename the helper')
    expect(taskText('')).toBe('')
  })
  it('reads a task that opens the text', () => {
    expect(taskLine('Task:\nwrite docs')).toBe('write docs')
  })
  it('uses the first marker, so a task that quotes one is whole', () => {
    expect(taskText('pre\nTask:\nfirst\nTask:\nsecond')).toBe('first\nTask:\nsecond')
  })
  it('shows nothing for a lone preamble line rather than the boilerplate', () => {
    expect(taskLine('You are working for uBixShepherd in lane x of repo y.')).toBe('')
  })
})
