<template>
  <div ref="rootRef" class="studio-custom-select" :class="{ open, disabled, 'open-up': placement === 'top' }">
    <button
      type="button"
      class="studio-custom-select-button"
      :class="buttonClass"
      :disabled="disabled"
      :aria-expanded="open"
      :aria-label="ariaLabel"
      @click="toggleOpen"
      @keydown.down.prevent="openMenu"
      @keydown.enter.prevent="toggleOpen"
      @keydown.space.prevent="toggleOpen"
    >
      <span class="studio-custom-select-label" :class="{ placeholder: !selectedOption }">
        {{ selectedOption?.label ?? placeholder }}
      </span>
      <span class="studio-custom-select-caret" aria-hidden="true"></span>
    </button>

    <select
      class="studio-native-select"
      :value="modelValue"
      :disabled="disabled"
      tabindex="-1"
      aria-hidden="true"
      :data-testid="dataTestid"
      @change="selectValue(($event.target as HTMLSelectElement).value)"
    >
      <option v-if="placeholder" value="" :disabled="placeholderDisabled">{{ placeholder }}</option>
      <option v-for="option in options" :key="option.value" :value="option.value" :disabled="option.disabled">
        {{ option.label }}
      </option>
    </select>

    <Transition name="studio-select-pop">
      <div v-if="open" class="studio-custom-select-menu" role="listbox">
        <button
          v-if="placeholder && !placeholderDisabled"
          type="button"
          class="studio-custom-select-option placeholder"
          :class="{ active: modelValue === '' }"
          role="option"
          :aria-selected="modelValue === ''"
          @click="selectValue('')"
        >
          {{ placeholder }}
        </button>
        <button
          v-for="option in options"
          :key="option.value"
          type="button"
          class="studio-custom-select-option"
          :class="{ active: modelValue === option.value, disabled: option.disabled }"
          :data-disabled-reason="option.disabledReason"
          role="option"
          :aria-selected="modelValue === option.value"
          :aria-disabled="option.disabled ? 'true' : undefined"
          :tabindex="option.disabled ? -1 : 0"
          :title="option.disabledReason"
          @click="selectValue(option.value)"
        >
          {{ option.label }}
        </button>
        <div v-if="options.length === 0 && emptyText" class="studio-custom-select-empty">
          {{ emptyText }}
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'

interface StudioSelectOption {
  value: string
  label: string
  disabled?: boolean
  disabledReason?: string
}

const props = withDefaults(defineProps<{
  modelValue: string
  options: StudioSelectOption[]
  placeholder?: string
  placeholderDisabled?: boolean
  disabled?: boolean
  emptyText?: string
  placement?: 'bottom' | 'top'
  ariaLabel?: string
  dataTestid?: string
  buttonClass?: string
}>(), {
  placeholder: '',
  placeholderDisabled: false,
  disabled: false,
  emptyText: '',
  placement: 'bottom',
  ariaLabel: '',
  dataTestid: '',
  buttonClass: ''
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [value: string]
}>()

const rootRef = ref<HTMLElement | null>(null)
const open = ref(false)

const selectedOption = computed(() => props.options.find((option) => option.value === props.modelValue) ?? null)

function openMenu() {
  if (props.disabled) return
  open.value = true
  bindOutsideListener()
}

function toggleOpen() {
  if (open.value) {
    closeMenu()
    return
  }
  openMenu()
}

function closeMenu() {
  open.value = false
  unbindOutsideListener()
}

function selectValue(value: string) {
  const option = props.options.find((item) => item.value === value)
  if (option?.disabled) return
  emit('update:modelValue', value)
  emit('change', value)
  closeMenu()
}

function handlePointerDown(event: PointerEvent) {
  if (rootRef.value?.contains(event.target as Node)) return
  closeMenu()
}

function bindOutsideListener() {
  window.addEventListener('pointerdown', handlePointerDown)
}

function unbindOutsideListener() {
  window.removeEventListener('pointerdown', handlePointerDown)
}

onBeforeUnmount(unbindOutsideListener)
</script>
