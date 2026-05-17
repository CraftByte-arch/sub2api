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
  outputCompression?: number | null
  advancedParams?: Record<string, any>
}

export interface ImageStudioGenerationInput extends ImageStudioCommonControls {}

export interface ImageStudioEditInput extends ImageStudioCommonControls {
  image: File
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
