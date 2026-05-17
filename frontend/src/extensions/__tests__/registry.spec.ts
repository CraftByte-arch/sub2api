import { describe, expect, it } from 'vitest'

import { extensionRoutes } from '../index'

describe('extension route registry', () => {
  it('registers image studio as an authenticated user route', () => {
    const route = extensionRoutes.find((item) => item.path === '/image-studio')

    expect(route?.name).toBe('ImageStudio')
    expect(route?.meta?.requiresAuth).toBe(true)
    expect(route?.meta?.requiresAdmin).toBe(false)
    expect(route?.meta?.titleKey).toBe('imageStudio.title')
  })
})
