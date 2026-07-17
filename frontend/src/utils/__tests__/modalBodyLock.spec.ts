import { afterEach, describe, expect, it } from 'vitest'
import { acquireModalBodyLock, releaseModalBodyLock } from '../modalBodyLock'

afterEach(() => {
  releaseModalBodyLock()
  releaseModalBodyLock()
  document.body.style.overflow = ''
})

describe('modalBodyLock', () => {
  it('keeps the body locked until every consumer releases it', () => {
    document.body.style.overflow = 'scroll'

    acquireModalBodyLock()
    acquireModalBodyLock()
    expect(document.body.style.overflow).toBe('hidden')

    releaseModalBodyLock()
    expect(document.body.style.overflow).toBe('hidden')

    releaseModalBodyLock()
    expect(document.body.style.overflow).toBe('scroll')
  })
})
