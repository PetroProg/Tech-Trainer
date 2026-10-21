<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from 'vue'

export interface DialogProps {
  show: boolean
  title?: string
  message: string
  type?: 'info' | 'success' | 'warning' | 'error' | 'confirm'
  confirmText?: string
  cancelText?: string
  danger?: boolean
}

const props = withDefaults(defineProps<DialogProps>(), {
  show: false,
  title: '',
  message: '',
  type: 'info',
  confirmText: '',
  cancelText: 'Cancel',
  danger: false
})

const emit = defineEmits<{
  (e: 'confirm'): void
  (e: 'cancel'): void
}>()

const isConfirm = computed(() => props.type === 'confirm')

const displayTitle = computed(() => {
  if (props.title) return props.title
  switch (props.type) {
    case 'success': return 'Success'
    case 'warning': return 'Warning'
    case 'error': return 'Error'
    case 'confirm': return props.danger ? 'Confirm Action' : 'Confirmation'
    default: return 'Information'
  }
})

const ambientClass = computed(() => {
  if (props.danger || props.type === 'error') return 'bg-rose-500/25'
  if (props.type === 'warning') return 'bg-amber-500/25'
  if (props.type === 'success') return 'bg-emerald-500/25'
  return 'bg-sky-500/25'
})

const iconBadgeClass = computed(() => {
  if (props.danger || props.type === 'error') {
    return 'bg-rose-950/60 border-rose-500/40 text-rose-400 shadow-rose-950/50'
  }
  if (props.type === 'warning') {
    return 'bg-amber-950/60 border-amber-500/40 text-amber-400 shadow-amber-950/50'
  }
  if (props.type === 'success') {
    return 'bg-emerald-950/60 border-emerald-500/40 text-emerald-400 shadow-emerald-950/50'
  }
  return 'bg-sky-950/60 border-sky-500/40 text-sky-400 shadow-sky-950/50'
})

const confirmBtnClass = computed(() => {
  if (props.danger || props.type === 'error') {
    return 'bg-gradient-to-r from-rose-600 to-red-600 hover:from-rose-500 hover:to-red-500 text-white shadow-lg shadow-rose-600/25 border border-rose-500/30'
  }
  if (props.type === 'success') {
    return 'bg-gradient-to-r from-emerald-600 to-teal-600 hover:from-emerald-500 hover:to-teal-500 text-white shadow-lg shadow-emerald-600/25 border border-emerald-500/30'
  }
  if (props.type === 'warning') {
    return 'bg-gradient-to-r from-amber-600 to-orange-600 hover:from-amber-500 hover:to-orange-500 text-white shadow-lg shadow-amber-600/25 border border-amber-500/30'
  }
  return 'bg-gradient-to-r from-sky-500 to-indigo-600 hover:from-sky-400 hover:to-indigo-500 text-white shadow-lg shadow-sky-500/25 border border-sky-400/30'
})

function handleKeydown(e: KeyboardEvent) {
  if (!props.show) return
  if (e.key === 'Escape') {
    emit('cancel')
  } else if (e.key === 'Enter') {
    emit('confirm')
  }
}

watch(() => props.show, (newVal) => {
  if (newVal) {
    window.addEventListener('keydown', handleKeydown)
  } else {
    window.removeEventListener('keydown', handleKeydown)
  }
})

onMounted(() => {
  if (props.show) window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div 
        v-if="show" 
        class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-md"
        @click.self="emit('cancel')"
      >
        <Transition
          enter-active-class="transition duration-200 ease-out"
          enter-from-class="opacity-0 scale-95 translate-y-2"
          enter-to-class="opacity-100 scale-100 translate-y-0"
          leave-active-class="transition duration-150 ease-in"
          leave-from-class="opacity-100 scale-100 translate-y-0"
          leave-to-class="opacity-0 scale-95 translate-y-2"
        >
          <div 
            v-if="show"
            class="relative w-full max-w-sm sm:max-w-md bg-slate-900/95 border border-slate-700/80 rounded-2xl p-6 shadow-2xl shadow-black/90 overflow-hidden"
          >
            <!-- Ambient Top Glow -->
            <div 
              class="absolute -top-14 left-1/2 -translate-x-1/2 w-48 h-32 rounded-full blur-3xl pointer-events-none"
              :class="ambientClass"
            ></div>

            <!-- Close Button -->
            <button 
              @click="emit('cancel')"
              class="absolute top-4 right-4 p-1.5 text-slate-400 hover:text-white rounded-lg hover:bg-slate-800/80 transition"
              title="Close"
            >
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
              </svg>
            </button>

            <!-- Modal Content Row -->
            <div class="flex items-start gap-4">
              <!-- Icon Badge -->
              <div 
                class="w-11 h-11 rounded-2xl border flex items-center justify-center shrink-0 shadow-lg"
                :class="iconBadgeClass"
              >
                <!-- Success Icon -->
                <svg v-if="type === 'success'" class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M5 13l4 4L19 7" />
                </svg>

                <!-- Warning Icon -->
                <svg v-else-if="type === 'warning'" class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>

                <!-- Error Icon -->
                <svg v-else-if="type === 'error'" class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>

                <!-- Danger / Delete Confirm Icon -->
                <svg v-else-if="isConfirm && danger" class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>

                <!-- Info / General Confirm Icon -->
                <svg v-else class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
              </div>

              <!-- Text Content -->
              <div class="flex-1 pr-6 pt-0.5">
                <h3 class="text-base font-bold text-white tracking-tight leading-snug">
                  {{ displayTitle }}
                </h3>
                <p class="mt-2 text-xs sm:text-sm text-slate-300 leading-relaxed font-normal whitespace-pre-line">
                  {{ message }}
                </p>
              </div>
            </div>

            <!-- Action Buttons Footer -->
            <div class="mt-6 flex items-center justify-end gap-2.5">
              <button 
                v-if="isConfirm"
                type="button" 
                @click="emit('cancel')"
                class="px-4 py-2.5 rounded-xl text-xs font-semibold text-slate-300 bg-slate-800/90 hover:bg-slate-700 hover:text-white border border-slate-700/80 transition active:scale-[0.98]"
              >
                {{ cancelText || 'Cancel' }}
              </button>

              <button 
                type="button" 
                @click="emit('confirm')"
                class="px-5 py-2.5 rounded-xl text-xs font-semibold transition active:scale-[0.98]"
                :class="[confirmBtnClass, isConfirm ? '' : 'w-full sm:w-auto']"
              >
                {{ confirmText || (isConfirm ? (danger ? 'Delete' : 'Confirm') : 'Got it') }}
              </button>
            </div>
          </div>
        </Transition>
      </div>
    </Transition>
  </Teleport>
</template>