<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'

const props = defineProps<{
  open: boolean
  title: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const dialogRef = ref<HTMLDialogElement | null>(null)

watch(
  () => props.open,
  (isOpen) => {
    const el = dialogRef.value
    if (!el) return
    if (isOpen && !el.open) {
      if (typeof el.showModal === 'function') {
        el.showModal()
      } else {
        el.setAttribute('open', '')
      }
    } else if (!isOpen && el.open) {
      if (typeof el.close === 'function') {
        el.close()
      } else {
        el.removeAttribute('open')
      }
    }
  }
)

onMounted(() => {
  const el = dialogRef.value
  if (!el) return
  if (typeof HTMLDialogElement !== 'undefined' && !('closedBy' in HTMLDialogElement.prototype)) {
    el.addEventListener('click', (event: MouseEvent) => {
      if (event.target !== el) return
      const rect = el.getBoundingClientRect()
      const isDialogContent =
        rect.top <= event.clientY &&
        event.clientY <= rect.top + rect.height &&
        rect.left <= event.clientX &&
        event.clientX <= rect.left + rect.width
      if (!isDialogContent) {
        emit('close')
      }
    })
  }
})
</script>

<template>
  <dialog
    ref="dialogRef"
    closedby="any"
    aria-labelledby="modal-title"
    class="m-auto w-full max-w-md rounded-xl bg-white p-6 text-slate-900 shadow-xl"
    @close="emit('close')"
  >
    <h2 id="modal-title" class="text-lg font-bold text-slate-900 mb-2">
      {{ title }}
    </h2>
    <div class="text-sm text-slate-600 mb-6">
      <slot />
    </div>
    <div class="flex justify-end gap-3">
      <slot name="actions" />
    </div>
  </dialog>
</template>
