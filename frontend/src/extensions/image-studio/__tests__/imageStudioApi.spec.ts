import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '@/api'

vi.mock('@/api', () => ({
  apiClient: {
    post: vi.fn(),
    get: vi.fn(),
    delete: vi.fn()
  }
}))

import {
  createImageStudioJob,
  deleteImageStudioJob,
  getImageStudioJobStats,
  getImageStudioJob,
  fetchImageStudioOriginal,
  fetchImageStudioThumbnail,
  listGatewayModels,
  sendImagesEditRequest,
  sendImagesGenerationRequest,
  sendPromptPolishRequest,
  sendResponsesImageRequest
} from '../imageStudioApi'

describe('imageStudioApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.mocked(apiClient.post).mockReset()
    vi.mocked(apiClient.get).mockReset()
    vi.mocked(apiClient.delete).mockReset()
  })

  it('sends Responses requests with the selected API key', async () => {
    const fetchMock = mockSseFetch([
      'data: {"type":"response.output_item.done","item":{"id":"ig_1","type":"image_generation_call","result":"ZmFrZQ==","output_format":"webp"}}',
      'data: [DONE]'
    ])

    const response = await sendResponsesImageRequest({
      apiKey: 'sk-user',
      body: { model: 'gpt-5.4', input: 'draw', stream: true }
    })

    expect(fetchMock).toHaveBeenCalledWith('/v1/responses', expect.objectContaining({
      method: 'POST',
      headers: {
        Authorization: 'Bearer sk-user',
        Accept: 'text/event-stream',
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ model: 'gpt-5.4', input: 'draw', stream: true })
    }))
    expect(response).toEqual({
      output: [{ id: 'ig_1', type: 'image_generation_call', result: 'ZmFrZQ==', output_format: 'webp' }]
    })
  })

  it('ignores partial images and deduplicates final Responses image events', async () => {
    mockSseFetch([
      'data: {"type":"response.image_generation_call.partial_image","item_id":"ig_1","partial_image_b64":"cGFydGlhbA==","output_format":"jpeg"}',
      'data: {"type":"response.output_item.done","item":{"id":"ig_1","type":"image_generation_call","result":"ZmluYWw=","output_format":"jpeg"}}',
      'data: {"type":"response.completed","response":{"output":[{"id":"ig_1","type":"image_generation_call","result":"ZmluYWw=","output_format":"jpeg"},{"id":"msg_1","type":"message","content":[]}]}}',
      'data: [DONE]'
    ])

    const response = await sendResponsesImageRequest({
      apiKey: 'sk-user',
      body: { model: 'gpt-5.4', input: 'draw', stream: true }
    })

    expect(response).toEqual({
      output: [{ id: 'ig_1', type: 'image_generation_call', result: 'ZmluYWw=', output_format: 'jpeg' }]
    })
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

  it('sends prompt polish requests through streaming Responses', async () => {
    const fetchMock = mockSseFetch([
      'data: {"type":"response.output_text.delta","text":"润色后的"}',
      'data: {"type":"response.output_text.done","text":"提示词"}',
      'data: [DONE]'
    ])

    const response = await sendPromptPolishRequest({
      apiKey: 'sk-user',
      body: { model: 'gpt-5.5', input: 'draw', stream: true }
    })

    expect(fetchMock).toHaveBeenCalledWith('/v1/responses', expect.objectContaining({
      method: 'POST',
      headers: {
        Authorization: 'Bearer sk-user',
        Accept: 'text/event-stream',
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ model: 'gpt-5.5', input: 'draw', stream: true })
    }))
    expect(response).toEqual({
      output: [],
      output_text: '润色后的提示词'
    })
  })

  it('lists gateway models with the supplied API key', async () => {
    const fetchMock = mockJsonFetch({
      data: [
        { id: 'gpt-5.6-sol' },
        { id: 'gpt-5.6-terra' },
        { id: 'gpt-5.6-sol' },
        { id: '' }
      ]
    })

    await expect(listGatewayModels('sk-polish')).resolves.toEqual([
      'gpt-5.6-sol',
      'gpt-5.6-terra'
    ])
    expect(fetchMock).toHaveBeenCalledWith('/v1/models', {
      headers: { Authorization: 'Bearer sk-polish' }
    })
  })

  it('propagates gateway model-list errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      text: vi.fn().mockResolvedValue(JSON.stringify({
        error: { message: 'model list unavailable' }
      }))
    }))

    await expect(listGatewayModels('sk-polish')).rejects.toThrow('model list unavailable')
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

  it('creates image studio jobs through the site API', async () => {
    vi.mocked(apiClient.post).mockResolvedValue({
      data: {
        id: 12,
        mode: 'generate',
        status: 'queued',
        attempt_count: 1,
        max_attempts: 3,
        next_attempt_at: '2026-06-11T00:00:10Z',
        prompt: 'draw',
        model: 'gpt-image-2',
        size: '1024x1024',
        output_format: 'png',
        queued_at: '2026-06-11T00:00:00Z'
      }
    })

    const job = await createImageStudioJob({
      apiKeyId: 9,
      mode: 'generate',
      prompt: 'draw',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png'
    })

    expect(apiClient.post).toHaveBeenCalledWith('/image-studio/jobs', {
      api_key_id: 9,
      mode: 'generate',
      prompt: 'draw',
      model: 'gpt-image-2',
      size: '1024x1024',
      output_format: 'png',
      quality: undefined,
      background: undefined,
      style: undefined,
      moderation: undefined,
      input_fidelity: undefined,
      output_compression: undefined,
      image_data_urls: undefined,
      mask_data_url: undefined
    })
    expect(job).toMatchObject({
      id: 12,
      status: 'queued',
      outputFormat: 'png',
      attemptCount: 1,
      maxAttempts: 3,
      nextAttemptAt: '2026-06-11T00:00:10Z'
    })
  })

  it('deletes image studio jobs through the site API', async () => {
    vi.mocked(apiClient.delete).mockResolvedValue({ data: null })

    await deleteImageStudioJob(42)

    expect(apiClient.delete).toHaveBeenCalledWith('/image-studio/jobs/42')
  })

  it('loads image studio job stats through the site API', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({
      data: {
        pending_count: 2,
        failed_count: 3
      }
    })

    const stats = await getImageStudioJobStats()

    expect(apiClient.get).toHaveBeenCalledWith('/image-studio/jobs/stats')
    expect(stats).toEqual({
      pendingCount: 2,
      failedCount: 3
    })
  })

  it('normalizes retry metadata from job detail responses', async () => {
    vi.mocked(apiClient.get).mockResolvedValue({
      data: {
        id: 44,
        mode: 'generate',
        status: 'running',
        attempt_count: 2,
        max_attempts: 3,
        prompt: 'draw again',
        model: 'gpt-image-2',
        size: '1024x1024',
        output_format: 'webp',
        queued_at: '2026-06-11T00:00:00Z'
      }
    })

    const job = await getImageStudioJob(44)

    expect(job).toMatchObject({
      id: 44,
      status: 'running',
      attemptCount: 2,
      maxAttempts: 3,
      outputFormat: 'webp'
    })
  })

  it('fetches original images with current auth token', async () => {
    localStorage.setItem('auth_token', 'jwt-token')
    const blob = new Blob(['img'], { type: 'image/png' })
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      blob: vi.fn().mockResolvedValue(blob)
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await fetchImageStudioOriginal(42)

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/image-studio/jobs/42/original', {
      method: 'GET',
      headers: { Authorization: 'Bearer jwt-token' },
      credentials: 'include'
    })
    expect(result).toBe(blob)
  })

  it('fetches thumbnails with current auth token and cookies', async () => {
    localStorage.setItem('auth_token', 'jwt-token')
    const blob = new Blob(['thumb'], { type: 'image/jpeg' })
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      blob: vi.fn().mockResolvedValue(blob)
    })
    vi.stubGlobal('fetch', fetchMock)

    const result = await fetchImageStudioThumbnail(42)

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/image-studio/jobs/42/thumbnail', {
      method: 'GET',
      headers: { Authorization: 'Bearer jwt-token' },
      credentials: 'include'
    })
    expect(result).toBe(blob)
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

function mockSseFetch(lines: string[]) {
  const encoder = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode(`${lines.join('\n\n')}\n\n`))
      controller.close()
    }
  })
  const fetchMock = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    headers: new Headers({ 'content-type': 'text/event-stream' }),
    body,
    text: vi.fn()
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}
