import type { ImageStudioEditInput, ImageStudioGenerationInput } from './types'

export function parseAdvancedJson(value: string): Record<string, any> {
  const trimmed = value.trim()
  if (!trimmed) return {}
  try {
    const parsed = JSON.parse(trimmed)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('advanced params must be an object')
    }
    return parsed
  } catch {
    throw new Error('高级参数 JSON 格式不正确')
  }
}

export function buildResponsesGenerationPayload(input: ImageStudioGenerationInput): Record<string, unknown> {
  const advancedParams = input.advancedParams ?? {}
  const toolExtras = objectValue(advancedParams.tool)
  const payloadExtras = { ...advancedParams }
  delete payloadExtras.tool

  return cleanObject({
    model: input.model,
    input: input.prompt,
    tools: [buildImageGenerationTool(input, toolExtras)],
    ...payloadExtras
  })
}

export function buildImagesGenerationPayload(input: ImageStudioGenerationInput): Record<string, unknown> {
  return cleanObject({
    model: input.model,
    prompt: input.prompt,
    ...commonImageControls(input),
    ...(input.advancedParams ?? {})
  })
}

export async function buildResponsesEditPayload(input: ImageStudioEditInput): Promise<Record<string, unknown>> {
  const advancedParams = input.advancedParams ?? {}
  const toolExtras = objectValue(advancedParams.tool)
  const payloadExtras = { ...advancedParams }
  delete payloadExtras.tool

  const content: Array<Record<string, string>> = [
    { type: 'input_text', text: input.prompt },
    { type: 'input_image', image_url: await fileToDataUrl(input.image) }
  ]

  if (input.mask) {
    content.push({ type: 'input_image_mask', image_url: await fileToDataUrl(input.mask) })
  }

  return cleanObject({
    model: input.model,
    input: [{ role: 'user', content }],
    tools: [buildImageGenerationTool(input, toolExtras)],
    ...payloadExtras
  })
}

export function buildImagesEditFormData(input: ImageStudioEditInput): FormData {
  const formData = new FormData()
  const payload = cleanObject({
    model: input.model,
    prompt: input.prompt,
    ...commonImageControls(input),
    ...(input.advancedParams ?? {})
  })

  appendRecordToFormData(formData, payload)
  formData.append('image', input.image)
  if (input.mask) {
    formData.append('mask', input.mask)
  }
  return formData
}

function buildImageGenerationTool(
  input: ImageStudioGenerationInput | ImageStudioEditInput,
  extras: Record<string, any>
): Record<string, unknown> {
  return cleanObject({
    type: 'image_generation',
    ...commonImageControls(input),
    partial_images: 1,
    ...extras
  })
}

function commonImageControls(input: ImageStudioGenerationInput | ImageStudioEditInput): Record<string, unknown> {
  return cleanObject({
    size: input.size,
    quality: input.quality,
    output_format: input.outputFormat,
    background: input.background,
    n: input.count,
    output_compression: input.outputCompression ?? undefined
  })
}

function cleanObject<T extends Record<string, any>>(value: T): T {
  const out: Record<string, any> = {}
  for (const [key, item] of Object.entries(value)) {
    if (item !== undefined && item !== null && item !== '') {
      out[key] = item
    }
  }
  return out as T
}

function objectValue(value: unknown): Record<string, any> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, any> : {}
}

function appendRecordToFormData(formData: FormData, payload: Record<string, unknown>) {
  for (const [key, value] of Object.entries(payload)) {
    if (value instanceof Blob) {
      formData.append(key, value)
    } else if (typeof value === 'object') {
      formData.append(key, JSON.stringify(value))
    } else {
      formData.append(key, String(value))
    }
  }
}

function fileToDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(new Error(`读取文件失败：${file.name}`))
    reader.readAsDataURL(file)
  })
}
