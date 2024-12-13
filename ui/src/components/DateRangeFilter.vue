<script setup lang="ts">
import { ref, watch } from 'vue'
import Calendar from 'primevue/calendar'
import Button from 'primevue/button'

const props = defineProps({
  startDate: {
    type: Date,
    required: true
  },
  endDate: {
    type: Date,
    required: true
  }
})

// Add fetch to emits declaration
const emit = defineEmits(['update:startDate', 'update:endDate', 'fetch'])

// Create local refs that track the props
const localStartDate = ref(props.startDate)
const localEndDate = ref(props.endDate)

// Keep local refs in sync with props
watch(() => props.startDate, (newVal) => {
  localStartDate.value = newVal
})

watch(() => props.endDate, (newVal) => {
  localEndDate.value = newVal
})

// Emit date updates
watch(localStartDate, (newVal, oldVal) => {
  if (newVal?.getTime() !== oldVal?.getTime()) {
    emit('update:startDate', newVal)
  }
})

watch(localEndDate, (newVal, oldVal) => {
  if (newVal?.getTime() !== oldVal?.getTime()) {
    emit('update:endDate', newVal)
  }
})
</script>

<template>
  <div class="flex items-center gap-2">
    <Calendar
      v-model="localStartDate"
      :showTime="true"
      :showSeconds="true"
      dateFormat="yy-mm-dd"
      :manualInput="false"
      :maxDate="localEndDate"
      class="w-[200px]"
      :pt="{
        input: 'text-sm'
      }"
    />
    <span class="text-gray-400">to</span>
    <Calendar
      v-model="localEndDate"
      :showTime="true"
      :showSeconds="true"
      dateFormat="yy-mm-dd"
      :manualInput="false"
      :minDate="localStartDate"
      class="w-[200px]"
      :pt="{
        input: 'text-sm'
      }"
    />
    <Button
      icon="pi pi-play"
      severity="primary"
      aria-label="Fetch Logs"
      @click="emit('fetch')"
      :pt="{
        root: 'w-10 h-10',
        icon: 'text-sm'
      }"
    />
  </div>
</template>

<style scoped>
.date-picker-custom :deep(.p-inputtext){
  font-size: 0.75rem;
}
</style>


