export type ImageStudioProtocol = 'responses' | 'images'
export type ImageStudioMode = 'generate' | 'edit' | 'history'

export interface ImageStudioCommonControls {
  model: string
  prompt: string
  size?: string
  quality?: string
  outputFormat?: string
  background?: string
  count?: number
  advancedParams?: Record<string, any>
}

export interface ImageStudioGenerationInput extends ImageStudioCommonControls {}

export interface ImageStudioEditInput extends ImageStudioCommonControls {
  image: File | File[]
  mask?: File | null
}

export interface ImageStudioOutput {
  id: string
  src: string
  kind: 'b64_json' | 'url'
  mimeType?: string
  revisedPrompt?: string
  raw: unknown
}

export type ImageStudioJobStatus = 'queued' | 'running' | 'succeeded' | 'failed'

export interface ImageStudioJob {
  id: number
  mode: Exclude<ImageStudioMode, 'history'>
  status: ImageStudioJobStatus
  attemptCount: number
  maxAttempts: number
  nextAttemptAt?: string
  prompt: string
  model: string
  size: string
  outputFormat: string
  estimatedCostUsd: number
  chargedAmountUsd: number
  mimeType?: string
  fileSizeBytes?: number
  width?: number
  height?: number
  errorCode?: string
  errorMessage?: string
  queuedAt: string
  startedAt?: string
  completedAt?: string
  expiresAt?: string
  assetsDeletedAt?: string
  thumbnailUrl?: string
  originalUrl?: string
}

export type ImageStudioGenerationPhase =
  | 'idle'
  | 'created'
  | 'preparing'
  | 'image_task_created'
  | 'image_in_progress'
  | 'generating'
  | 'partial_preview'
  | 'image_done'
  | 'completed'
  | 'failed'

export interface ImageStudioStreamState {
  phase: ImageStudioGenerationPhase
  message: string
  detail?: string
  startedAt?: number
  updatedAt?: number
}

export interface ImageStudioStreamEvent {
  type: string
  data: Record<string, any>
}

export interface ImageStudioHistoryRecord {
  id: string
  jobId: number
  createdAt: string
  mode: Exclude<ImageStudioMode, 'history'>
  status: ImageStudioJobStatus
  attemptCount: number
  maxAttempts: number
  nextAttemptAt?: string
  prompt: string
  model: string
  size: string
  count: number
  outputFormat: string
  errorMessage?: string
  thumbnailUrl?: string
  originalUrl?: string
  expiresAt?: string
  assetsDeletedAt?: string
  images: Array<{
    id: string
    src: string
    mimeType?: string
    revisedPrompt?: string
  }>
}

export interface ImageStudioPromptHistoryRecord {
  id: string
  createdAt: string
  mode: Exclude<ImageStudioMode, 'history'>
  prompt: string
  source: 'generated' | 'polished'
}

export interface ImageStudioRatioOption {
  value: string
  label: string
  tier: string
  size: string
  aspect: string
}

export interface ImageStudioSelectOption {
  value: string
  label: string
  tier?: '1K' | '2K' | '4K'
  status?: 'standard' | 'experimental'
  description?: string
  disabled?: boolean
  disabledReason?: string
}

export interface StudioApiKey {
  id: number
  key: string
  name: string
  status: string
  group?: {
    id?: number
    platform?: string
    allow_image_generation?: boolean
    image_rate_multiplier?: number
    image_price_1k?: number | null
    image_price_2k?: number | null
    image_price_4k?: number | null
  }
}

export type ImageStudioLightboxImage =
  | {
    kind: 'output'
    title: string
    src: string
    downloadName: string
    canEdit: boolean
    output: ImageStudioOutput
    index: number
  }
  | {
    kind: 'history'
    title: string
    src: string
    downloadName: string | null
    canEdit: boolean
    record: ImageStudioHistoryRecord
  }
  | {
    kind: 'reference'
    title: string
    src: string
    downloadName: null
    canEdit: false
  }
