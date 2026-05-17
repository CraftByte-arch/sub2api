import { describe, expect, it } from 'vitest'

import {
  buildImagesEditFormData,
  buildImagesGenerationPayload,
  buildResponsesEditPayload,
  buildResponsesGenerationPayload,
  parseAdvancedJson
} from '../payload'

describe('image studio payload builders', () => {
  it('builds a Responses image generation payload with the native tool', () => {
    const payload = buildResponsesGenerationPayload({
      model: 'gpt-5.4',
      prompt: 'a quiet tea house',
      size: '1024x1024',
      quality: 'high',
      outputFormat: 'png',
      count: 2,
      advancedParams: {
        tool: { background: 'transparent' },
        metadata: { source: 'image-studio' }
      }
    })

    expect(payload).toMatchObject({
      model: 'gpt-5.4',
      input: 'a quiet tea house',
      metadata: { source: 'image-studio' }
    })
    expect(payload.tools).toEqual([
      {
        type: 'image_generation',
        size: '1024x1024',
        quality: 'high',
        output_format: 'png',
        n: 2,
        background: 'transparent',
        partial_images: 1
      }
    ])
  })

  it('builds an Images API generation payload with common controls', () => {
    const payload = buildImagesGenerationPayload({
      model: 'gpt-image-2',
      prompt: 'a ceramic lamp',
      size: '1536x1024',
      quality: 'medium',
      outputFormat: 'webp',
      count: 1,
      advancedParams: { user: 'demo-user' }
    })

    expect(payload).toEqual({
      model: 'gpt-image-2',
      prompt: 'a ceramic lamp',
      size: '1536x1024',
      quality: 'medium',
      output_format: 'webp',
      n: 1,
      user: 'demo-user'
    })
  })

  it('builds a Responses edit payload with input image data URLs', async () => {
    const image = new File(['source'], 'source.png', { type: 'image/png' })
    const mask = new File(['mask'], 'mask.png', { type: 'image/png' })

    const payload = await buildResponsesEditPayload({
      model: 'gpt-5.4',
      prompt: 'replace the background',
      image,
      mask,
      size: '1024x1024',
      count: 1,
      advancedParams: {}
    })

    expect(payload.model).toBe('gpt-5.4')
    expect(payload.tools).toEqual([{ type: 'image_generation', size: '1024x1024', n: 1, partial_images: 1 }])
    expect(JSON.stringify(payload.input)).toContain('data:image/png;base64,')
    expect(JSON.stringify(payload.input)).toContain('input_image')
    expect(JSON.stringify(payload.input)).toContain('input_image_mask')
  })

  it('builds an Images API edit FormData payload', () => {
    const image = new File(['source'], 'source.png', { type: 'image/png' })
    const formData = buildImagesEditFormData({
      model: 'gpt-image-2',
      prompt: 'make it brighter',
      image,
      size: '1024x1024',
      outputFormat: 'png',
      count: 1,
      advancedParams: { background: 'opaque' }
    })

    expect(formData.get('model')).toBe('gpt-image-2')
    expect(formData.get('prompt')).toBe('make it brighter')
    expect(formData.get('size')).toBe('1024x1024')
    expect(formData.get('output_format')).toBe('png')
    expect(formData.get('background')).toBe('opaque')
    expect(formData.get('image')).toBe(image)
  })

  it('rejects invalid advanced JSON', () => {
    expect(() => parseAdvancedJson('{"broken":')).toThrow('高级参数 JSON 格式不正确')
  })
})
