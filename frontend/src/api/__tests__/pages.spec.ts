import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, del } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  del: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, delete: del }
}))

import { pagesAPI as exportedPagesAPI } from '@/api'
import { adminAPI } from '@/api/admin'
import { adminPagesAPI } from '@/api/admin/pages'
import { pagesAPI } from '@/api/pages'

describe('HTML page API clients', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    del.mockReset()
  })

  it('reads user and admin HTML source as text', async () => {
    get.mockResolvedValue({ data: '<h1>About</h1>' })

    await expect(pagesAPI.getHtml('about page')).resolves.toBe('<h1>About</h1>')
    await expect(adminPagesAPI.getHtml('about page')).resolves.toBe('<h1>About</h1>')

    expect(get).toHaveBeenNthCalledWith(1, '/pages/about%20page/html', {
      responseType: 'text'
    })
    expect(get).toHaveBeenNthCalledWith(2, '/admin/pages/about%20page/html', {
      responseType: 'text'
    })
  })

  it('writes and deletes admin HTML source', async () => {
    put.mockResolvedValue({ data: { slug: 'about' } })
    del.mockResolvedValue({ data: { slug: 'about' } })

    await adminPagesAPI.putHtml('about', '<h1>About</h1>')
    await adminPagesAPI.deleteHtml('about')

    expect(put).toHaveBeenCalledWith('/admin/pages/about/html', '<h1>About</h1>', {
      headers: { 'Content-Type': 'text/html; charset=utf-8' }
    })
    expect(del).toHaveBeenCalledWith('/admin/pages/about/html')
  })

  it('exposes page clients from the public barrels', () => {
    expect(exportedPagesAPI).toBe(pagesAPI)
    expect(adminAPI.pages).toBe(adminPagesAPI)
  })
})
