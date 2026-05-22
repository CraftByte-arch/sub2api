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
  delete payloadExtras.n
  delete payloadExtras.output_compression
  delete toolExtras.n
  delete toolExtras.output_compression
  moveResponsesToolField(payloadExtras, toolExtras, 'partial_images')
  const responsesModel = stringValue(payloadExtras.model) || stringValue(payloadExtras.responses_model) || 'gpt-5.5'
  delete payloadExtras.model
  delete payloadExtras.responses_model

  return cleanObject({
    model: responsesModel,
    input: [{ role: 'user', content: [{ type: 'input_text', text: input.prompt }] }],
    tools: [buildImageGenerationTool(input, toolExtras)],
    tool_choice: { type: 'image_generation' },
    reasoning: { effort: 'xhigh' },
    store: false,
    stream: true,
    ...payloadExtras
  })
}

export function buildImagesGenerationPayload(input: ImageStudioGenerationInput): Record<string, unknown> {
  return cleanObject({
    model: input.model,
    prompt: input.prompt,
    ...commonImageControls(input, { includeCount: true }),
    ...(input.advancedParams ?? {})
  })
}

export async function buildResponsesEditPayload(input: ImageStudioEditInput): Promise<Record<string, unknown>> {
  const advancedParams = input.advancedParams ?? {}
  const toolExtras = objectValue(advancedParams.tool)
  const payloadExtras = { ...advancedParams }
  delete payloadExtras.tool
  delete payloadExtras.n
  delete payloadExtras.output_compression
  delete toolExtras.n
  delete toolExtras.output_compression
  moveResponsesToolField(payloadExtras, toolExtras, 'partial_images')
  const responsesModel = stringValue(payloadExtras.model) || stringValue(payloadExtras.responses_model) || 'gpt-5.5'
  delete payloadExtras.model
  delete payloadExtras.responses_model

  const content: Array<Record<string, string>> = [
    { type: 'input_text', text: input.prompt }
  ]
  const images = Array.isArray(input.image) ? input.image : [input.image]
  for (const image of images) {
    content.push({ type: 'input_image', image_url: await fileToDataUrl(image) })
  }

  if (input.mask) {
    content.push({ type: 'input_image_mask', image_url: await fileToDataUrl(input.mask) })
  }

  return cleanObject({
    model: responsesModel,
    input: [{ role: 'user', content }],
    tools: [buildImageGenerationTool(input, toolExtras)],
    tool_choice: { type: 'image_generation' },
    reasoning: { effort: 'xhigh' },
    store: false,
    stream: true,
    ...payloadExtras
  })
}

export function buildImagesEditFormData(input: ImageStudioEditInput): FormData {
  const formData = new FormData()
  const payload = cleanObject({
    model: input.model,
    prompt: input.prompt,
    ...commonImageControls(input, { includeCount: true }),
    ...(input.advancedParams ?? {})
  })

  appendRecordToFormData(formData, payload)
  const images = Array.isArray(input.image) ? input.image : [input.image]
  for (const image of images) {
    formData.append('image', image)
  }
  if (input.mask) {
    formData.append('mask', input.mask)
  }
  return formData
}

function buildImageGenerationTool(
  input: ImageStudioGenerationInput | ImageStudioEditInput,
  extras: Record<string, any>,
  options: { includeCount?: boolean } = {}
): Record<string, unknown> {
  return cleanObject({
    type: 'image_generation',
    model: input.model,
    action: isImageStudioEditInput(input) ? 'edit' : 'generate',
    ...commonImageControls(input, options),
    moderation: 'low',
    ...extras
  })
}

function commonImageControls(
  input: ImageStudioGenerationInput | ImageStudioEditInput,
  options: { includeCount?: boolean } = {}
): Record<string, unknown> {
  const countValue = input.count ?? 1
  const count = options.includeCount ? countValue : undefined
  return cleanObject({
    size: input.size,
    quality: input.quality || 'auto',
    output_format: input.outputFormat,
    background: input.background,
    n: count
  })
}

function moveResponsesToolField(payloadExtras: Record<string, any>, toolExtras: Record<string, any>, field: string) {
  if (payloadExtras[field] !== undefined && toolExtras[field] === undefined) {
    toolExtras[field] = payloadExtras[field]
  }
  delete payloadExtras[field]
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

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function isImageStudioEditInput(
  input: ImageStudioGenerationInput | ImageStudioEditInput
): input is ImageStudioEditInput {
  return 'image' in input
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
