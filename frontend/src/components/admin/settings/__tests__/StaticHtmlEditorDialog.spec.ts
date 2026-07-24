import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import StaticHtmlEditorDialog from '@/components/admin/settings/StaticHtmlEditorDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: Boolean, title: String },
  emits: ['close'],
  template: `
    <section v-if="show">
      <button data-testid="dialog-close" @click="$emit('close')">close</button>
      <slot />
      <slot name="footer" />
    </section>
  `
})

const ConfirmDialogStub = defineComponent({
  name: 'ConfirmDialog',
  props: { show: Boolean },
  emits: ['confirm', 'cancel'],
  template: `
    <section v-if="show" data-testid="discard-confirmation">
      <button data-testid="confirm-discard" @click="$emit('confirm')">confirm</button>
      <button data-testid="cancel-discard" @click="$emit('cancel')">cancel</button>
    </section>
  `
})

const StaticHtmlFrameStub = defineComponent({
  name: 'StaticHtmlFrame',
  props: { source: String, title: String },
  template: '<div data-testid="static-html-frame" />'
})

function mountEditor(source: string) {
  return mount(StaticHtmlEditorDialog, {
    props: { show: true, title: 'About', source },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        StaticHtmlFrame: StaticHtmlFrameStub,
        Icon: true
      }
    }
  })
}

async function selectFile(wrapper: ReturnType<typeof mountEditor>, file: File) {
  const input = wrapper.get('input[type="file"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
  await input.trigger('change')
  await new Promise((resolve) => setTimeout(resolve, 0))
  await flushPromises()
}

describe('StaticHtmlEditorDialog', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('uploads UTF-8 HTML, previews it, and applies the source', async () => {
    const wrapper = mountEditor('<p>old</p>')
    await selectFile(
      wrapper,
      new File(['<h1>new</h1>'], 'about.HTML', { type: 'text/html' })
    )

    await wrapper.get('[data-testid="html-preview-tab"]').trigger('click')
    expect(wrapper.getComponent(StaticHtmlFrameStub).props('source')).toBe('<h1>new</h1>')

    await wrapper.get('[data-testid="html-editor-apply"]').trigger('click')
    expect(wrapper.emitted('apply')?.[0]).toEqual(['<h1>new</h1>'])
  })

  it('applies pasted visible source', async () => {
    const wrapper = mountEditor('')
    await wrapper.get('textarea').setValue('<style>h1{color:red}</style><h1>About</h1>')
    await wrapper.get('[data-testid="html-editor-apply"]').trigger('click')

    expect(wrapper.emitted('apply')?.[0]).toEqual([
      '<style>h1{color:red}</style><h1>About</h1>'
    ])
  })

  it('rejects a non-HTML file without replacing the draft', async () => {
    const wrapper = mountEditor('<p>keep</p>')
    await selectFile(wrapper, new File(['x'], 'about.txt'))

    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('<p>keep</p>')
    expect(wrapper.text()).toContain('admin.settings.customMenu.html.invalidFile')
  })

  it('rejects oversized uploads and pasted source', async () => {
    const wrapper = mountEditor('<p>keep</p>')
    const oversized = new Uint8Array((1 << 20) + 1)
    await selectFile(wrapper, new File([oversized], 'about.html', { type: 'text/html' }))

    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('<p>keep</p>')
    expect(wrapper.text()).toContain('admin.settings.customMenu.html.tooLarge')

    await wrapper.get('textarea').setValue('x'.repeat((1 << 20) + 1))
    await wrapper.get('[data-testid="html-editor-apply"]').trigger('click')
    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.text()).toContain('admin.settings.customMenu.html.tooLarge')
  })

  it('rejects invalid UTF-8 without replacing the draft', async () => {
    const wrapper = mountEditor('<p>keep</p>')
    await selectFile(wrapper, new File([new Uint8Array([0xff])], 'about.html'))

    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('<p>keep</p>')
    expect(wrapper.text()).toContain('admin.settings.customMenu.html.invalidEncoding')
  })

  it('rejects source with no visible content after sanitizing', async () => {
    const wrapper = mountEditor('<script>alert(1)</script>')
    await wrapper.get('[data-testid="html-editor-apply"]').trigger('click')

    expect(wrapper.emitted('apply')).toBeUndefined()
    expect(wrapper.text()).toContain('admin.settings.customMenu.html.emptyAfterSanitize')
  })

  it('confirms before closing a dirty draft', async () => {
    const wrapper = mountEditor('<p>old</p>')
    await wrapper.get('textarea').setValue('<p>changed</p>')
    await wrapper.get('[data-testid="dialog-close"]').trigger('click')

    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.find('[data-testid="discard-confirmation"]').exists()).toBe(true)

    await wrapper.get('[data-testid="confirm-discard"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('closes immediately when pristine and resets for a newly opened source', async () => {
    const wrapper = mountEditor('<p>old</p>')
    await wrapper.get('[data-testid="dialog-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)

    await wrapper.setProps({ show: false, source: '<p>new</p>' })
    await wrapper.setProps({ show: true })
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('<p>new</p>')
  })
})
