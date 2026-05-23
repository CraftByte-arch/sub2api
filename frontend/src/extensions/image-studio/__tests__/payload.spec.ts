import { describe, expect, it } from 'vitest'

import {
  buildImagesEditFormData,
  buildImagesGenerationPayload,
  buildResponsesEditPayload,
  buildResponsesGenerationPayload,
  parseAdvancedJson
} from '../payload'

describe('image studio payload builders', () => {
  class MockFileReader {
    result: string | ArrayBuffer | null = null
    onload: ((this: FileReader, ev: ProgressEvent<FileReader>) => any) | null = null
    onerror: ((this: FileReader, ev: ProgressEvent<FileReader>) => any) | null = null

    readAsDataURL(blob: Blob) {
      this.result = `data:${blob.type || 'application/octet-stream'};base64,bW9jaw==`
      this.onload?.call(this as unknown as FileReader, new ProgressEvent('load'))
    }
  }

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
      model: 'gpt-5.5',
      input: [{ role: 'user', content: [{ type: 'input_text', text: 'a quiet tea house' }] }],
      tool_choice: { type: 'image_generation' },
      reasoning: { effort: 'xhigh' },
      store: false,
      stream: true,
      metadata: { source: 'image-studio' }
    })
    expect(payload.tools).toEqual([
      {
        type: 'image_generation',
        model: 'gpt-5.4',
        action: 'generate',
        size: '1024x1024',
        quality: 'high',
        output_format: 'png',
        moderation: 'low',
        background: 'transparent'
      }
    ])
    expect(payload).not.toHaveProperty('output_format')
  })

  it('drops unsupported Responses image tool overrides', () => {
    const payload = buildResponsesGenerationPayload({
      model: 'gpt-image-2',
      prompt: 'three cats',
      size: '1024x1024',
      outputFormat: 'webp',
      count: 1,
      advancedParams: {
        n: 3,
        tool: { n: 4 },
        partial_images: 0
      }
    })

    expect(payload).not.toHaveProperty('n')
    expect(payload).not.toHaveProperty('partial_images')
    expect(payload.tools).toEqual([
      expect.objectContaining({
        type: 'image_generation',
        partial_images: 0
      })
    ])
    expect((payload.tools as Array<Record<string, unknown>>)[0]).not.toHaveProperty('n')
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

  it('keeps the selected output format in Responses image generation payloads', () => {
    const payload = buildResponsesGenerationPayload({
      model: 'gpt-5.4',
      prompt: 'a quiet tea house',
      size: '1024x1024',
      quality: 'high',
      outputFormat: 'webp',
      count: 1,
      advancedParams: {}
    })

    expect(payload.tools).toEqual([
      expect.objectContaining({
        output_format: 'webp'
      })
    ])
  })

  it('builds a Responses edit payload with input image data URLs', async () => {
    const originalFileReader = globalThis.FileReader
    globalThis.FileReader = MockFileReader as unknown as typeof FileReader
    const image = new File(['source'], 'source.png', { type: 'image/png' })
    const secondImage = new File(['source-2'], 'source-2.png', { type: 'image/png' })
    const mask = new File(['mask'], 'mask.png', { type: 'image/png' })

    try {
      const payload = await buildResponsesEditPayload({
        model: 'gpt-image-2',
        prompt: 'replace the background',
        image: [image, secondImage],
        mask,
        size: '1024x1024',
        outputFormat: 'webp',
        count: 1,
        advancedParams: {}
      })

      expect(payload.model).toBe('gpt-5.5')
      expect(payload.tool_choice).toEqual({ type: 'image_generation' })
      expect(payload.stream).toBe(true)
      expect(payload.store).toBe(false)
      expect(payload.tools).toEqual([
        {
          type: 'image_generation',
          model: 'gpt-image-2',
          action: 'edit',
          size: '1024x1024',
          quality: 'auto',
          output_format: 'webp',
          moderation: 'low'
        }
      ])
      const content = (payload.input as Array<{ content: Array<{ type: string }> }>)[0].content
      expect(JSON.stringify(content)).toContain('data:image/png;base64,')
      expect(content.filter((item) => item.type === 'input_image')).toHaveLength(2)
      expect(content.some((item) => item.type === 'input_image_mask')).toBe(true)
    } finally {
      globalThis.FileReader = originalFileReader
    }
  })

  it('builds an Images API edit FormData payload', () => {
    const image = new File(['source'], 'source.png', { type: 'image/png' })
    const secondImage = new File(['source-2'], 'source-2.png', { type: 'image/png' })
    const formData = buildImagesEditFormData({
      model: 'gpt-image-2',
      prompt: 'make it brighter',
      image: [image, secondImage],
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
    expect(formData.getAll('image')).toEqual([image, secondImage])
  })

  it('rejects invalid advanced JSON', () => {
    expect(() => parseAdvancedJson('{"broken":')).toThrow('高级参数 JSON 格式不正确')
  })
})
