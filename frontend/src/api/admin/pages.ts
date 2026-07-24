import { apiClient } from '../client'

export const adminPagesAPI = {
  async getHtml(slug: string): Promise<string> {
    const { data } = await apiClient.get<string>(
      `/admin/pages/${encodeURIComponent(slug)}/html`,
      { responseType: 'text' }
    )
    return data
  },

  async putHtml(slug: string, source: string): Promise<void> {
    await apiClient.put(`/admin/pages/${encodeURIComponent(slug)}/html`, source, {
      headers: { 'Content-Type': 'text/html; charset=utf-8' }
    })
  },

  async deleteHtml(slug: string): Promise<void> {
    await apiClient.delete(`/admin/pages/${encodeURIComponent(slug)}/html`)
  }
}

export default adminPagesAPI
