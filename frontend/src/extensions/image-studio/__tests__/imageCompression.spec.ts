import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  ImageReferenceCompressionError,
  compressImageReferences
} from '../imageCompression'

describe('compressImageReferences', () => {
  let bitmapClose: ReturnType<typeof vi.fn>
  let drawImage: ReturnType<typeof vi.fn>
  let canvasDimensions: Array<[number, number]>

  beforeEach(() => {
    bitmapClose = vi.fn()
    drawImage = vi.fn()
    canvasDimensions = []
    class ImageBitmapStub {
      width = 640
      height = 360
      close = bitmapClose
    }
    vi.stubGlobal('ImageBitmap', ImageBitmapStub)
    vi.stubGlobal('createImageBitmap', vi.fn(async () => new ImageBitmapStub()))
    HTMLCanvasElement.prototype.getContext = vi.fn(function (this: HTMLCanvasElement) {
      canvasDimensions.push([this.width, this.height])
      return { drawImage }
    }) as any
    HTMLCanvasElement.prototype.toBlob = vi.fn(function (callback: BlobCallback, type?: string, quality?: number) {
      callback(new Blob([`${type}:${quality}`], { type }))
    }) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('preserves pixel dimensions and encodes ordered WebP files at quality 0.72', async () => {
    const files = [
      new File(['first'], '../../ unsafe first.PNG', { type: 'image/png' }),
      new File(['second'], 'second.jpg', { type: 'image/jpeg' })
    ]

    const output = await compressImageReferences(files)

    expect(output.map((file) => ({ name: file.name, type: file.type, lastModified: file.lastModified }))).toEqual([
      { name: 'image-studio-reference-1.webp', type: 'image/webp', lastModified: 0 },
      { name: 'image-studio-reference-2.webp', type: 'image/webp', lastModified: 0 }
    ])
    expect(HTMLCanvasElement.prototype.toBlob).toHaveBeenCalledTimes(2)
    expect(HTMLCanvasElement.prototype.toBlob).toHaveBeenNthCalledWith(1, expect.any(Function), 'image/webp', 0.72)
    expect(drawImage).toHaveBeenCalledWith(expect.any(ImageBitmap), 0, 0, 640, 360)
    expect(canvasDimensions).toEqual([[640, 360], [640, 360]])
    expect(output.every((file) => file.size > 0)).toBe(true)
  })

  it('closes every decoded ImageBitmap when encoding succeeds', async () => {
    await compressImageReferences([new File(['image'], 'one.png', { type: 'image/png' })])

    expect(bitmapClose).toHaveBeenCalledTimes(1)
  })

  it('closes the ImageBitmap when canvas context creation fails', async () => {
    HTMLCanvasElement.prototype.getContext = vi.fn(() => null)

    await expect(compressImageReferences([
      new File(['image'], 'broken.png', { type: 'image/png' })
    ])).rejects.toMatchObject({ code: 'canvasUnsupported' })
    expect(bitmapClose).toHaveBeenCalledTimes(1)
  })

  it('rejects decode and null toBlob failures without returning partial output', async () => {
    vi.mocked(createImageBitmap).mockRejectedValueOnce(new Error('decode failed'))
    await expect(compressImageReferences([
      new File(['broken'], 'broken.png', { type: 'image/png' })
    ])).rejects.toMatchObject({ code: 'decodeFailed' })

    HTMLCanvasElement.prototype.toBlob = vi.fn((callback: BlobCallback) => callback(null)) as any
    await expect(compressImageReferences([
      new File(['image'], 'valid.png', { type: 'image/png' })
    ])).rejects.toMatchObject({ code: 'encodeFailed' })
    expect(bitmapClose).toHaveBeenCalledTimes(1)
  })

  it('rejects canvas encoders that fall back to a non-WebP blob', async () => {
    HTMLCanvasElement.prototype.toBlob = vi.fn((callback: BlobCallback) => {
      callback(new Blob(['png fallback'], { type: 'image/png' }))
    }) as any

    await expect(compressImageReferences([
      new File(['image'], 'valid.png', { type: 'image/png' })
    ])).rejects.toMatchObject({ code: 'encodeFailed' })
    expect(bitmapClose).toHaveBeenCalledTimes(1)
  })

  it('revokes fallback object URLs on both load and decode error', async () => {
    vi.stubGlobal('createImageBitmap', undefined)
    const revoke = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', {
      value: vi.fn().mockReturnValueOnce('blob:success').mockReturnValueOnce('blob:error'),
      configurable: true
    })
    Object.defineProperty(URL, 'revokeObjectURL', { value: revoke, configurable: true })

    class ImageStub {
      naturalWidth = 320
      naturalHeight = 180
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      set src(value: string) {
        if (!value) return
        queueMicrotask(() => value === 'blob:success' ? this.onload?.() : this.onerror?.())
      }
    }
    vi.stubGlobal('Image', ImageStub)

    await compressImageReferences([new File(['ok'], 'ok.png', { type: 'image/png' })])
    await expect(compressImageReferences([
      new File(['bad'], 'bad.png', { type: 'image/png' })
    ])).rejects.toMatchObject({ code: 'decodeFailed' })

    expect(revoke).toHaveBeenCalledWith('blob:success')
    expect(revoke).toHaveBeenCalledWith('blob:error')
  })

  it.each(['constructor', 'src'] as const)('revokes fallback URLs when the Image %s throws synchronously', async (failure) => {
    vi.stubGlobal('createImageBitmap', undefined)
    const revoke = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', {
      value: vi.fn(() => 'blob:sync-failure'),
      configurable: true
    })
    Object.defineProperty(URL, 'revokeObjectURL', { value: revoke, configurable: true })
    class ThrowingImageStub {
      naturalWidth = 1
      naturalHeight = 1
      onload: (() => void) | null = null
      onerror: (() => void) | null = null
      constructor() {
        if (failure === 'constructor') throw new Error('constructor failed')
      }
      set src(_value: string) {
        if (failure === 'src') throw new Error('src failed')
      }
    }
    vi.stubGlobal('Image', ThrowingImageStub)

    const result = compressImageReferences([new File(['bad'], 'bad.png', { type: 'image/png' })])

    await expect(result).rejects.toBeInstanceOf(ImageReferenceCompressionError)
    await expect(result).rejects.toMatchObject({ code: 'decodeFailed' })
    expect(revoke).toHaveBeenCalledWith('blob:sync-failure')
  })
})
