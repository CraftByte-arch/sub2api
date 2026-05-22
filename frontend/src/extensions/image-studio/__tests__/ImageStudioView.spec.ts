import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ImageStudioView from '../ImageStudioView.vue'
import { sendPromptPolishRequest, sendResponsesImageRequest } from '../imageStudioApi'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => ({
      'imageStudio.title': '生图体验',
      'imageStudio.description': '使用当前账号的 API Key 调用 Sub2API 生图能力。'
    }[key] ?? key)
  })
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<main><slot /></main>' }
}))

vi.mock('@/api', () => ({
  keysAPI: {
    list: vi.fn()
  }
}))

vi.mock('../imageStudioApi', () => ({
  sendResponsesImageRequest: vi.fn(),
  sendPromptPolishRequest: vi.fn()
}))

const keysAPI = await import('@/api').then((mod) => mod.keysAPI)
let mountedWrappers: VueWrapper[] = []

describe('ImageStudioView', () => {
  beforeEach(() => {
    window.localStorage.clear()
    vi.stubGlobal('indexedDB', createIndexedDbStub())
    Object.defineProperty(URL, 'createObjectURL', {
      value: vi.fn((file: File) => `blob:${file.name}`),
      configurable: true
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      value: vi.fn(),
      configurable: true
    })
    vi.mocked(keysAPI.list).mockResolvedValue({
      items: [
        {
          id: 1,
          key: 'sk-active',
          name: '生图 · 生图分组',
          status: 'active',
          group: {
            id: 10,
            name: '生图分组',
            platform: 'openai',
            allow_image_generation: true,
            image_rate_multiplier: 1,
            image_price_1k: 1,
            image_price_2k: 2,
            image_price_4k: 4
          }
        },
        {
          id: 2,
          key: 'sk-disabled',
          name: 'Disabled',
          status: 'inactive'
        }
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    } as any)
    vi.mocked(sendResponsesImageRequest).mockResolvedValue({
      output: [{ id: 'ig_test', type: 'image_generation_call', result: 'ZmFrZQ==', output_format: 'png' }]
    })
  })

  afterEach(() => {
    mountedWrappers.forEach((wrapper) => wrapper.unmount())
    mountedWrappers = []
    document.body.innerHTML = ''
  })

  it('renders the workspace shell and active API keys', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('文生图')
    expect(wrapper.text()).toContain('图生图')
    expect(wrapper.text()).toContain('记录')
    expect(wrapper.text()).toContain('生图 · 生图分组')
    expect(wrapper.text()).not.toContain('Disabled')
    expect(wrapper.text()).toContain('还没有图片')
    expect(wrapper.text()).toContain('赛博朋克城市夜景')
    expect(wrapper.text()).toContain('高级参数')
    expect(wrapper.text()).toContain('预估费用')
    expect(wrapper.text()).toContain('$1.00')
    expect(wrapper.text()).not.toContain('Responses')
    expect(wrapper.find('#studio-model').exists()).toBe(false)
  })

  it('applies prompt examples to the prompt field', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.findAll('[data-testid="prompt-example"]')[0].trigger('click')

    expect((wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value).toBe(
      '赛博朋克城市夜景，霓虹雨夜，电影感光影，8K'
    )
  })

  it('switches to image-to-image mode', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')

    expect(wrapper.text()).toContain('参考图')
    expect(wrapper.text()).toContain('0 / 4')
    expect(wrapper.text()).toContain('添加')
    expect(wrapper.find('.reference-collapse-button').exists()).toBe(false)
  })

  it('accepts reference images in image-to-image mode', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')
    const file = new File(['fake'], 'reference.png', { type: 'image/png' })
    const input = wrapper.get('[data-testid="reference-input"]')

    Object.defineProperty(input.element, 'files', {
      value: [file],
      configurable: true
    })
    await input.trigger('change')

    expect(wrapper.text()).toContain('1 / 4')
    expect(wrapper.text()).toContain('reference.png')
    expect(wrapper.get('[data-testid="reference-preview"]').attributes()).toMatchObject({
      src: 'blob:reference.png',
      alt: ''
    })
  })

  it('opens reference image previews in fullscreen', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')
    const file = new File(['fake'], 'reference.png', { type: 'image/png' })
    const input = wrapper.get('[data-testid="reference-input"]')

    Object.defineProperty(input.element, 'files', {
      value: [file],
      configurable: true
    })
    await input.trigger('change')

    await wrapper.get('.reference-strip-inline').trigger('click')
    await wrapper.get('[data-testid="reference-preview"]').trigger('click')

    expect(wrapper.get('[data-testid="image-lightbox"]').text()).toContain('参考图')
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('src')).toBe('blob:reference.png')
    expect(wrapper.find('[data-testid="lightbox-edit"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="lightbox-download"]').exists()).toBe(false)
  })

  it('accepts pasted clipboard images in image-to-image mode', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')
    const file = new File(['fake'], 'clipboard.png', { type: 'image/png' })
    const preventDefault = vi.fn()
    const pasteEvent = new Event('paste', { bubbles: true }) as ClipboardEvent
    Object.defineProperty(pasteEvent, 'clipboardData', {
      value: {
        items: [
          {
            kind: 'file',
            type: 'image/png',
            getAsFile: () => file
          }
        ]
      },
      configurable: true
    })
    Object.defineProperty(pasteEvent, 'preventDefault', {
      value: preventDefault,
      configurable: true
    })

    document.dispatchEvent(pasteEvent)
    await flushPromises()

    expect(wrapper.text()).toContain('1 / 4')
    expect(wrapper.text()).toContain('clipboard.png')
    expect(wrapper.get('[data-testid="reference-preview"]').attributes('src')).toBe('blob:clipboard.png')
    expect(preventDefault).toHaveBeenCalled()
  })

  it('switches to image-to-image mode when pasting images in text-to-image mode', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="tab-generate"]').classes()).toContain('active')
    const file = new File(['fake'], 'paste-switch.png', { type: 'image/png' })
    const preventDefault = vi.fn()
    const pasteEvent = new Event('paste', { bubbles: true }) as ClipboardEvent
    Object.defineProperty(pasteEvent, 'clipboardData', {
      value: {
        items: [
          {
            kind: 'file',
            type: 'image/png',
            getAsFile: () => file
          }
        ]
      },
      configurable: true
    })
    Object.defineProperty(pasteEvent, 'preventDefault', {
      value: preventDefault,
      configurable: true
    })

    document.dispatchEvent(pasteEvent)
    await flushPromises()

    expect(wrapper.get('[data-testid="tab-edit"]').classes()).toContain('active')
    expect(wrapper.text()).toContain('1 / 4')
    expect(wrapper.text()).toContain('paste-switch.png')
    expect(preventDefault).toHaveBeenCalled()
  })

  it('enables image-to-image submit after prompt and reference image are provided', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')
    await wrapper.get('[data-testid="prompt-input"]').setValue('保留构图，改成雨夜电影感')
    const file = new File(['fake'], 'reference.png', { type: 'image/png' })
    const input = wrapper.get('[data-testid="reference-input"]')
    Object.defineProperty(input.element, 'files', {
      value: [file],
      configurable: true
    })
    await input.trigger('change')

    expect(wrapper.get('[data-testid="generate-button"]').text()).toContain('生成改图')
    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeUndefined()
  })

  it('enables image-to-image submit with a reference image even when prompt is empty', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')
    const file = new File(['fake'], 'reference.png', { type: 'image/png' })
    const input = wrapper.get('[data-testid="reference-input"]')
    Object.defineProperty(input.element, 'files', {
      value: [file],
      configurable: true
    })
    await input.trigger('change')

    expect(wrapper.get('[data-testid="generate-button"]').text()).toContain('生成改图')
    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeUndefined()
  })

  it('disables submit until prompt is entered', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')

    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeUndefined()
  })

  it('sends the selected resolution for the chosen ratio in generation payloads', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="ratio-option-1:1"]').trigger('click')
    await wrapper.get('[data-testid="resolution-option-2048x2048"]').trigger('click')
    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    expect(sendResponsesImageRequest).toHaveBeenCalledWith(expect.objectContaining({
      body: expect.objectContaining({
        tools: [
          expect.objectContaining({
            size: '2048x2048',
            output_format: 'jpeg'
          })
        ]
      })
    }))
  })

  it('renders generated image outputs after submit', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('.advanced-header').trigger('click')
    await wrapper.get('[data-testid="quality-select"]').setValue('high')
    await wrapper.get('[data-testid="background-select"]').setValue('transparent')
    await wrapper.get('[data-testid="output-format-select"]').setValue('webp')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    expect(wrapper.get('[data-testid="generating-preview"]').text()).toContain('正在生成图片')
    await flushPromises()

    expect(sendResponsesImageRequest).toHaveBeenCalledWith(expect.objectContaining({
      apiKey: 'sk-active',
      body: expect.objectContaining({
        model: 'gpt-5.4-mini',
        input: '一座安静茶室',
        tool_choice: { type: 'image_generation' },
        tools: [
          expect.objectContaining({
            model: 'gpt-image-2',
            quality: 'high',
            background: 'transparent',
            output_format: 'webp'
          })
        ]
      })
    }))
    expect(wrapper.find('img[alt="Generated image 1"]').attributes('src')).toBe('data:image/png;base64,ZmFrZQ==')
    expect(wrapper.get('[data-testid="download-button"]').attributes()).toMatchObject({
      href: 'data:image/png;base64,ZmFrZQ==',
      download: 'image-studio-1.png'
    })
  })

  it('opens prompt history and manages saved prompt records', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-history-button"]').trigger('click')
    expect(wrapper.text()).toContain('还没有提示词历史')

    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    const storedAfterGenerate = JSON.parse(window.localStorage.getItem('sub2api:image-studio:prompt-history:v1') || '[]')
    expect(storedAfterGenerate).toMatchObject([
      expect.objectContaining({
        prompt: '一座安静茶室',
        source: 'generated',
        mode: 'generate'
      })
    ])

    await wrapper.get('[data-testid="prompt-history-button"]').trigger('click')
    await wrapper.get('[data-testid="prompt-history-button"]').trigger('click')
    expect(wrapper.text()).toContain('一座安静茶室')

    await wrapper.get('[data-testid="prompt-input"]').setValue('森林里的玻璃房子')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="prompt-history-search"]').setValue('玻璃')
    expect(wrapper.text()).toContain('森林里的玻璃房子')
    expect(wrapper.text()).not.toContain('一座安静茶室')

    await wrapper.get('[data-testid="prompt-history-delete-button"]').trigger('click')
    expect(wrapper.text()).toContain('没有匹配的提示词历史')
    expect(JSON.parse(window.localStorage.getItem('sub2api:image-studio:prompt-history:v1') || '[]')).toHaveLength(1)

    await wrapper.get('[data-testid="prompt-input"]').setValue('第二条提示词')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="prompt-history-search"]').setValue('')
    await wrapper.get('[data-testid="prompt-history-clear-button"]').trigger('click')

    expect(wrapper.text()).toContain('还没有提示词历史')
    expect(JSON.parse(window.localStorage.getItem('sub2api:image-studio:prompt-history:v1') || '[]')).toEqual([])
  })

  it('clears prompts from the prompt header and closes prompt history', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('需要清空的提示词')
    await wrapper.get('[data-testid="prompt-clear-button"]').trigger('click')
    expect((wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value).toBe('')

    await wrapper.get('[data-testid="prompt-history-button"]').trigger('click')
    expect(wrapper.find('[data-testid="prompt-history-popover"]').exists()).toBe(true)

    await wrapper.get('[data-testid="prompt-history-close-button"]').trigger('click')
    expect(wrapper.find('[data-testid="prompt-history-popover"]').exists()).toBe(false)

    await wrapper.get('[data-testid="prompt-history-button"]').trigger('click')
    expect(wrapper.find('[data-testid="prompt-history-popover"]').exists()).toBe(true)
    document.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(wrapper.find('[data-testid="prompt-history-popover"]').exists()).toBe(false)
  })

  it('stores the original prompt when polishing succeeds', async () => {
    vi.mocked(sendPromptPolishRequest).mockResolvedValue({
      output: [
        {
          type: 'message',
          content: [{ type: 'output_text', text: '润色后的提示词' }]
        }
      ]
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('原始提示词')
    await wrapper.get('[data-testid="polish-prompt-button"]').trigger('click')
    await flushPromises()

    expect(sendPromptPolishRequest).toHaveBeenCalledWith(expect.objectContaining({
      body: expect.objectContaining({
        model: 'gpt-5.4-mini',
        instructions: expect.stringContaining('专业图像生成提示词编辑'),
        input: [
          {
            role: 'user',
            content: [
              {
                type: 'input_text',
                text: '原始提示词'
              }
            ]
          }
        ],
        store: false,
        stream: true
      })
    }))
    expect((wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value).toBe('润色后的提示词')
    expect(JSON.parse(window.localStorage.getItem('sub2api:image-studio:prompt-history:v1') || '[]')).toMatchObject([
      expect.objectContaining({
        prompt: '原始提示词',
        source: 'polished',
        mode: 'generate'
      })
    ])
  })

  it('opens the template drawer and syncs a template prompt into the main input', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="template-button"]').trigger('click')
    expect(templateDrawer().exists()).toBe(true)
    expect(templateDrawer().text()).toContain('模板中心')
    expect(templateDrawer().text()).not.toContain('模板辅助')

    await templateDrawer().get('[data-testid="template-card"]').trigger('click')

    const promptValue = (wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value
    expect(promptValue).toContain('蓝橙对比色')
    expect(promptValue).toContain('柔和自然光')
    expect(templateDrawer().text()).toContain('已连接提示词输入框')
  })

  it('shows image-to-image reference images inside the template panel', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')
    await wrapper.get('[data-testid="template-button"]').trigger('click')

    expect(templateDrawer().exists()).toBe(true)

    const file = new File(['fake'], 'reference.png', { type: 'image/png' })
    const input = wrapper.get('[data-testid="reference-input"]')

    Object.defineProperty(input.element, 'files', {
      value: [file],
      configurable: true
    })
    await input.trigger('change')

    expect(templateDrawer().text()).toContain('reference.png')
    expect(templateDrawer().find('.reference-strip').exists()).toBe(true)
    expect(templateDrawer().find('.reference-expand-guide').exists()).toBe(false)
    expect(templateDrawer().find('.reference-collapse-guide').exists()).toBe(false)
    expect(wrapper.find('[data-testid="reference-input"]').exists()).toBe(true)
  })

  it('switches to image-to-image mode when selecting an edit template', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="template-button"]').trigger('click')
    await templateDrawer().get('[data-testid="template-mode-edit"]').trigger('click')
    const cards = templateDrawer().findAll('[data-testid="template-card"]')
    await cards[0].trigger('click')

    expect(wrapper.get('[data-testid="tab-edit"]').classes()).toContain('active')
    expect(wrapper.text()).toContain('参考图')
  })

  it('detaches template sync after manual prompt edits and can resume syncing', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="template-button"]').trigger('click')
    await templateDrawer().get('[data-testid="template-card"]').trigger('click')
    await wrapper.get('[data-testid="prompt-input"]').setValue('我手动改写过提示词')

    expect(templateDrawer().text()).toContain('已脱离模板同步')
    expect(templateDrawer().find('[data-testid="template-resume-sync-button"]').exists()).toBe(true)

    await templateDrawer().get('[data-testid="template-resume-sync-button"]').trigger('click')

    const promptValue = (wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value
    expect(promptValue).not.toBe('我手动改写过提示词')
    expect(promptValue).toContain('蓝橙对比色')
    expect(templateDrawer().text()).toContain('已连接提示词输入框')
  })

  it('shows richer template metadata in cards', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="template-button"]').trigger('click')

    expect(templateDrawer().text()).toContain('品牌')
    expect(templateDrawer().text()).toContain('gpt-image-2')
  })

  it('shows text-to-image templates in image-to-image mode', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')
    await wrapper.get('[data-testid="template-button"]').trigger('click')

    expect(templateDrawer().text()).toContain('极简海报')
    expect(templateDrawer().text()).toContain('活动主视觉海报')
    expect(templateDrawer().text()).toContain('品牌包装概念板')
  })

  it('syncs the selected template when switching template categories', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="template-button"]').trigger('click')
    expect(templateDrawer().text()).toContain('极简海报')

    const commonCategoryButtons = templateDrawer().findAll('.template-category-group:not(.advanced) .template-category-tabs button')
    const sceneButton = commonCategoryButtons.find((button) => button.text() === '场景')
    expect(sceneButton).toBeTruthy()
    await sceneButton!.trigger('click')

    expect(templateDrawer().text()).toContain('电影感夜景')
    expect(templateDrawer().text()).not.toContain('科技感 UI 概念图')

    const advancedToggle = templateDrawer().get('[data-testid="template-advanced-toggle"]')
    await advancedToggle.trigger('click')
    const advancedCategoryButtons = templateDrawer().findAll('.template-category-group.advanced .template-category-tabs button')
    const brandingButton = advancedCategoryButtons.find((button) => button.text() === '品牌高级')
    expect(brandingButton).toBeTruthy()
    await brandingButton!.trigger('click')

    expect(templateDrawer().text()).toContain('品牌包装概念板')
    expect(templateDrawer().text()).toContain('高级模板')
  })

  it('stores and restores template history for the same template', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="template-button"]').trigger('click')
    await templateDrawer().get('[data-testid="template-card"]').trigger('click')

    const inputs = templateDrawer().findAll('.template-form-grid input')
    await inputs[0].setValue('宠物咖啡店开业海报')
    await inputs[1].setValue('一只慵懒的大橘猫')
    await wrapper.get('[data-testid="template-button"]').trigger('click')
    expect(templateDrawer().exists()).toBe(false)
    await wrapper.get('[data-testid="template-button"]').trigger('click')

    await templateDrawer().get('[data-testid="template-history-button"]').trigger('click')
    expect(templateDrawer().find('[data-testid="template-history-popover"]').exists()).toBe(true)
    expect(templateDrawer().text()).toContain('宠物咖啡店开业海报')

    await templateDrawer().get('.template-history-actions button').trigger('click')

    const refreshedInputs = templateDrawer().findAll('.template-form-grid input')
    expect((refreshedInputs[0].element as HTMLInputElement).value).toBe('宠物咖啡店开业海报')
    expect((refreshedInputs[1].element as HTMLInputElement).value).toBe('一只慵懒的大橘猫')
  })

  it('moves a generated image into image-to-image reference images', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="edit-output-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('参考图')
    expect(wrapper.text()).toContain('1 / 4')
    expect(wrapper.text()).toContain('generated-reference-1.png')
    expect(wrapper.get('[data-testid="reference-preview"]').attributes('src')).toBe('data:image/png;base64,ZmFrZQ==')
    expect(wrapper.get('[data-testid="tab-edit"]').classes()).toContain('active')
    expect(wrapper.find('img[alt="Generated image 1"]').exists()).toBe(false)
  })

  it('opens generated images in a fullscreen preview with edit and download actions', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="result-preview-image"]').trigger('click')

    expect(wrapper.get('[data-testid="image-lightbox"]').text()).toContain('生成图片')
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('src')).toBe('data:image/png;base64,ZmFrZQ==')
    expect(wrapper.get('[data-testid="lightbox-edit"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="lightbox-download"]').attributes()).toMatchObject({
      href: 'data:image/png;base64,ZmFrZQ==',
      download: 'image-studio-1.png'
    })

    await wrapper.get('[data-testid="lightbox-close"]').trigger('click')
    expect(wrapper.find('[data-testid="image-lightbox"]').exists()).toBe(false)
  })

  it('zooms and resets fullscreen image previews', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="result-preview-image"]').trigger('click')
    expect(wrapper.get('[data-testid="image-lightbox"]').text()).toContain('100%')
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('style')).toContain('scale(1)')

    await wrapper.get('[data-testid="lightbox-zoom-in"]').trigger('click')
    expect(wrapper.get('[data-testid="image-lightbox"]').text()).toContain('125%')
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('style')).toContain('scale(1.25)')

    await wrapper.get('[data-testid="lightbox-reset"]').trigger('click')
    expect(wrapper.get('[data-testid="image-lightbox"]').text()).toContain('100%')
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('style')).toContain('scale(1)')
  })

  it('opens reference images in a fullscreen preview with only close action', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')
    const file = new File(['fake'], 'reference.png', { type: 'image/png' })
    const input = wrapper.get('[data-testid="reference-input"]')
    Object.defineProperty(input.element, 'files', {
      value: [file],
      configurable: true
    })
    await input.trigger('change')

    await wrapper.get('.reference-strip-inline').trigger('click')
    await wrapper.get('[data-testid="reference-preview"]').trigger('click')

    expect(wrapper.get('[data-testid="image-lightbox"]').text()).toContain('参考图')
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('src')).toBe('blob:reference.png')
    expect(wrapper.find('[data-testid="lightbox-edit"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="lightbox-download"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="lightbox-close"]').exists()).toBe(true)
  })

  it('clears visible outputs when switching between generation tabs directly', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('img[alt="Generated image 1"]').exists()).toBe(true)

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')

    expect(wrapper.find('img[alt="Generated image 1"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('还没有图片')
  })

  it('renders local-only generation history without storing API keys', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    expect(wrapper.text()).toContain('最近生成')
    expect(wrapper.text()).toContain('仅保存在当前浏览器')
    expect(wrapper.text()).toContain('还没有生成记录')

    await wrapper.get('[data-testid="tab-generate"]').trigger('click')
    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    const storedValue = window.localStorage.getItem('sub2api:image-studio:history:v1') || ''
    expect(storedValue).toContain('一座安静茶室')
    expect(storedValue).not.toContain('data:image/png;base64,ZmFrZQ==')
    expect(storedValue).not.toContain('sk-active')

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    expect(wrapper.find('[data-testid="history-grid"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('一座安静茶室')
    expect(wrapper.find('.history-card img').attributes('src')).toBe('data:image/png;base64,ZmFrZQ==')
    expect(wrapper.get('[data-testid="history-download-button"]').attributes()).toMatchObject({
      href: 'data:image/png;base64,ZmFrZQ=='
    })

    await wrapper.get('[data-testid="history-edit-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="tab-edit"]').classes()).toContain('active')
    expect(wrapper.text()).toContain('参考图')
    expect(wrapper.text()).toContain('1 / 4')
    expect(wrapper.text()).toContain('generated-reference-1.png')
  })

  it('opens history images in a fullscreen preview with edit and download actions', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一张复古海报')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')

    await wrapper.get('[data-testid="history-preview-image"]').trigger('click')

    expect(wrapper.get('[data-testid="image-lightbox"]').text()).toContain('一张复古海报')
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('src')).toBe('data:image/png;base64,ZmFrZQ==')
    expect(wrapper.get('[data-testid="lightbox-edit"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="lightbox-download"]').attributes('href')).toBe('data:image/png;base64,ZmFrZQ==')
  })

  it('deletes local generation history records', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一张复古海报')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    expect(wrapper.find('[data-testid="history-grid"]').exists()).toBe(true)
    expect(window.localStorage.getItem('sub2api:image-studio:history:v1')).toContain('一张复古海报')

    await wrapper.get('[data-testid="history-delete-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="history-empty"]').exists()).toBe(true)
    expect(window.localStorage.getItem('sub2api:image-studio:history:v1')).not.toContain('一张复古海报')
  })

  it('clears cached images that are no longer in visible history', async () => {
    const indexedDbStub = createIndexedDbStub()
    vi.stubGlobal('indexedDB', indexedDbStub)
    const wrapper = mountView()
    await flushPromises()

    const db = await openStubDb(indexedDbStub)
    const store = db.getStore('history-images')
    store.set('orphan:image', { id: 'orphan:image', src: 'data:image/png;base64,b3JwaGFu' })

    await wrapper.get('[data-testid="prompt-input"]').setValue('一张复古海报')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()
    expect(store.has('orphan:image')).toBe(true)

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await wrapper.get('[data-testid="history-cache-clean-button"]').trigger('click')
    await waitForAsyncIndexedDb()
    await flushPromises()

    expect(store.has('orphan:image')).toBe(false)
    expect([...store.keys()]).toHaveLength(1)
    expect(wrapper.text()).toContain('已清理 1 张缓存图片')
  })

  it('reuses a history prompt back in text-to-image mode', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一张复古海报')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await wrapper.get('[data-testid="history-reuse-button"]').trigger('click')

    expect(wrapper.get('[data-testid="tab-generate"]').classes()).toContain('active')
    expect((wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value).toBe('一张复古海报')
    expect(wrapper.text()).toContain('还没有图片')
  })
})

