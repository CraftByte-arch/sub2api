import type { ImageStudioStreamEvent } from './types'

interface JsonRequestInput {
  apiKey: string
  body: Record<string, unknown>
  onStreamEvent?: (event: ImageStudioStreamEvent) => void
}

interface FormRequestInput {
  apiKey: string
  body: FormData
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
