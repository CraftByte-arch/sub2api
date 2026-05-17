import { describe, expect, it } from 'vitest'

import { extractImageStudioOutputs } from '../output'

describe('extractImageStudioOutputs', () => {
  it('extracts Images API base64 outputs', () => {
    const outputs = extractImageStudioOutputs({
      data: [
        {
          b64_json: 'ZmFrZQ==',
          revised_prompt: 'a revised prompt',
          output_format: 'png'
        }
      ]
    })

    expect(outputs).toHaveLength(1)
    expect(outputs[0]).toMatchObject({
      kind: 'b64_json',
      src: 'data:image/png;base64,ZmFrZQ==',
      revisedPrompt: 'a revised prompt',
      mimeType: 'image/png'
    })
  })

  it('extracts Images API URL outputs', () => {
    const outputs = extractImageStudioOutputs({
      data: [{ url: 'https://example.com/image.webp' }]
    })

    expect(outputs).toHaveLength(1)
    expect(outputs[0]).toMatchObject({
      kind: 'url',
      src: 'https://example.com/image.webp'
    })
  })

  it('extracts Responses image_generation_call result outputs', () => {
    const outputs = extractImageStudioOutputs({
      output: [
        {
          id: 'ig_123',
          type: 'image_generation_call',
          result: 'ZmFrZQ==',
          revised_prompt: 'responses revised',
          output_format: 'webp'
        }
      ]
    })

    expect(outputs).toHaveLength(1)
    expect(outputs[0]).toMatchObject({
      id: 'ig_123',
      kind: 'b64_json',
      src: 'data:image/webp;base64,ZmFrZQ==',
      revisedPrompt: 'responses revised'
    })
  })

  it('extracts aggregated stream image_generation completed outputs', () => {
    const outputs = extractImageStudioOutputs({
      images: [
        {
          id: 'ig_stream',
          b64_json: 'ZmFrZQ==',
          mime_type: 'image/png'
        }
      ]
    })

    expect(outputs).toHaveLength(1)
    expect(outputs[0]).toMatchObject({
      id: 'ig_stream',
      src: 'data:image/png;base64,ZmFrZQ=='
    })
  })
})