function mountView() {
  const wrapper = mount(ImageStudioView)
  mountedWrappers.push(wrapper)
  return wrapper
}

function templateDrawer() {
  return new DOMWrapper(document.body).find('[data-testid="template-drawer"]')
}

function waitForAsyncIndexedDb() {
  return new Promise((resolve) => setTimeout(resolve, 10))
}

function createIndexedDbStub() {
  const stores = new Map<string, Map<string, any>>()
  const db = {
    objectStoreNames: {
      contains: (storeName: string) => stores.has(storeName)
    },
    createObjectStore: (storeName: string) => {
      if (!stores.has(storeName)) stores.set(storeName, new Map())
      return {}
    },
    getStore: (storeName: string) => {
      const store = stores.get(storeName) ?? new Map<string, any>()
      stores.set(storeName, store)
      return store
    },
    transaction: (storeName: string) => {
      const store = db.getStore(storeName)
      const transaction: any = {
        error: null,
        objectStore: () => ({
          put: (value: any) => {
            store.set(value.id, value)
          },
          get: (key: string) => {
            const getRequest: any = {}
            setTimeout(() => {
              getRequest.result = store.get(key)
              getRequest.onsuccess?.()
            }, 0)
            return getRequest
          },
          delete: (key: string) => {
            store.delete(key)
          },
          openCursor: () => {
            const cursorRequest: any = {}
            const entries = [...store.keys()]
            let index = 0
            const advance = () => {
              const key = entries[index]
              cursorRequest.result = key
                ? {
                    primaryKey: key,
                    delete: () => store.delete(key),
                    continue: () => {
                      index += 1
                      setTimeout(advance, 0)
                    }
                  }
                : null
              cursorRequest.onsuccess?.()
              if (!key) transaction.oncomplete?.()
            }
            setTimeout(advance, 0)
            return cursorRequest
          }
        })
      }
      return transaction
    }
  }
  return {
    open: vi.fn((_name: string, _version: number) => {
      const request: any = {}
      setTimeout(() => {
        request.result = db
        request.onupgradeneeded?.()
        request.onsuccess?.()
      }, 0)
      return request
    }),
    getDb: () => db
  }
}

async function openStubDb(indexedDbStub: ReturnType<typeof createIndexedDbStub>) {
  return new Promise<any>((resolve) => {
    const request = indexedDbStub.open('sub2api-image-studio', 1)
    request.onsuccess = () => resolve(request.result)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains('history-images')) {
        request.result.createObjectStore('history-images', { keyPath: 'id' })
      }
    }
  })
}
