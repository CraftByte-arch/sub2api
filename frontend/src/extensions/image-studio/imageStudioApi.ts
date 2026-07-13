import { apiClient } from '@/api'
import type { PaginatedResponse } from '@/types'
import type {
  ImageStudioJob,
  ImageStudioStreamEvent
} from './types'

interface JsonRequestInput {
  apiKey: string
  body: Record<string, unknown>
  onStreamEvent?: (event: ImageStudioStreamEvent) => void
}

interface FormRequestInput {
  apiKey: string
  body: FormData
}

interface ImageStudioJobCreateInput {
  apiKeyId: number
  mode: 'generate' | 'edit'
  prompt: string
  model: string
  size: string
  outputFormat: string
  quality?: string
  background?: string
  style?: string
  moderation?: string
  inputFidelity?: string
  outputCompression?: number
  imageDataUrls?: string[]
  maskDataUrl?: string
}

export interface ImageStudioJobStats {
  pendingCount: number
  failedCount: number
}

export async function sendResponsesImageRequest(input: JsonRequestInput): Promise<unknown> {
  const response = await fetch('/v1/responses', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${input.apiKey}`,
      Accept: 'text/event-stream',
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(input.body)
  })

  if (!response.ok) return parseGatewayResponse(response)

  const contentType = response.headers.get('content-type') || ''
  if (!contentType.includes('text/event-stream') || !response.body) {
    return parseGatewayResponse(response)
  }

  return parseResponsesImageStream(response.body, input.onStreamEvent)
}

export async function sendImagesGenerationRequest(input: JsonRequestInput): Promise<unknown> {
  return sendJson('/v1/images/generations', input)
}

export async function sendImagesEditRequest(input: FormRequestInput): Promise<unknown> {
  const response = await fetch('/v1/images/edits', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${input.apiKey}`
    },
    body: input.body
  })
  return parseGatewayResponse(response)
}

