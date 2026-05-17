import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ImageStudioView from '../ImageStudioView.vue'
import { sendResponsesImageRequest } from '../imageStudioApi'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => ({
      'imageStudio.title': '生图工作台',
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
  sendImagesGenerationRequest: vi.fn(),
  sendImagesEditRequest: vi.fn()
}))

const keysAPI = await import('@/api').then((mod) => mod.keysAPI)

describe('ImageStudioView', () => {
  beforeEach(() => {
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
            allow_image_generation: true
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
      output: [{ id: 'ig_1', type: 'image_generation_call', result: 'ZmFrZQ==', output_format: 'png' }]
    })
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
  })

  it('switches to image-to-image mode', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="tab-edit"]').trigger('click')

    expect(wrapper.text()).toContain('上传原图')
    expect(wrapper.text()).toContain('上传蒙版')
  })

  it('disables submit until prompt is entered', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')

    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeUndefined()
  })

  it('renders generated image outputs after submit', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    expect(sendResponsesImageRequest).toHaveBeenCalledWith(expect.objectContaining({
      apiKey: 'sk-active'
    }))
    expect(wrapper.find('img[alt="Generated image 1"]').attributes('src')).toBe('data:image/png;base64,ZmFrZQ==')
  })
})

function mountView() {
  return mount(ImageStudioView)
}
