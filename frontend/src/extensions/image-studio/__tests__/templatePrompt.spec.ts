import { describe, expect, it } from 'vitest'

import { imagePromptTemplates } from '../templateRegistry'
import { createTemplateDefaultValues, renderTemplatePrompt } from '../templatePrompt'

describe('image prompt templates', () => {
  it('renders the minimal poster style when the topic is empty', () => {
    const template = imagePromptTemplates.find((item) => item.id === 'poster-minimal-cat')
    expect(template).toBeDefined()

    const values = createTemplateDefaultValues(template!)
    values.style = '艺术海报'
    values.topic = ''

    expect(renderTemplatePrompt(template, values)).toContain('整体风格为艺术海报')
  })
})