export async function sendPromptPolishRequest(input: JsonRequestInput): Promise<unknown> {
  const response = await fetch('/v1/responses', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${input.apiKey}`,
      Accept: 'text/event-stream',
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(input.body)
  })

  if (!response.ok) return parseGatewayResponse(response)

  const contentType = response.headers.get('content-type') || ''
  if (!contentType.includes('text/event-stream') || !response.body) {
    return parseGatewayResponse(response)
  }

  return parseResponsesTextStream(response.body)
}

export async function listGatewayModels(apiKey: string): Promise<string[]> {
  const response = await fetch('/v1/models', {
    headers: { Authorization: `Bearer ${apiKey}` }
  })
  const payload = await parseGatewayResponse(response) as { data?: Array<{ id?: unknown }> }
  const seen = new Set<string>()

  return (Array.isArray(payload?.data) ? payload.data : [])
    .map((item) => typeof item?.id === 'string' ? item.id.trim() : '')
    .filter((id) => id.length > 0 && !seen.has(id) && Boolean(seen.add(id)))
}

export async function createImageStudioJob(input: ImageStudioJobCreateInput): Promise<ImageStudioJob> {
  const { data } = await apiClient.post<ImageStudioJob>('/image-studio/jobs', {
    api_key_id: input.apiKeyId,
    mode: input.mode,
    prompt: input.prompt,
    model: input.model,
    size: input.size,
    output_format: input.outputFormat,
    quality: input.quality,
    background: input.background,
    style: input.style,
    moderation: input.moderation,
    input_fidelity: input.inputFidelity,
    output_compression: input.outputCompression,
    image_data_urls: input.imageDataUrls,
    mask_data_url: input.maskDataUrl
  })
  return normalizeJob(data)
}

export async function listImageStudioJobs(page = 1, pageSize = 20): Promise<PaginatedResponse<ImageStudioJob>> {
  const { data } = await apiClient.get<PaginatedResponse<ImageStudioJob>>('/image-studio/jobs', {
    params: { page, page_size: pageSize }
  })
  return {
    ...data,
    items: (data.items || []).map(normalizeJob)
  }
}

export async function getImageStudioJob(id: number): Promise<ImageStudioJob> {
  const { data } = await apiClient.get<ImageStudioJob>(`/image-studio/jobs/${id}`)
  return normalizeJob(data)
}

export async function getImageStudioJobStats(): Promise<ImageStudioJobStats> {
  const { data } = await apiClient.get('/image-studio/jobs/stats')
  return {
    pendingCount: Number(data?.pending_count || data?.pendingCount || 0),
    failedCount: Number(data?.failed_count || data?.failedCount || 0)
  }
}

export async function deleteImageStudioJob(id: number): Promise<void> {
  await apiClient.delete(`/image-studio/jobs/${id}`)
}

export async function fetchImageStudioOriginal(id: number): Promise<Blob> {
  const response = await fetch(`/api/v1/image-studio/jobs/${id}/original`, {
    method: 'GET',
    headers: buildAuthHeaders(),
    credentials: 'include'
  })
  if (!response.ok) {
    throw new Error(await extractFetchError(response))
  }
  return response.blob()
}

export async function fetchImageStudioThumbnail(id: number): Promise<Blob> {
  const response = await fetch(`/api/v1/image-studio/jobs/${id}/thumbnail`, {
    method: 'GET',
    headers: buildAuthHeaders(),
    credentials: 'include'
  })
  if (!response.ok) {
    throw new Error(await extractFetchError(response))
  }
  return response.blob()
}

async function sendJson(url: string, input: JsonRequestInput): Promise<unknown> {
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${input.apiKey}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(input.body)
  })
  return parseGatewayResponse(response)
}

async function parseGatewayResponse(response: Response): Promise<unknown> {
  const text = await response.text()
  const payload = parseMaybeJson(text)

  if (!response.ok) {
    throw new Error(extractErrorMessage(payload) || text || `Gateway request failed (${response.status})`)
  }

  return payload
}

function parseMaybeJson(text: string): unknown {
  if (!text.trim()) return null
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

function extractErrorMessage(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  const record = payload as Record<string, any>
  const error = record.error
  if (typeof error === 'string') return error
  if (error && typeof error === 'object' && typeof error.message === 'string') return error.message
  if (typeof record.message === 'string') return record.message
  return ''
}

function buildAuthHeaders(): HeadersInit {
  const token = localStorage.getItem('auth_token')
  const headers: Record<string, string> = {}
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  return headers
}

async function extractFetchError(response: Response): Promise<string> {
  const text = await response.text()
  const payload = parseMaybeJson(text)
  return extractErrorMessage(payload) || text || `Request failed (${response.status})`
}

function normalizeJob(job: any): ImageStudioJob {
  return {
    id: Number(job?.id || 0),
    mode: job?.mode === 'edit' ? 'edit' : 'generate',
    status: job?.status || 'queued',
    attemptCount: Number(job?.attempt_count || job?.attemptCount || 0),
    maxAttempts: Number(job?.max_attempts || job?.maxAttempts || 0),
    nextAttemptAt: job?.next_attempt_at || job?.nextAttemptAt || undefined,
    prompt: job?.prompt || '',
    model: job?.model || '',
    size: job?.size || '',
    outputFormat: job?.output_format || job?.outputFormat || 'png',
    estimatedCostUsd: Number(job?.estimated_cost_usd || job?.estimatedCostUsd || 0),
    chargedAmountUsd: Number(job?.charged_amount_usd || job?.chargedAmountUsd || 0),
    mimeType: job?.mime_type || job?.mimeType || undefined,
    fileSizeBytes: Number(job?.file_size_bytes || job?.fileSizeBytes || 0) || undefined,
    width: Number(job?.width || 0) || undefined,
    height: Number(job?.height || 0) || undefined,
    errorCode: job?.error_code || job?.errorCode || undefined,
    errorMessage: job?.error_message || job?.errorMessage || undefined,
    queuedAt: job?.queued_at || job?.queuedAt || '',
    startedAt: job?.started_at || job?.startedAt || undefined,
    completedAt: job?.completed_at || job?.completedAt || undefined,
    expiresAt: job?.expires_at || job?.expiresAt || undefined,
    assetsDeletedAt: job?.assets_deleted_at || job?.assetsDeletedAt || undefined,
    thumbnailUrl: job?.thumbnail_url || job?.thumbnailUrl || undefined,
    originalUrl: job?.original_url || job?.originalUrl || undefined
  }
}

async function parseResponsesImageStream(
  body: ReadableStream<Uint8Array>,
  onStreamEvent?: (event: ImageStudioStreamEvent) => void
): Promise<unknown> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const output: unknown[] = []
  const outputKeys = new Set<string>()

  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split(/\r?\n/)
    buffer = lines.pop() || ''

    for (const line of lines) {
      const trimmed = line.trim()
      if (!trimmed.startsWith('data:')) continue
      const data = trimmed.slice(5).trim()
      if (!data || data === '[DONE]') continue
      const event = parseMaybeJson(data)
      notifyResponsesStreamEvent(event, onStreamEvent)
      collectResponsesStreamImageEvent(event, output, outputKeys)
      const errorMessage = extractStreamErrorMessage(event)
      if (errorMessage) throw new Error(errorMessage)
    }
  }

  const tail = decoder.decode()
  if (tail) {
    buffer += tail
  }
  for (const line of buffer.split(/\r?\n/)) {
    const trimmed = line.trim()
    if (!trimmed.startsWith('data:')) continue
    const data = trimmed.slice(5).trim()
    if (!data || data === '[DONE]') continue
    const event = parseMaybeJson(data)
    notifyResponsesStreamEvent(event, onStreamEvent)
    collectResponsesStreamImageEvent(event, output, outputKeys)
    const errorMessage = extractStreamErrorMessage(event)
    if (errorMessage) throw new Error(errorMessage)
  }

  return { output }
}

async function parseResponsesTextStream(body: ReadableStream<Uint8Array>): Promise<unknown> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const output: unknown[] = []
  let outputText = ''

  const handleLine = (line: string) => {
    const trimmed = line.trim()
    if (!trimmed.startsWith('data:')) return
    const data = trimmed.slice(5).trim()
    if (!data || data === '[DONE]') return
    const event = parseMaybeJson(data)
    collectResponsesTextEvent(event, output, (text) => {
      outputText += text
    })
    const errorMessage = extractStreamErrorMessage(event)
    if (errorMessage) throw new Error(errorMessage)
  }

  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split(/\r?\n/)
    buffer = lines.pop() || ''
    for (const line of lines) {
      handleLine(line)
    }
  }

  const tail = decoder.decode()
  if (tail) buffer += tail
  for (const line of buffer.split(/\r?\n/)) {
    handleLine(line)
  }

  return {
    output,
    output_text: outputText.trim()
  }
}

function collectResponsesTextEvent(
  event: unknown,
  output: unknown[],
  appendText: (text: string) => void
) {
  if (!event || typeof event !== 'object') return
  const record = event as Record<string, any>
  if (
    (record.type === 'response.output_text.delta' || record.type === 'response.output_text.done') &&
    typeof record.text === 'string'
  ) {
    appendText(record.text)
  }
  if (record.type === 'response.output_item.done' && record.item && typeof record.item === 'object') {
    output.push(record.item)
  }
  const nestedOutput = record.response?.output
  if (Array.isArray(nestedOutput)) {
    output.splice(0, output.length, ...nestedOutput)
  }
}

function notifyResponsesStreamEvent(event: unknown, onStreamEvent?: (event: ImageStudioStreamEvent) => void) {
  if (!onStreamEvent || !event || typeof event !== 'object') return
  const record = event as Record<string, any>
  if (typeof record.type !== 'string' || !record.type) return
  onStreamEvent({ type: record.type, data: record })
}

function collectResponsesStreamImageEvent(event: unknown, output: unknown[], outputKeys: Set<string>) {
  if (!event || typeof event !== 'object') return
  const record = event as Record<string, any>
  const item = record.item
  if (
    record.type === 'response.output_item.done' &&
    item &&
    typeof item === 'object' &&
    item.type === 'image_generation_call' &&
    typeof item.result === 'string' &&
    item.result
  ) {
    appendResponsesImageOutput(output, outputKeys, item)
    return
  }

  const nestedOutput = record.response?.output
  if (Array.isArray(nestedOutput)) {
    for (const candidate of nestedOutput) {
      if (
        candidate &&
        typeof candidate === 'object' &&
        (candidate as Record<string, any>).type === 'image_generation_call' &&
        typeof (candidate as Record<string, any>).result === 'string'
      ) {
        appendResponsesImageOutput(output, outputKeys, candidate)
      }
    }
  }
}

function appendResponsesImageOutput(output: unknown[], outputKeys: Set<string>, item: Record<string, any>) {
  const key = typeof item.id === 'string' && item.id
    ? `id:${item.id}`
    : `result:${item.result}`
  if (outputKeys.has(key)) return
  outputKeys.add(key)
  output.push(item)
}

function extractStreamErrorMessage(event: unknown): string {
  if (!event || typeof event !== 'object') return ''
  const record = event as Record<string, any>
  const error = record.error
  if (typeof error === 'string') return error
  if (error && typeof error === 'object' && typeof error.message === 'string') return error.message
  return ''
}
