import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  sendImagesEditRequest,
  sendImagesGenerationRequest,
  sendPromptPolishRequest,
  sendResponsesImageRequest
} from '../imageStudioApi'

describe('imageStudioApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
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
