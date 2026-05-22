export type TemplateMode = 'text-to-image' | 'image-to-image'

export type TemplateFieldType = 'text' | 'textarea' | 'select' | 'ratio'

export interface TemplateFieldOption {
  label: string
  value: string
}

export interface TemplateField {
  key: string
  label: string
  type: TemplateFieldType
  required?: boolean
  placeholder?: string
  helpText?: string
  defaultValue?: string
  options?: TemplateFieldOption[]
  section?: 'basic' | 'advanced'
}

export interface ImagePromptTemplate {
  id: string
  title: string
  mode: TemplateMode
  section: 'common' | 'advanced'
  category: string
  description: string
  tags: string[]
  previewText: string
  recommendedRatios?: string[]
  requiresReference?: boolean
  recommendedModel?: string
  badge?: string
  promptFragments: string[]
  fields: TemplateField[]
}

export interface TemplateDraftPayload {
  prompt: string
  templateId: string
  mode: TemplateMode
  recommendedRatio?: string
}

export type TemplateSyncState = 'idle' | 'linked' | 'detached'
