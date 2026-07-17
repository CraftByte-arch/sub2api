import { afterEach, describe, expect, it } from 'vitest'
import { acquireModalBodyLock, lockAppInert, releaseAppInert, releaseModalBodyLock } from '../modalBodyLock'

afterEach(() => {
  releaseModalBodyLock()
  releaseModalBodyLock()
  releaseAppInert()
  releaseAppInert()
  document.body.style.overflow = ''
  document.getElementById('app')?.remove()
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

  it('restores a pre-existing inert attribute after the last release', () => {
    const appRoot = document.createElement('div')
    appRoot.id = 'app'
    appRoot.setAttribute('inert', 'pre-existing')
    document.body.append(appRoot)

    lockAppInert()
    lockAppInert()
    expect(appRoot.getAttribute('inert')).toBe('')

    releaseAppInert()
    expect(appRoot.getAttribute('inert')).toBe('')

    releaseAppInert()
    expect(appRoot.getAttribute('inert')).toBe('pre-existing')
  })
})
