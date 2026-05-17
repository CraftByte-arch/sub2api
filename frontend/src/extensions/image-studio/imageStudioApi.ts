interface JsonRequestInput {
  apiKey: string
  body: Record<string, unknown>
}

interface FormRequestInput {
  apiKey: string
  body: FormData
}

export async function sendResponsesImageRequest(input: JsonRequestInput): Promise<unknown> {
  return sendJson('/v1/responses', input)
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
