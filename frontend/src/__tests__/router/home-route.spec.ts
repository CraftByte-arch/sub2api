import { describe, it, expect } from 'vitest'
import router from '@/router/index'

describe('Router Home Route', () => {
  it('resolves the /home path to the runtime homepage selector', async () => {
    const route = router.resolve('/home')
    expect(route.name).toBe('Home')
    expect(route.matched.length).toBeGreaterThan(0)
    expect(String(route.matched[0].components?.default)).toContain('HomeVariantView')
  })
})
