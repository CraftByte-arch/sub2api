import { describe, expect, it } from 'vitest'
import type { RouteLocationRaw } from 'vue-router'
import router from '@/router'

// router/index.ts 只默认导出 router 实例（routes 数组未导出），
// 用 getRoutes() 断言，不触发导航（避免路由守卫依赖）。
// 注意：vue-router 4 的 router.resolve() 不会跟随 redirect（重定向仅在导航时应用），
// 因此重定向断言直接读取路由记录的 redirect 函数并调用，等价验证导航结果。

describe('monitoring routes', () => {
  it('registers /admin/monitoring', () => {
    expect(router.getRoutes().some((route) => route.path === '/admin/monitoring')).toBe(true)
  })

  it.each(['/admin/ttft', '/admin/performance'])('redirects %s to /admin/monitoring preserving query', (path) => {
    const record = router.getRoutes().find((route) => route.path === path)
    expect(record, `route record for ${path}`).toBeDefined()
    expect(typeof record!.redirect).toBe('function')

    const redirect = record!.redirect as (to: { path: string; query: Record<string, string> }) => RouteLocationRaw
    const target = redirect({ path, query: { range: '7d' } })

    expect(typeof target).toBe('object')
    const resolved = router.resolve(target)
    expect(resolved.path).toBe('/admin/monitoring')
    expect(resolved.query.range).toBe('7d')
  })
})
