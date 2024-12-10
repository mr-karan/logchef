<script setup>
import { ref, watch } from 'vue'
import DatePicker from 'primevue/datepicker'
import FloatLabel from 'primevue/floatlabel'

const props = defineProps({
  startDate: {
    type: Date,
    default: null
  },
  endDate: {
    type: Date,
    default: null
  }
})

const emit = defineEmits(['update:startDate', 'update:endDate'])

const startDateTime = ref(props.startDate)
const endDateTime = ref(props.endDate)

watch(startDateTime, (newValue) => {
  emit('update:startDate', newValue)
})

watch(endDateTime, (newValue) => {
  emit('update:endDate', newValue)
})
</script>

<template>
  <div class="flex items-center gap-3">
    <div class="flex items-center gap-2">
      <FloatLabel>
        <DatePicker
          v-model="startDateTime"
          inputId="start_time"
          showTime
          showIcon
          iconDisplay="input"
          :maxDate="endDateTime"
          class="w-[180px] date-picker-custom"
        />
        <label for="start_time" class="text-[11px]">From</label>
      </FloatLabel>
      <span class="text-gray-400">-</span>
      <FloatLabel>
        <DatePicker
          v-model="endDateTime"
          inputId="end_time"
          showTime
          showIcon
          iconDisplay="input"
          :minDate="startDateTime"
          class="w-[180px] date-picker-custom"
        />
        <label for="end_time" class="text-[11px]">To</label>
      </FloatLabel>
    </div>
  </div>
</template>

<style scoped>
.date-picker-custom :deep(.p-inputtext){
  font-size: 0.75rem;
}
</style>


