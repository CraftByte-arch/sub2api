import type { ImageStudioOutput } from './types'

export function extractImageStudioOutputs(response: unknown): ImageStudioOutput[] {
  const outputs: ImageStudioOutput[] = []
  const root = asRecord(response)
  if (!root) return outputs

  appendImagesApiOutputs(outputs, arrayValue(root.data))
  appendResponsesOutputs(outputs, arrayValue(root.output))
  appendAggregatedOutputs(outputs, arrayValue(root.images))

  const nestedResponse = asRecord(root.response)
  if (nestedResponse) {
    appendResponsesOutputs(outputs, arrayValue(nestedResponse.output))
  }

  return outputs
}

function appendImagesApiOutputs(outputs: ImageStudioOutput[], items: unknown[]) {
  items.forEach((item, index) => {
    const record = asRecord(item)
    if (!record) return
    appendOutput(outputs, record, index, {
      b64: stringValue(record.b64_json),
      url: stringValue(record.url),
      revisedPrompt: stringValue(record.revised_prompt),
      outputFormat: stringValue(record.output_format)
    })
  })
}

function appendResponsesOutputs(outputs: ImageStudioOutput[], items: unknown[]) {
  items.forEach((item, index) => {
    const record = asRecord(item)
    if (!record || stringValue(record.type) !== 'image_generation_call') return
    appendOutput(outputs, record, index, {
      id: stringValue(record.id),
      b64: stringValue(record.result) || stringValue(record.b64_json),
      url: stringValue(record.url),
      revisedPrompt: stringValue(record.revised_prompt),
      outputFormat: stringValue(record.output_format)
    })
  })
}

function appendAggregatedOutputs(outputs: ImageStudioOutput[], items: unknown[]) {
  items.forEach((item, index) => {
    const record = asRecord(item)
    if (!record) return
    appendOutput(outputs, record, index, {
      id: stringValue(record.id),
      b64: stringValue(record.b64_json) || stringValue(record.result),
      url: stringValue(record.url),
      revisedPrompt: stringValue(record.revised_prompt),
      mimeType: stringValue(record.mime_type) || stringValue(record.mimeType),
      outputFormat: stringValue(record.output_format)
    })
  })
}

function appendOutput(
  outputs: ImageStudioOutput[],
  raw: Record<string, unknown>,
  index: number,
  input: {
    id?: string
    b64?: string
    url?: string
    revisedPrompt?: string
    mimeType?: string
    outputFormat?: string
  }
) {
  if (input.b64) {
    const mimeType = input.mimeType || mimeTypeFromFormat(input.outputFormat)
    outputs.push({
      id: input.id || `image-${outputs.length || index}`,
      kind: 'b64_json',
      src: toDataUrl(input.b64, mimeType),
      mimeType,
      revisedPrompt: input.revisedPrompt || undefined,
      raw
    })
    return
  }

  if (input.url) {
    outputs.push({
      id: input.id || `image-${outputs.length || index}`,
      kind: 'url',
      src: input.url,
      mimeType: input.mimeType || undefined,
      revisedPrompt: input.revisedPrompt || undefined,
      raw
    })
  }
}

function toDataUrl(value: string, mimeType: string): string {
  if (value.startsWith('data:')) return value
  return `data:${mimeType};base64,${value}`
}

function mimeTypeFromFormat(format?: string): string {
  const normalized = (format || '').trim().toLowerCase()
  if (normalized === 'webp') return 'image/webp'
  if (normalized === 'jpeg' || normalized === 'jpg') return 'image/jpeg'
  return 'image/png'
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}
