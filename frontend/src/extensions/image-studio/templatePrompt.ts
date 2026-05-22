import type { ImagePromptTemplate } from './templateTypes'

export function createTemplateDefaultValues(template: ImagePromptTemplate): Record<string, string> {
  return Object.fromEntries(template.fields.map((field) => [field.key, field.defaultValue ?? '']))
}

export function renderTemplatePrompt(
  template: ImagePromptTemplate | null | undefined,
  values: Record<string, string>
): string {
  if (!template) return ''
  const fragments = template.promptFragments
    .map((fragment) => replaceTemplateFragment(fragment, values))
    .filter((fragment): fragment is string => Boolean(fragment))

  return fragments
    .join('，')
    .replace(/[，,。]\s*[，,。]/g, '，')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/[，,。]+$/, '')
}

function replaceTemplateFragment(fragment: string, values: Record<string, string>): string {
  const placeholders = Array.from(fragment.matchAll(/\{([^}]+)\}/g)).map((match) => match[1])
  if (placeholders.some((key) => !(values[key] || '').trim())) return ''
  return fragment.replace(/\{([^}]+)\}/g, (_, key: string) => (values[key] || '').trim())
}
