import { describe, expect, it } from 'vitest'
import { prefillEmptyLoginFields } from '../demoCredentials'

const credentials = {
  email: 'demo@example.com',
  password: 'demo-password',
}

describe('prefillEmptyLoginFields', () => {
  it('prefills empty login fields from demo credentials', () => {
    expect(prefillEmptyLoginFields({ email: '', password: '' }, credentials)).toEqual(credentials)
  })

  it('does not overwrite text the visitor already entered', () => {
    expect(prefillEmptyLoginFields({
      email: 'visitor@example.com',
      password: 'visitor-password',
    }, credentials)).toEqual({
      email: 'visitor@example.com',
      password: 'visitor-password',
    })
  })

  it('prefills each empty field independently', () => {
    expect(prefillEmptyLoginFields({ email: 'visitor@example.com', password: '' }, credentials)).toEqual({
      email: 'visitor@example.com',
      password: 'demo-password',
    })
  })
})
