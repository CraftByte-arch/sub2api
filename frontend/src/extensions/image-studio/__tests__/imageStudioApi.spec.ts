import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  sendImagesEditRequest,
  sendImagesGenerationRequest,
  sendResponsesImageRequest
} from '../imageStudioApi'

describe('imageStudioApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends Responses requests with the selected API key', async () => {
    const fetchMock = mockJsonFetch({ id: 'resp_1' })

    await sendResponsesImageRequest({
      apiKey: 'sk-user',
      body: { model: 'gpt-5.4', input: 'draw' }
    })

    expect(fetchMock).toHaveBeenCalledWith('/v1/responses', expect.objectContaining({
      method: 'POST',
      headers: {
        Authorization: 'Bearer sk-user',
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ model: 'gpt-5.4', input: 'draw' })
    }))
  })

  it('sends Images API generation requests with the selected API key', async () => {
    const fetchMock = mockJsonFetch({ data: [] })

    await sendImagesGenerationRequest({
      apiKey: 'sk-user',
      body: { model: 'gpt-image-2', prompt: 'draw' }
    })

    expect(fetchMock).toHaveBeenCalledWith('/v1/images/generations', expect.objectContaining({
      method: 'POST',
      headers: {
        Authorization: 'Bearer sk-user',
        'Content-Type': 'application/json'
      }
    }))
  })

  it('sends multipart edit requests without forcing JSON content type', async () => {
    const fetchMock = mockJsonFetch({ data: [] })
    const formData = new FormData()
    formData.append('model', 'gpt-image-2')

    await sendImagesEditRequest({
      apiKey: 'sk-user',
      body: formData
    })

    expect(fetchMock).toHaveBeenCalledWith('/v1/images/edits', expect.objectContaining({
      method: 'POST',
      headers: {
        Authorization: 'Bearer sk-user'
      },
      body: formData
    }))
  })

  it('throws gateway error messages when the response is not ok', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 403,
      text: vi.fn().mockResolvedValue(JSON.stringify({
        error: { message: 'Image generation is not enabled for this group' }
      }))
    }))

    await expect(sendResponsesImageRequest({
      apiKey: 'sk-user',
      body: { model: 'gpt-5.4', input: 'draw' }
    })).rejects.toThrow('Image generation is not enabled for this group')
  })
})

function mockJsonFetch(payload: unknown) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    text: vi.fn().mockResolvedValue(JSON.stringify(payload))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}
