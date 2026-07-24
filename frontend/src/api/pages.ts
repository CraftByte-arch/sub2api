import { apiClient } from './client'

export const pagesAPI = {
  async getHtml(slug: string): Promise<string> {
    const { data } = await apiClient.get<string>(
      `/pages/${encodeURIComponent(slug)}/html`,
      { responseType: 'text' }
    )
    return data
  }
}

export default pagesAPI
