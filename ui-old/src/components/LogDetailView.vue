<template>
  <div class="bg-gray-50 border-t border-gray-200">
    <div class="relative text-xs font-mono">
      <div
        @click="copyToClipboard"
        class="absolute right-2 top-2 flex items-center gap-1 text-[10px] text-gray-500 hover:text-gray-700 cursor-pointer select-none"
      >
        <i class="pi pi-copy text-[10px]" />
        {{ copied ? 'Copied!' : 'Copy' }}
      </div>
      <pre class="p-2 overflow-x-auto"><code>{{ formattedJson }}</code></pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Log } from '@/types/logs'

const props = defineProps<{
  log: Log
}>()

const copied = ref(false)
const formattedJson = computed(() => JSON.stringify(props.log, null, 2))

const copyToClipboard = () => {
  navigator.clipboard.writeText(formattedJson.value)
  copied.value = true
  setTimeout(() => {
    copied.value = false
  }, 2000)
}
</script>
