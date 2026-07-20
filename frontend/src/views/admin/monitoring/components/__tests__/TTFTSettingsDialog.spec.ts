import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TTFTSettingsDialog from '../TTFTSettingsDialog.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const settings = {
  saved: { enabled: true, timeout_seconds: 20 },
  effective: { enabled: true, timeout_seconds: 20 },
  loaded_at: '2026-07-15T00:00:00Z'
}

// BaseDialog renders through <Teleport to="body">; stub teleport so the dialog
// content is reachable via wrapper.get. Kept local because other suites rely on
// real teleports landing in document.body.
const mountDialog = () =>
  mount(TTFTSettingsDialog, {
    props: { open: true, settings, saving: false, error: '' },
    global: { stubs: { teleport: true } }
  })

describe('TTFTSettingsDialog', () => {
  it('rejects out-of-range timeout values', async () => {
    const wrapper = mountDialog()
    await wrapper.get('input[type="number"]').setValue(0)
    await wrapper.get('form').trigger('submit.prevent')
    expect(wrapper.emitted('save')).toBeUndefined()
  })

  it('emits save with current values', async () => {
    const wrapper = mountDialog()
    await wrapper.get('input[type="number"]').setValue(45)
    await wrapper.get('form').trigger('submit.prevent')
    expect(wrapper.emitted('save')?.[0]?.[0]).toEqual({ enabled: true, timeout_seconds: 45 })
  })
})
