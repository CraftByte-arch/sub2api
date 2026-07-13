import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { DOMWrapper, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ImageStudioView from '../ImageStudioView.vue'
import {
  createImageStudioJob,
  deleteImageStudioJob,
  fetchImageStudioOriginal,
  fetchImageStudioThumbnail,
  getImageStudioJobStats,
  getImageStudioJob,
  listGatewayModels,
  listImageStudioJobs,
  sendPromptPolishRequest
} from '../imageStudioApi'

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

vi.mock('@/api/auth', () => ({
  getPublicSettings: vi.fn()
}))

vi.mock('../imageStudioApi', () => ({
  createImageStudioJob: vi.fn(),
  deleteImageStudioJob: vi.fn(),
  getImageStudioJobStats: vi.fn(),
  getImageStudioJob: vi.fn(),
  listGatewayModels: vi.fn(),
  listImageStudioJobs: vi.fn(),
  fetchImageStudioOriginal: vi.fn(),
  fetchImageStudioThumbnail: vi.fn(),
  sendPromptPolishRequest: vi.fn()
}))

vi.mock('../imageStudioCache', () => ({
  clearImageStudioAssetCache: vi.fn(async () => 0),
  deleteImageStudioAssetCache: vi.fn(async () => undefined),
  getCachedImageStudioAsset: vi.fn(async () => null),
  putImageStudioAssetCache: vi.fn(async () => undefined)
}))

const keysAPI = await import('@/api').then((mod) => mod.keysAPI)
const getPublicSettings = await import('@/api/auth').then((mod) => mod.getPublicSettings)
const imageStudioCache = await import('../imageStudioCache')

let mountedWrappers: VueWrapper[] = []
let jobCounter = 100
let jobs: any[] = []
let originalBlobByJobId = new Map<number, Blob>()

describe('ImageStudioView', () => {
  beforeEach(() => {
    window.localStorage.clear()
    vi.mocked(createImageStudioJob).mockReset()
    vi.mocked(deleteImageStudioJob).mockReset()
    vi.mocked(getImageStudioJobStats).mockReset()
    vi.mocked(getImageStudioJob).mockReset()
    vi.mocked(listGatewayModels).mockReset()
    vi.mocked(listImageStudioJobs).mockReset()
    vi.mocked(fetchImageStudioOriginal).mockReset()
    vi.mocked(fetchImageStudioThumbnail).mockReset()
    vi.mocked(sendPromptPolishRequest).mockReset()
    vi.mocked(getPublicSettings).mockReset()
    vi.mocked(imageStudioCache.clearImageStudioAssetCache).mockReset()
    vi.mocked(imageStudioCache.deleteImageStudioAssetCache).mockReset()
    vi.mocked(imageStudioCache.getCachedImageStudioAsset).mockReset()
    vi.mocked(imageStudioCache.putImageStudioAssetCache).mockReset()
    vi.mocked(imageStudioCache.clearImageStudioAssetCache).mockResolvedValue(0)
    vi.mocked(imageStudioCache.deleteImageStudioAssetCache).mockResolvedValue(undefined)
    vi.mocked(imageStudioCache.getCachedImageStudioAsset).mockResolvedValue(null)
    vi.mocked(imageStudioCache.putImageStudioAssetCache).mockResolvedValue(undefined)
    vi.mocked(getPublicSettings).mockResolvedValue({
      image_studio_available_group_ids: [10]
    } as any)
    vi.mocked(listGatewayModels).mockResolvedValue(['gpt-5.6-sol'])

    Object.defineProperty(URL, 'createObjectURL', {
      value: vi.fn((value: Blob | File) => {
        if (value instanceof File) return `blob:${value.name}`
        return `data:${value.type || 'application/octet-stream'};base64,ZmFrZQ==`
      }),
      configurable: true
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      value: vi.fn(),
      configurable: true
    })
    class ImageBitmapStub {
      width = 32
      height = 32
      close = vi.fn()
    }
    vi.stubGlobal('ImageBitmap', ImageBitmapStub)
    vi.stubGlobal('createImageBitmap', vi.fn(async () => new ImageBitmapStub()))
    HTMLCanvasElement.prototype.getContext = vi.fn(() => ({
      drawImage: vi.fn()
    })) as any
    HTMLCanvasElement.prototype.toBlob = vi.fn(function (callback: BlobCallback) {
      callback?.(new Blob(['converted'], { type: 'image/webp' }))
    }) as any

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
        }
      ],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    } as any)

    jobCounter = 100
    jobs = []
    originalBlobByJobId = new Map()

    vi.mocked(listImageStudioJobs).mockImplementation(async (_page = 1, pageSize = 20) => ({
      items: jobs.slice(0, pageSize),
      total: jobs.length,
      page: 1,
      page_size: pageSize,
      pages: 1
    }))

    vi.mocked(createImageStudioJob).mockImplementation(async (input: any) => {
      const id = ++jobCounter
      const outputFormat = input.outputFormat || 'png'
      const mimeType = outputFormat === 'webp' ? 'image/webp' : outputFormat === 'jpeg' ? 'image/jpeg' : 'image/png'
      const job = {
        id,
        mode: input.mode,
        status: 'succeeded',
        prompt: input.prompt,
        model: input.model,
        size: input.size,
        outputFormat,
        estimatedCostUsd: 1,
        chargedAmountUsd: 1,
        mimeType,
        width: 1024,
        height: 1024,
        queuedAt: '2026-06-11T00:00:00Z',
        startedAt: '2026-06-11T00:00:01Z',
        completedAt: '2026-06-11T00:00:02Z',
        thumbnailUrl: `/api/v1/image-studio/jobs/${id}/thumbnail`,
        originalUrl: `/api/v1/image-studio/jobs/${id}/original`
      }
      jobs = [job, ...jobs]
      originalBlobByJobId.set(id, new Blob(['fake'], { type: mimeType }))
      return job as any
    })

    vi.mocked(getImageStudioJob).mockImplementation(async (id: number) => {
      const job = jobs.find((item) => item.id === id)
      if (!job) throw new Error('job not found')
      return job as any
    })

    vi.mocked(fetchImageStudioOriginal).mockImplementation(async (id: number) => {
      const blob = originalBlobByJobId.get(id)
      if (!blob) throw new Error('original not found')
      return blob
    })

    vi.mocked(fetchImageStudioThumbnail).mockImplementation(async (id: number) => {
      const job = jobs.find((item) => item.id === id)
      return new Blob(['thumb'], { type: job?.mimeType || 'image/png' })
    })

    vi.mocked(getImageStudioJobStats).mockImplementation(async () => ({
      pendingCount: jobs.filter((job) => job.status === 'queued' || job.status === 'running').length,
      failedCount: jobs.filter((job) => job.status === 'failed').length
    }))
  })

  afterEach(() => {
    vi.useRealTimers()
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
    expect(wrapper.text()).toContain('还没有图片')
    expect(wrapper.text()).toContain('$1.00')
  })

  it('only lists API keys from groups that allow image generation', async () => {
    vi.mocked(getPublicSettings).mockResolvedValueOnce({
      image_studio_available_group_ids: [23]
    } as any)
    vi.mocked(keysAPI.list).mockResolvedValueOnce({
      items: [
        {
          id: 11,
          key: 'sk-chat',
          name: '普通对话',
          status: 'active',
          group: {
            id: 21,
            name: '默认对话组',
            platform: 'openai',
            allow_image_generation: false
          }
        },
        {
          id: 12,
          key: 'sk-anthropic',
          name: 'Claude Key',
          status: 'active',
          group: {
            id: 22,
            name: 'Claude 组',
            platform: 'anthropic',
            allow_image_generation: true
          }
        },
        {
          id: 13,
          key: 'sk-image',
          name: '生图专用',
          status: 'active',
          group: {
            id: 23,
            name: '生图组',
            platform: 'openai',
            allow_image_generation: true,
            image_price_1k: 1
          }
        }
      ],
      total: 3,
      page: 1,
      page_size: 20,
      pages: 1
    } as any)

    const wrapper = mountView()
    await flushPromises()

    const options = wrapper.get('[data-testid="api-key-select"]').findAll('option')
    const selectableOptions = options.filter((option) => option.attributes('value'))
    expect(selectableOptions.map((option) => option.text())).toEqual(['生图专用'])
    expect((wrapper.get('[data-testid="api-key-select"]').element as HTMLSelectElement).value).toBe('sk-image')
  })

  it('only lists API keys from image studio available groups', async () => {
    vi.mocked(getPublicSettings).mockResolvedValueOnce({
      image_studio_available_group_ids: [23]
    } as any)
    vi.mocked(keysAPI.list).mockResolvedValueOnce({
      items: [
        {
          id: 13,
          key: 'sk-allowed',
          name: '允许体验',
          status: 'active',
          group: {
            id: 23,
            name: '体验分组',
            platform: 'openai',
            allow_image_generation: true,
            image_price_1k: 1
          }
        },
        {
          id: 14,
          key: 'sk-denied',
          name: '未开放体验',
          status: 'active',
          group: {
            id: 24,
            name: '后台生图但不开放体验',
            platform: 'openai',
            allow_image_generation: true,
            image_price_1k: 1
          }
        }
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    } as any)

    const wrapper = mountView()
    await flushPromises()

    const options = wrapper.get('[data-testid="api-key-select"]').findAll('option')
    const selectableOptions = options.filter((option) => option.attributes('value'))
    expect(selectableOptions.map((option) => option.text())).toEqual(['允许体验'])
    expect((wrapper.get('[data-testid="api-key-select"]').element as HTMLSelectElement).value).toBe('sk-allowed')
  })

  it('shows no selectable API key when no image studio group is available', async () => {
    vi.mocked(getPublicSettings).mockResolvedValueOnce({
      image_studio_available_group_ids: []
    } as any)

    const wrapper = mountView()
    await flushPromises()

    const options = wrapper.get('[data-testid="api-key-select"]').findAll('option')
    const selectableOptions = options.filter((option) => option.attributes('value'))
    expect(selectableOptions).toHaveLength(0)
    expect(wrapper.text()).toContain('没有可用于生图的 API 密钥')
    expect((wrapper.get('[data-testid="api-key-select"]').element as HTMLSelectElement).value).toBe('')
  })

  it('loads compatible prompt polish keys from every API-key page', async () => {
    vi.mocked(keysAPI.list).mockImplementation(async (page = 1) => {
      if (page === 1) {
        return {
          items: [{
            id: 1,
            key: 'sk-page-one',
            name: '生图 Key',
            status: 'active',
            group: {
              id: 10,
              name: '生图分组',
              platform: 'openai',
              allow_image_generation: true
            }
          }],
          total: 2,
          page: 1,
          page_size: 1000,
          pages: 2
        } as any
      }

      return {
        items: [{
          id: 2,
          key: 'sk-page-two',
          name: 'Sol',
          status: 'active',
          group: { id: 20, name: '文本分组', platform: 'openai' }
        }],
        total: 2,
        page: 2,
        page_size: 1000,
        pages: 2
      } as any
    })
    vi.mocked(listGatewayModels).mockImplementation(async (key) => ({
      'sk-page-one': ['gpt-image-2'],
      'sk-page-two': ['gpt-5.6-sol']
    }[key] || []))

    const wrapper = mountView()
    await flushPromises()

    const options = wrapper.get('[data-testid="prompt-polish-key-select"]').findAll('option')
    expect(options.filter((option) => option.attributes('value')).map((option) => option.text())).toEqual([
      'Sol · 文本分组'
    ])
    expect(keysAPI.list).toHaveBeenCalledWith(1, 1000)
    expect(keysAPI.list).toHaveBeenCalledWith(2, 1000)
  })

  it('only offers the three 5.6 prompt polish models in bottom-up Luna, Terra, Sol order', async () => {
    vi.mocked(keysAPI.list).mockResolvedValueOnce({
      items: [
        {
          id: 1,
          key: 'sk-image',
          name: '生图 Key',
          status: 'active',
          group: {
            id: 10,
            name: '生图分组',
            platform: 'openai',
            allow_image_generation: true,
            image_price_1k: 1
          }
        },
        {
          id: 2,
          key: 'sk-sol-a',
          name: 'Sol A',
          status: 'active',
          group: { id: 20, name: '文本分组', platform: 'openai' }
        },
        {
          id: 3,
          key: 'sk-sol-b',
          name: 'Sol B',
          status: 'active',
          group: { id: 20, name: '文本分组', platform: 'openai' }
        },
        {
          id: 4,
          key: 'sk-luna',
          name: 'Luna',
          status: 'active',
          group: { id: 30, name: 'Luna 分组', platform: 'openai' }
        }
      ],
      total: 4,
      page: 1,
      page_size: 20,
      pages: 1
    } as any)
    vi.mocked(listGatewayModels).mockImplementation(async (key) => ({
      'sk-image': ['gpt-image-2'],
      'sk-sol-a': ['gpt-5.6-sol', 'gpt-5.5'],
      'sk-luna': ['gpt-5.6-luna']
    }[key] || []))

    const wrapper = mountView()
    await flushPromises()

    const modelSelect = wrapper.get('[data-testid="prompt-polish-model-select"]')
    expect(modelSelect.findAll('option').map((option) => option.attributes('value'))).toEqual([
      'gpt-5.6-sol',
      'gpt-5.6-terra',
      'gpt-5.6-luna'
    ])
    expect((modelSelect.element as HTMLSelectElement).value).toBe('gpt-5.6-sol')

    await wrapper.get('.prompt-polish-model-select').trigger('click')
    expect(wrapper.findAll('[role="option"]').map((option) => option.text()).reverse()).toEqual([
      '5.6 Luna',
      '5.6 Terra',
      '5.6 Sol'
    ])

    await modelSelect.setValue('gpt-5.6-sol')
    await flushPromises()

    const polishKeySelect = wrapper.get('[data-testid="prompt-polish-key-select"]')
    const selectableOptions = polishKeySelect.findAll('option').filter((option) => option.attributes('value'))
    expect(selectableOptions.map((option) => option.text())).toEqual([
      'Sol A · 文本分组',
      'Sol B · 文本分组'
    ])
    expect(listGatewayModels).toHaveBeenCalledTimes(3)

    await modelSelect.setValue('gpt-5.6-luna')
    await flushPromises()

    expect(polishKeySelect.findAll('option').filter((option) => option.attributes('value')).map((option) => option.text())).toEqual([
      'Luna · Luna 分组'
    ])

    await modelSelect.setValue('gpt-5.6-terra')
    await flushPromises()
    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')

    expect(polishKeySelect.findAll('option').filter((option) => option.attributes('value'))).toHaveLength(0)
    expect((polishKeySelect.element as HTMLSelectElement).disabled).toBe(true)
    expect(wrapper.get('[data-testid="polish-prompt-button"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeUndefined()
  })

  it('uses the selected prompt polish key instead of the image key', async () => {
    vi.mocked(keysAPI.list).mockResolvedValueOnce({
      items: [
        {
          id: 1,
          key: 'sk-image',
          name: '生图 Key',
          status: 'active',
          group: {
            id: 10,
            name: '生图分组',
            platform: 'openai',
            allow_image_generation: true,
            image_price_1k: 1
          }
        },
        {
          id: 2,
          key: 'sk-sol-a',
          name: 'Sol A',
          status: 'active',
          group: { id: 20, name: '文本分组', platform: 'openai' }
        },
        {
          id: 3,
          key: 'sk-sol-b',
          name: 'Sol B',
          status: 'active',
          group: { id: 20, name: '文本分组', platform: 'openai' }
        }
      ],
      total: 3,
      page: 1,
      page_size: 20,
      pages: 1
    } as any)
    vi.mocked(listGatewayModels).mockImplementation(async (key) => ({
      'sk-image': ['gpt-image-2'],
      'sk-sol-a': ['gpt-5.6-sol'],
      'sk-sol-b': ['gpt-5.6-sol']
    }[key] || []))
    vi.mocked(sendPromptPolishRequest).mockResolvedValue({ output_text: '润色后的提示词' })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-polish-model-select"]').setValue('gpt-5.6-sol')
    await flushPromises()
    await wrapper.get('[data-testid="prompt-polish-key-select"]').setValue('sk-sol-b')
    await wrapper.get('[data-testid="prompt-input"]').setValue('原始提示词')
    await wrapper.get('[data-testid="polish-prompt-button"]').trigger('click')
    await flushPromises()

    expect(sendPromptPolishRequest).toHaveBeenCalledWith(expect.objectContaining({
      apiKey: 'sk-sol-b',
      body: expect.objectContaining({ model: 'gpt-5.6-sol' })
    }))
    expect(createImageStudioJob).not.toHaveBeenCalled()
  })

  it('submits a generate job through the site API and adds the queued task to history', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('一座安静茶室')
    await wrapper.get('.advanced-header').trigger('click')
    await wrapper.get('[data-testid="quality-select"]').setValue('high')
    await wrapper.get('[data-testid="output-format-select"]').setValue('webp')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    expect(createImageStudioJob).toHaveBeenCalledWith(expect.objectContaining({
      apiKeyId: 1,
      mode: 'generate',
      prompt: '一座安静茶室',
      model: 'gpt-image-2',
      quality: 'high',
      outputFormat: 'webp',
      size: '1024x1024'
    }))
    expect(wrapper.find('[data-testid="generating-preview"]').exists()).toBe(false)

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('一座安静茶室')
  })

  it('guides users to history after a job is submitted', async () => {
    vi.mocked(createImageStudioJob).mockImplementationOnce(async (input: any) => {
      const id = ++jobCounter
      const job = {
        id,
        mode: input.mode,
        status: 'queued',
        attemptCount: 0,
        maxAttempts: 3,
        prompt: input.prompt,
        model: input.model,
        size: input.size,
        outputFormat: input.outputFormat || 'png',
        estimatedCostUsd: 1,
        chargedAmountUsd: 0,
        queuedAt: '2026-06-11T00:00:00Z'
      }
      jobs = [job, ...jobs]
      return job as any
    })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('需要明显引导的任务')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="history-guidance"]').text()).toContain('任务已进入历史记录')
    expect(wrapper.get('[data-testid="history-nav-guidance"]').text()).toContain('生成中：1')
    expect(wrapper.get('[data-testid="tab-history"]').classes()).toContain('has-pending-jobs')

    await wrapper.get('[data-testid="history-guidance-open"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="tab-history"]').classes()).toContain('active')
    expect(wrapper.get('[data-testid="history-card"]').classes()).toContain('is-new')
    expect(wrapper.text()).toContain('需要明显引导的任务')
  })

  it('centers history guidance within the studio main area instead of pinning it to the viewport left edge', async () => {
    vi.mocked(createImageStudioJob).mockImplementationOnce(async (input: any) => {
      const id = ++jobCounter
      const job = {
        id,
        mode: input.mode,
        status: 'queued',
        attemptCount: 0,
        maxAttempts: 3,
        prompt: input.prompt,
        model: input.model,
        size: input.size,
        outputFormat: input.outputFormat || 'png',
        estimatedCostUsd: 1,
        chargedAmountUsd: 0,
        queuedAt: '2026-06-11T00:00:00Z'
      }
      jobs = [job, ...jobs]
      return job as any
    })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('检查引导定位')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    const guidance = wrapper.get('[data-testid="history-guidance"]')
    expect(guidance.classes()).toContain('history-guidance')
    expect(guidance.attributes('style') || '').toContain('--history-guidance-max-width')
  })

  it('renders backend job stats on the history tab badge', async () => {
    vi.mocked(getImageStudioJobStats).mockResolvedValue({
      pendingCount: 0,
      failedCount: 2
    })

    const wrapper = mountView()
    await flushPromises()

    expect(getImageStudioJobStats).toHaveBeenCalled()
    expect(wrapper.get('[data-testid="history-nav-guidance"]').text()).toContain('失败：2')

    vi.mocked(getImageStudioJobStats).mockResolvedValue({
      pendingCount: 3,
      failedCount: 1
    })

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="history-nav-guidance"]').text()).toContain('生成中：3')
    expect(wrapper.get('[data-testid="history-nav-guidance"]').text()).toContain('失败：1')
  })

  it('renders history navigation guidance outside the button with explicit status labels', async () => {
    vi.mocked(getImageStudioJobStats).mockResolvedValue({
      pendingCount: 1,
      failedCount: 9
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="history-nav-guidance"]').text()).toContain('任务进度请到历史记录查看')
    expect(wrapper.get('[data-testid="history-nav-guidance"]').text()).toContain('生成中：1')
    expect(wrapper.get('[data-testid="history-nav-guidance"]').text()).toContain('失败：9')
    expect(wrapper.get('[data-testid="tab-history"]').text()).toContain('历史记录')
    expect(wrapper.get('[data-testid="tab-history"]').text()).toContain('历史记录')
  })

  it('animates submitted jobs from the composer area into the history tab', async () => {
    vi.useFakeTimers()
    const wrapper = mountView()
    await flushPromises()

    const composerAnchor = wrapper.get('[data-testid="composer-transfer-anchor"]').element as HTMLElement
    const historyTab = wrapper.get('[data-testid="tab-history"]').element as HTMLElement
    vi.spyOn(composerAnchor, 'getBoundingClientRect').mockReturnValue({
      x: 80,
      y: 520,
      width: 640,
      height: 112,
      top: 520,
      right: 720,
      bottom: 632,
      left: 80,
      toJSON: () => ({})
    } as DOMRect)
    vi.spyOn(historyTab, 'getBoundingClientRect').mockReturnValue({
      x: 930,
      y: 118,
      width: 118,
      height: 42,
      top: 118,
      right: 1048,
      bottom: 160,
      left: 930,
      toJSON: () => ({})
    } as DOMRect)

    await wrapper.get('[data-testid="prompt-input"]').setValue('传送动效任务')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(0)
    await flushPromises()

    const transfer = wrapper.get('[data-testid="history-transfer-effect"]')
    expect(transfer.exists()).toBe(true)
    expect(transfer.classes()).toContain('history-transfer-effect')
    expect(wrapper.find('[data-testid="history-transfer-trail"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="history-transfer-aura"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="history-transfer-orb"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="history-transfer-comet"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="generate-button"]').text()).toContain('生成中...')
    expect(transfer.attributes('style')).toContain('--transfer-start-x: 400px')
    expect(transfer.attributes('style')).toContain('--transfer-start-y: 576px')
    expect(transfer.attributes('style')).toContain('--transfer-end-x: 989px')
    expect(transfer.attributes('style')).toContain('--transfer-end-y: 139px')
    expect(wrapper.get('[data-testid="composer-transfer-anchor"]').classes()).toContain('transfer-origin-active')

    await vi.advanceTimersByTimeAsync(2220)
    await flushPromises()
    expect(wrapper.get('[data-testid="generate-button"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="generate-button"]').text()).toContain('生成图片')
  })

  it('opens the original image from history lazily after the async job succeeds', async () => {
    jobs = [{
      id: 206,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '历史里的成品图',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'webp',
      mimeType: 'image/webp',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/206/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/206/original'
    }]
    originalBlobByJobId.set(206, new Blob(['fake'], { type: 'image/webp' }))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="history-preview-image"]').trigger('click')
    await flushPromises()

    expect(fetchImageStudioOriginal).toHaveBeenCalledWith(206)
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('src')).toBe('data:image/webp;base64,ZmFrZQ==')
  })

  it('downloads a converted history image without fetching the blob URL', async () => {
    const fetchMock = vi.fn()
    const anchorClick = vi.fn()
    const originalCreateElement = document.createElement.bind(document)
    vi.stubGlobal('fetch', fetchMock)
    vi.spyOn(document, 'createElement').mockImplementation((tagName: string, options?: ElementCreationOptions) => {
      const element = originalCreateElement(tagName, options)
      if (tagName.toLowerCase() === 'a') {
        Object.defineProperty(element, 'click', {
          value: anchorClick,
          configurable: true
        })
      }
      return element
    })
    vi.mocked(URL.createObjectURL).mockImplementation((value: Blob | File) => {
      if (value instanceof File) return `blob:${value.name}`
      return value.type === 'image/png' ? 'blob:history-original-209' : 'blob:converted-209'
    })

    jobs = [{
      id: 209,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '需要下载原图',
      model: 'gpt-image-2',
      size: '2160x3840',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/209/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/209/original'
    }]
    originalBlobByJobId.set(209, new Blob(['original'], { type: 'image/png' }))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="history-preview-image"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="lightbox-download"]').trigger('click')
    await wrapper.get('[data-testid="lightbox-download-webp-button"]').trigger('click')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalledWith('blob:history-original-209', expect.anything())
    expect(anchorClick).toHaveBeenCalledTimes(1)
  })

  it('accepts reference images and submits an edit job', async () => {
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
    await wrapper.get('[data-testid="prompt-input"]').setValue('保留构图，改成雨夜电影感')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()
    await waitForNextTask()
    await flushPromises()

    expect(wrapper.text()).toContain('参考图1/4')
    expect(wrapper.text()).not.toContain('生成失败')
    expect(createImageStudioJob).toHaveBeenCalledWith(expect.objectContaining({
      apiKeyId: 1,
      mode: 'edit',
      prompt: '保留构图，改成雨夜电影感',
      imageDataUrls: expect.any(Array)
    }))
  })

  it('renders backend job history with thumbnails and opens original image lazily', async () => {
    jobs = [{
      id: 201,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '一张复古海报',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/201/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/201/original'
    }]
    originalBlobByJobId.set(201, new Blob(['fake'], { type: 'image/png' }))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="history-grid"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('一张复古海报')
    expect(wrapper.find('.history-card img').attributes('src')).toBe('data:image/png;base64,ZmFrZQ==')

    await wrapper.get('[data-testid="history-preview-image"]').trigger('click')
    await flushPromises()

    expect(fetchImageStudioOriginal).toHaveBeenCalledWith(201)
    expect(wrapper.get('[data-testid="lightbox-image"]').attributes('src')).toBe('data:image/png;base64,ZmFrZQ==')
  })

  it('edits a history image using the original blob without fetching the blob URL', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).startsWith('blob:')) {
        throw new TypeError('Refused to connect because it violates the document Content Security Policy')
      }
      return new Response(new Blob(['fallback'], { type: 'image/png' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.mocked(URL.createObjectURL).mockImplementation((value: Blob | File) => {
      if (value instanceof File) return `blob:${value.name}`
      return 'blob:history-original-211'
    })
    jobs = [{
      id: 211,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '历史图片继续编辑',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/211/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/211/original'
    }]
    originalBlobByJobId.set(211, new Blob(['original'], { type: 'image/png' }))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="history-edit-button"]').trigger('click')
    await flushPromises()

    expect(fetchImageStudioOriginal).toHaveBeenCalledWith(211)
    expect(fetchMock).not.toHaveBeenCalledWith('blob:history-original-211')
    expect(wrapper.text()).toContain('参考图1/4')
    expect((wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value).toBe('历史图片继续编辑')
    expect(wrapper.text()).not.toContain('生成失败')
  })

  it('does not render a frontend history limit control', async () => {
    jobs = Array.from({ length: 35 }, (_, index) => ({
      id: 300 + index,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: `历史任务 ${index + 1}`,
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: `/api/v1/image-studio/jobs/${300 + index}/thumbnail`,
      originalUrl: `/api/v1/image-studio/jobs/${300 + index}/original`
    }))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('历史上限')
    expect(wrapper.text()).toContain('35 条任务记录')
  })

  it('loads more history records from the next backend page on demand', async () => {
    jobs = Array.from({ length: 75 }, (_, index) => ({
      id: 400 + index,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: `分页任务 ${index + 1}`,
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: `/api/v1/image-studio/jobs/${400 + index}/thumbnail`,
      originalUrl: `/api/v1/image-studio/jobs/${400 + index}/original`
    }))
    vi.mocked(listImageStudioJobs).mockImplementation(async (page = 1, pageSize = 20) => {
      const start = (page - 1) * pageSize
      return {
        items: jobs.slice(start, start + pageSize),
        total: jobs.length,
        page,
        page_size: pageSize,
        pages: Math.ceil(jobs.length / pageSize)
      }
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('已显示 50 / 共 75 条')
    expect(wrapper.text()).toContain('分页任务 50')
    expect(wrapper.text()).not.toContain('分页任务 51')

    await wrapper.get('[data-testid="history-load-more-button"]').trigger('click')
    await flushPromises()

    expect(listImageStudioJobs).toHaveBeenCalledWith(2, 50)
    expect(wrapper.text()).toContain('已显示 75 / 共 75 条')
    expect(wrapper.text()).toContain('分页任务 75')
    expect(wrapper.find('[data-testid="history-load-more-button"]').exists()).toBe(false)
  })

  it('refreshes the currently displayed history pages on demand', async () => {
    jobs = Array.from({ length: 60 }, (_, index) => ({
      id: 500 + index,
      mode: 'generate',
      status: index === 0 ? 'queued' : 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: `刷新任务 ${index + 1}`,
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: index === 0 ? 0 : 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: index === 0 ? undefined : '2026-06-11T00:00:02Z',
      thumbnailUrl: index === 0 ? undefined : `/api/v1/image-studio/jobs/${500 + index}/thumbnail`,
      originalUrl: index === 0 ? undefined : `/api/v1/image-studio/jobs/${500 + index}/original`
    }))
    vi.mocked(listImageStudioJobs).mockImplementation(async (page = 1, pageSize = 20) => {
      const start = (page - 1) * pageSize
      return {
        items: jobs.slice(start, start + pageSize),
        total: jobs.length,
        page,
        page_size: pageSize,
        pages: Math.ceil(jobs.length / pageSize)
      }
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="history-load-more-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('刷新任务 60')
    expect(wrapper.text()).toContain('排队中')

    jobs = jobs.map((job, index) => index === 0
      ? {
        ...job,
        status: 'succeeded',
        chargedAmountUsd: 1,
        completedAt: '2026-06-11T00:00:02Z',
        thumbnailUrl: '/api/v1/image-studio/jobs/500/thumbnail',
        originalUrl: '/api/v1/image-studio/jobs/500/original'
      }
      : job)

    vi.mocked(listImageStudioJobs).mockClear()
    await wrapper.get('[data-testid="history-refresh-button"]').trigger('click')
    await flushPromises()

    expect(listImageStudioJobs).toHaveBeenCalledWith(1, 50)
    expect(listImageStudioJobs).toHaveBeenCalledWith(2, 50)
    expect(wrapper.text()).not.toContain('排队中')
    expect(wrapper.text()).toContain('已完成')
    expect(wrapper.text()).toContain('刷新任务 60')
  })

  it('deletes one history record through the backend and keeps cache cleanup local-only', async () => {
    jobs = [{
      id: 208,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '待删除历史',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/208/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/208/original'
    }]
    vi.mocked(deleteImageStudioJob).mockImplementation(async (id: number) => {
      jobs = jobs.filter((job) => job.id !== id)
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="history-cache-clean-button"]').trigger('click')
    await flushPromises()
    expect(deleteImageStudioJob).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="history-card-delete-button"]').trigger('click')
    await flushPromises()

    expect(deleteImageStudioJob).toHaveBeenCalledWith(208)
    expect(wrapper.text()).not.toContain('待删除历史')
  })

  it('renders thumbnails via authenticated blob fetch without falling back to protected urls', async () => {
    jobs = [{
      id: 207,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '缩略图失败测试',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/207/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/207/original'
    }]
    vi.mocked(fetchImageStudioThumbnail).mockResolvedValueOnce(new Blob(['thumb'], { type: 'image/jpeg' }))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(fetchImageStudioThumbnail).toHaveBeenCalledWith(207)
    expect(wrapper.find('.history-card img').attributes('src')).toBe('data:image/jpeg;base64,ZmFrZQ==')
  })

  it('shows retry and expired asset states in history', async () => {
    jobs = [{
      id: 203,
      mode: 'generate',
      status: 'queued',
      attemptCount: 1,
      maxAttempts: 3,
      nextAttemptAt: '2026-06-11T00:00:10Z',
      prompt: '重试中的海报',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/203/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/203/original'
    }, {
      id: 204,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '已过期的海报',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      assetsDeletedAt: '2026-06-11T01:00:00Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/204/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/204/original'
    }]
    originalBlobByJobId.set(203, new Blob(['fake'], { type: 'image/png' }))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('排队重试中')
    expect(wrapper.text()).toContain('图片已过期清理')

    await wrapper.findAll('[data-testid="history-card"]')[1].trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('图片文件已按全站保留策略清理')
    await wrapper.findAll('[data-testid="history-preview-image"]')[1].trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="lightbox-download"]').exists()).toBe(false)
  })

  it('renders proportional placeholders and regenerates failed or expired history jobs', async () => {
    jobs = [{
      id: 301,
      mode: 'generate',
      status: 'failed',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '失败后重新生成',
      model: 'gpt-image-2',
      size: '1536x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 0,
      queuedAt: '2026-06-11T00:00:00Z',
      errorMessage: '上游超时'
    }, {
      id: 302,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '文件过期后重新生成',
      model: 'gpt-image-2',
      size: '1024x1536',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      assetsDeletedAt: '2026-06-11T01:00:00Z'
    }]

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    const placeholders = wrapper.findAll('[data-testid="history-preview-placeholder"]')
    expect(placeholders).toHaveLength(2)
    expect(placeholders[0].text()).toContain('失败')
    expect(placeholders[1].text()).toContain('重新生成')
    expect(placeholders[0].attributes('style')).toContain('--history-preview-aspect-ratio: 1536 / 1024')
    expect(placeholders[1].attributes('style')).toContain('--history-preview-aspect-ratio: 1024 / 1536')

    const regenerateButtons = wrapper.findAll('[data-testid="history-regenerate-button"]')
    expect(regenerateButtons).toHaveLength(2)

    await regenerateButtons[0].trigger('click')
    await flushPromises()

    expect(createImageStudioJob).toHaveBeenCalledWith(expect.objectContaining({
      apiKeyId: 1,
      mode: 'generate',
      prompt: '失败后重新生成',
      model: 'gpt-image-2',
      size: '1536x1024',
      outputFormat: 'png'
    }))
    expect(wrapper.text()).toContain('失败后重新生成')

    await regenerateButtons[1].trigger('click')
    await flushPromises()

    expect(createImageStudioJob).toHaveBeenCalledWith(expect.objectContaining({
      apiKeyId: 1,
      mode: 'generate',
      prompt: '文件过期后重新生成',
      model: 'gpt-image-2',
      size: '1024x1536',
      outputFormat: 'png'
    }))
  })

  it('still fetches thumbnails when the persisted image cache is unavailable', async () => {
    vi.mocked(imageStudioCache.getCachedImageStudioAsset).mockRejectedValue(new Error('missing object store'))
    jobs = [{
      id: 303,
      mode: 'generate',
      status: 'succeeded',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '缩略图缺失但未过期',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'jpeg',
      mimeType: 'image/jpeg',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/303/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/303/original'
    }]

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(fetchImageStudioThumbnail).toHaveBeenCalledWith(303)
    expect(fetchImageStudioOriginal).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="history-preview-placeholder"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="history-card"] img').attributes('src')).toContain('data:image/jpeg;base64')
  })

  it('reuses a history prompt back in text-to-image mode', async () => {
    jobs = [{
      id: 202,
      mode: 'generate',
      status: 'succeeded',
      prompt: '一张复古海报',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      mimeType: 'image/png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 1,
      queuedAt: '2026-06-11T00:00:00Z',
      completedAt: '2026-06-11T00:00:02Z',
      thumbnailUrl: '/api/v1/image-studio/jobs/202/thumbnail',
      originalUrl: '/api/v1/image-studio/jobs/202/original'
    }]
    originalBlobByJobId.set(202, new Blob(['fake'], { type: 'image/png' }))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="history-reuse-button"]').trigger('click')

    expect(wrapper.get('[data-testid="tab-generate"]').classes()).toContain('active')
    expect((wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value).toBe('一张复古海报')
  })

  it('stores prompt history after job submission', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('森林里的玻璃房子')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    const stored = JSON.parse(window.localStorage.getItem('sub2api:image-studio:prompt-history:v1') || '[]')
    expect(stored).toMatchObject([
      expect.objectContaining({
        prompt: '森林里的玻璃房子',
        source: 'generated',
        mode: 'generate'
      })
    ])
  })

  it('stops the current preview loading immediately after async job submission and shows queued job in history', async () => {
    vi.mocked(createImageStudioJob).mockImplementationOnce(async (input: any) => {
      const id = ++jobCounter
      const job = {
        id,
        mode: input.mode,
        status: 'queued',
        attemptCount: 0,
        maxAttempts: 3,
        prompt: input.prompt,
        model: input.model,
        size: input.size,
        outputFormat: input.outputFormat || 'png',
        estimatedCostUsd: 1,
        chargedAmountUsd: 0,
        queuedAt: '2026-06-11T00:00:00Z'
      }
      jobs = [job, ...jobs]
      return job as any
    })

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('排队任务')
    await wrapper.get('[data-testid="generate-button"]').trigger('click')
    await flushPromises()

    expect(vi.mocked(getImageStudioJob)).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="generating-preview"]').exists()).toBe(false)

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('排队任务')
    expect(wrapper.text()).toContain('排队中')
  })

  it('refreshes queued job status when switching into history', async () => {
    jobs = [{
      id: 205,
      mode: 'generate',
      status: 'queued',
      attemptCount: 0,
      maxAttempts: 3,
      prompt: '会变成运行中的任务',
      model: 'gpt-image-2',
      size: '1024x1024',
      outputFormat: 'png',
      estimatedCostUsd: 1,
      chargedAmountUsd: 0,
      queuedAt: '2026-06-11T00:00:00Z'
    }]

    let listCallCount = 0
    vi.mocked(listImageStudioJobs).mockImplementation(async (_page = 1, pageSize = 20) => {
      listCallCount += 1
      const currentJobs = listCallCount >= 2
        ? [{
          ...jobs[0],
          status: 'running',
          attemptCount: 0
        }]
        : jobs
      return {
        items: currentJobs.slice(0, pageSize),
        total: currentJobs.length,
        page: 1,
        page_size: pageSize,
        pages: 1
      }
    })

    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).not.toContain('会变成运行中的任务')

    await wrapper.get('[data-testid="tab-history"]').trigger('click')
    await flushPromises()

    expect(listCallCount).toBeGreaterThanOrEqual(2)
    expect(wrapper.text()).toContain('会变成运行中的任务')
    expect(wrapper.text()).toContain('生成中')
  })

  it('opens template drawer and syncs a template prompt into the composer', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="template-button"]').trigger('click')
    expect(templateDrawer().exists()).toBe(true)

    await templateDrawer().get('[data-testid="template-card"]').trigger('click')
    const promptValue = (wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value
    expect(promptValue).toContain('蓝橙对比色')
    expect(promptValue).toContain('柔和自然光')
  })

  it('stores the original prompt when polishing succeeds', async () => {
    vi.mocked(sendPromptPolishRequest).mockResolvedValue({
      output: [
        {
          type: 'message',
          content: [{ type: 'output_text', text: '润色后的提示词' }]
        }
      ]
    } as any)

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="prompt-input"]').setValue('原始提示词')
    await wrapper.get('[data-testid="polish-prompt-button"]').trigger('click')
    await flushPromises()

    expect((wrapper.get('[data-testid="prompt-input"]').element as HTMLTextAreaElement).value).toBe('润色后的提示词')
    expect(JSON.parse(window.localStorage.getItem('sub2api:image-studio:prompt-history:v1') || '[]')).toMatchObject([
      expect.objectContaining({
        prompt: '原始提示词',
        source: 'polished',
        mode: 'generate'
      })
    ])
  })

  it('keeps selected ratio and resolution cards readable in dark mode', () => {
    const darkOverrides = readFileSync(
      resolve(__dirname, '../styles/dark-overrides.css'),
      'utf8'
    )

    expect(darkOverrides).toContain('.dark .ratio-card.active')
    expect(darkOverrides).toContain('.dark .resolution-card.active')
    expect(darkOverrides).toContain('color: #ecfeff')
    expect(darkOverrides).toContain('.dark .resolution-card.active .resolution-description')
    expect(darkOverrides).toContain('.dark .param-panel > .studio-tabs .studio-tab')
    expect(darkOverrides).toContain('.dark .param-panel .ratio-card:not(.resolution-card)')
    expect(darkOverrides).toContain('.dark .param-panel .ratio-card:not(.resolution-card):disabled')
    expect(darkOverrides).toContain('color: rgba(226, 232, 240, 0.88)')
    expect(darkOverrides).toContain('.dark .preview-header')
    expect(darkOverrides).toContain('.dark .api-key-strip')
    expect(darkOverrides).toContain('.dark .history-nav-guidance-copy')
    expect(darkOverrides).toContain('background: rgba(9, 19, 32, 0.94)')
    expect(darkOverrides).toContain('.dark .history-mode .detail-actions .result-edit')
    expect(darkOverrides).toContain('.dark .history-mode .detail-actions .result-download')
    expect(darkOverrides).toContain('.dark .history-mode .detail-actions .result-delete')
    expect(darkOverrides).toContain('background: linear-gradient(135deg, rgba(20, 184, 166, 0.22), rgba(8, 145, 178, 0.14))')
  })

  it('wraps prompt polish controls by the composer width', () => {
    const composerStyles = readFileSync(
      resolve(__dirname, '../styles/composer-base.css'),
      'utf8'
    )
    const darkOverrides = readFileSync(
      resolve(__dirname, '../styles/dark-overrides.css'),
      'utf8'
    )

    expect(composerStyles).toContain('container-type: inline-size')
    expect(composerStyles).toContain('@container (max-width: 780px)')
    expect(composerStyles).toContain('.prompt-head-actions {\n    flex-wrap: wrap')
    expect(composerStyles).toContain('@container (max-width: 600px)')
    expect(darkOverrides).toContain('@container (max-width: 600px)')
  })

  it('keeps the prompt polish model selector at a stable width', () => {
    const composerStyles = readFileSync(
      resolve(__dirname, '../styles/composer-base.css'),
      'utf8'
    )

    expect(composerStyles).toMatch(
      /\.prompt-polish-model-select\s*\{[^}]*\bwidth:\s*96px;/s
    )
  })
})

function mountView() {
  const wrapper = mount(ImageStudioView, { attachTo: document.body })
  mountedWrappers.push(wrapper)
  return wrapper
}

function templateDrawer() {
  return new DOMWrapper(document.body).find('[data-testid="template-drawer"]')
}

function waitForNextTask() {
  return new Promise((resolve) => setTimeout(resolve, 0))
}
