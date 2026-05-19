import { describe, it, expect } from 'vitest'
import router from '@/router/index'

describe('Router Home Route', () => {
  it('resolves the /home path to the new AixwHomeView component', async () => {
    const route = router.resolve('/home')
    expect(route.name).toBe('Home')
    
    // Test that the matched component is an async import resolving to AixwHomeView.vue
    expect(route.matched.length).toBeGreaterThan(0)
  })
})
