const REFERENCE_WEBP_QUALITY = 0.72

export type ImageReferenceCompressionErrorCode =
  | 'canvasUnsupported'
  | 'decodeFailed'
  | 'encodeFailed'
  | 'invalidDimensions'

export class ImageReferenceCompressionError extends Error {
  constructor(public readonly code: ImageReferenceCompressionErrorCode) {
    super(code)
    this.name = 'ImageReferenceCompressionError'
  }
}

interface DecodedImage {
  source: CanvasImageSource
  width: number
  height: number
  release: () => void
}

export async function compressImageReferences(files: File[]): Promise<File[]> {
  const compressed: File[] = []
  for (let index = 0; index < files.length; index += 1) {
    compressed.push(await compressImageReference(files[index], index))
  }
  return compressed
}

async function compressImageReference(file: File, index: number): Promise<File> {
  const decoded = await decodeImage(file)
  try {
    if (decoded.width <= 0 || decoded.height <= 0) {
      throw new ImageReferenceCompressionError('invalidDimensions')
    }
    const canvas = document.createElement('canvas')
    canvas.width = decoded.width
    canvas.height = decoded.height
    const context = canvas.getContext('2d')
    if (!context) throw new ImageReferenceCompressionError('canvasUnsupported')
    context.drawImage(decoded.source, 0, 0, decoded.width, decoded.height)
    const blob = await canvasToBlob(canvas)
    return new File([blob], `image-studio-reference-${index + 1}.webp`, {
      type: 'image/webp',
      lastModified: 0
    })
  } finally {
    decoded.release()
  }
}

async function decodeImage(file: File): Promise<DecodedImage> {
  if (typeof createImageBitmap === 'function') {
    let bitmap: ImageBitmap
    try {
      bitmap = await createImageBitmap(file)
    } catch {
      throw new ImageReferenceCompressionError('decodeFailed')
    }
    return {
      source: bitmap,
      width: bitmap.width,
      height: bitmap.height,
      release: () => bitmap.close()
    }
  }

  if (typeof Image === 'undefined' || typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') {
    throw new ImageReferenceCompressionError('decodeFailed')
  }
  let objectUrl: string
  try {
    objectUrl = URL.createObjectURL(file)
  } catch {
    throw new ImageReferenceCompressionError('decodeFailed')
  }
  return new Promise((resolve, reject) => {
    let revoked = false
    const revoke = () => {
      if (revoked) return
      revoked = true
      URL.revokeObjectURL(objectUrl)
    }
    try {
      const image = new Image()
      image.onload = () => {
        revoke()
        resolve({
          source: image,
          width: image.naturalWidth,
          height: image.naturalHeight,
          release: () => { image.src = '' }
        })
      }
      image.onerror = () => {
        revoke()
        reject(new ImageReferenceCompressionError('decodeFailed'))
      }
      image.src = objectUrl
    } catch {
      revoke()
      reject(new ImageReferenceCompressionError('decodeFailed'))
    }
  })
}

function canvasToBlob(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob?.type.toLowerCase() === 'image/webp') {
        resolve(blob)
      } else {
        reject(new ImageReferenceCompressionError('encodeFailed'))
      }
    }, 'image/webp', REFERENCE_WEBP_QUALITY)
  })
}
