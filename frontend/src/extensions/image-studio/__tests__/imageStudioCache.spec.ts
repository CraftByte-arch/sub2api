import { beforeEach, describe, expect, it } from 'vitest'

import {
  clearImageStudioAssetCache,
  deleteImageStudioAssetCache,
  getCachedImageStudioAsset,
  putImageStudioAssetCache
} from '../imageStudioCache'

describe('imageStudioCache', () => {
  beforeEach(async () => {
    await clearImageStudioAssetCache()
  })

  it('persists thumbnail and original blobs by job id', async () => {
    const thumbnail = new Blob(['thumb'], { type: 'image/jpeg' })
    const original = new Blob(['original'], { type: 'image/png' })

    await putImageStudioAssetCache({ jobId: 10, kind: 'thumbnail', blob: thumbnail })
    await putImageStudioAssetCache({ jobId: 10, kind: 'original', blob: original })

    const cachedThumbnail = await getCachedImageStudioAsset(10, 'thumbnail')
    const cachedOriginal = await getCachedImageStudioAsset(10, 'original')

    expect(cachedThumbnail?.blob.type).toBe('image/jpeg')
    expect(cachedThumbnail?.sizeBytes).toBe(thumbnail.size)
    expect(cachedOriginal?.blob.type).toBe('image/png')
    expect(cachedOriginal?.sizeBytes).toBe(original.size)
  })

  it('deletes cached assets for one job without deleting other jobs', async () => {
    await putImageStudioAssetCache({ jobId: 10, kind: 'thumbnail', blob: new Blob(['a']) })
    await putImageStudioAssetCache({ jobId: 10, kind: 'original', blob: new Blob(['b']) })
    await putImageStudioAssetCache({ jobId: 11, kind: 'thumbnail', blob: new Blob(['c']) })

    await deleteImageStudioAssetCache(10)

    expect(await getCachedImageStudioAsset(10, 'thumbnail')).toBeNull()
    expect(await getCachedImageStudioAsset(10, 'original')).toBeNull()
    expect(await getCachedImageStudioAsset(11, 'thumbnail')).not.toBeNull()
  })

  it('clears all cached image studio assets', async () => {
    await putImageStudioAssetCache({ jobId: 10, kind: 'thumbnail', blob: new Blob(['a']) })
    await putImageStudioAssetCache({ jobId: 11, kind: 'original', blob: new Blob(['b']) })

    const removed = await clearImageStudioAssetCache()

    expect(removed).toBe(2)
    expect(await getCachedImageStudioAsset(10, 'thumbnail')).toBeNull()
    expect(await getCachedImageStudioAsset(11, 'original')).toBeNull()
  })
})
