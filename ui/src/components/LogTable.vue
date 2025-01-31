<template>
  <div class="log-table-container" :class="props.class">
    <div class="table-controls">
      <n-popover>
        <template #trigger>
          <n-button size="small" secondary>
            Columns ({{ localVisibleColumns.length }})
          </n-button>
        </template>
        <div class="column-selector">
          <n-checkbox-group v-model:value="localVisibleColumns" @update:value="updateVisibleColumns">
            <n-space vertical>
              <n-checkbox v-for="col in props.columns" :key="col.key" :value="col.key" :label="col.title"
                :disabled="col.required">
                <n-tooltip v-if="col.type" trigger="hover">
                  <template #trigger>
                    <span>{{ col.title }}</span>
                  </template>
                  Type: {{ col.type }}
                </n-tooltip>
                <span v-else>{{ col.title }}</span>
              </n-checkbox>
            </n-space>
          </n-checkbox-group>
        </div>
      </n-popover>
    </div>

    <!-- Virtual Table -->
    <div class="table-wrapper" ref="tableWrapper">
      <table ref="table" class="log-table">
        <thead>
          <tr>
            <th v-for="col in visibleColumns" :key="col.key" :style="getColumnStyle(col)" class="table-header">
              <div class="header-content">
                {{ col.title }}
                <div class="resize-handle" @mousedown="startResize($event, col.key)"></div>
              </div>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, index) in props.data" :key="index" :class="getRowClass(row)" @click="emit('row-click', row)">
            <td v-for="col in visibleColumns" :key="col.key" :style="getColumnStyle(col)">
              <div class="cell-content" :title="formatCellValue(row[col.key], col)">
                {{ formatCellValue(row[col.key], col) }}
              </div>
            </td>
          </tr>
        </tbody>
      </table>

      <!-- Loading State -->
      <div v-if="props.loading" class="loading-overlay">
        <n-spin size="medium" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import type { Log } from '@/api/types'
import { format } from 'date-fns'
import { NButton, NPopover, NCheckboxGroup, NCheckbox, NSpace, NSpin, NTooltip } from 'naive-ui'

interface Column {
  key: string
  title: string
  width?: number
  required?: boolean
  type?: string
}

interface Props {
  data: Log[]
  loading: boolean
  columns: Column[]
  visibleColumnKeys: string[]
  severityColors: Record<string, string>
  class?: string
}

interface Emits {
  (e: 'update:visible-column-keys', keys: string[]): void
  (e: 'row-click', log: Log): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

// Refs
const tableWrapper = ref<HTMLElement | null>(null)
const table = ref<HTMLElement | null>(null)
const columnWidths = ref<Record<string, number>>({})
const resizing = ref<{ key: string; startX: number; startWidth: number } | null>(null)
const localVisibleColumns = ref<string[]>([])

// Watch for prop changes
watch(() => props.visibleColumnKeys, (newKeys) => {
  localVisibleColumns.value = [...newKeys]
}, { immediate: true })

// Computed
const visibleColumns = computed(() =>
  props.columns.filter(col => localVisibleColumns.value.includes(col.key))
)

// Methods
function getColumnStyle(column: Column) {
  const width = columnWidths.value[column.key] || column.width || 150
  return {
    width: `${width}px`,
    minWidth: `${getMinColumnWidth(column.type)}px`
  }
}

function getRowClass(row: Log) {
  return {
    'severity-error': row.severity_text?.toLowerCase() === 'error',
    'severity-warn': row.severity_text?.toLowerCase() === 'warn',
    'severity-info': row.severity_text?.toLowerCase() === 'info',
    'severity-debug': row.severity_text?.toLowerCase() === 'debug'
  }
}

function formatCellValue(value: any, column: Column) {
  if (!value) return '-'

  if (column.key === 'timestamp') {
    return format(new Date(value), 'yyyy-MM-dd HH:mm:ss.SSS')
  }

  if (typeof value === 'object') {
    return JSON.stringify(value)
  }

  return value
}

function startResize(event: MouseEvent, columnKey: string) {
  const columnWidth = columnWidths.value[columnKey] || 150

  resizing.value = {
    key: columnKey,
    startX: event.pageX,
    startWidth: columnWidth
  }

  document.addEventListener('mousemove', handleResize)
  document.addEventListener('mouseup', stopResize)
}

function handleResize(event: MouseEvent) {
  if (!resizing.value) return

  const { key, startX, startWidth } = resizing.value
  const diff = event.pageX - startX

  const column = props.columns.find(col => col.key === key)
  const minWidth = getMinColumnWidth(column?.type)

  const newWidth = Math.max(minWidth, startWidth + diff)
  columnWidths.value[key] = newWidth
}

function getMinColumnWidth(type?: string): number {
  if (!type) return 60

  const columnType = type.toLowerCase()
  if (columnType.includes('timestamp')) return 120
  if (columnType.includes('bool')) return 60
  if (columnType.includes('int') || columnType.includes('float')) return 60
  if (columnType.includes('text') || columnType.includes('varchar')) return 80

  return 60
}

function stopResize() {
  resizing.value = null
  document.removeEventListener('mousemove', handleResize)
  document.removeEventListener('mouseup', stopResize)
}

function updateVisibleColumns(keys: string[]) {
  localVisibleColumns.value = [...keys]
  emit('update:visible-column-keys', [...keys])
}

// Watch for column changes
watch(
  () => props.columns,
  (newColumns) => {
    // Initialize column widths for new columns
    newColumns.forEach(col => {
      if (!columnWidths.value[col.key]) {
        columnWidths.value[col.key] = col.width || 150
      }
    })
  },
  { immediate: true }
)
</script>

<style scoped>
.log-table-container {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.table-controls {
  padding: 8px 0;
}

.column-selector {
  padding: 12px;
  min-width: 240px;
  max-height: 400px;
  overflow-y: auto;
}

.table-wrapper {
  flex: 1;
  width: 100%;
  overflow: auto;
  position: relative;
  min-height: 0;
}

.log-table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
}

.table-header {
  position: sticky;
  top: 0;
  background: #f8f8f8;
  z-index: 1;
  padding: 8px;
  text-align: left;
  font-weight: 500;
  border-bottom: 1px solid #eee;
}

.header-content {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.resize-handle {
  position: absolute;
  right: -4px;
  top: 0;
  width: 8px;
  height: 100%;
  cursor: col-resize;
  background: transparent;
  transition: background 0.2s;
}

.resize-handle:hover {
  background: rgba(64, 158, 255, 0.1);
}

.cell-content {
  padding: 8px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.loading-overlay {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(255, 255, 255, 0.8);
  display: flex;
  align-items: center;
  justify-content: center;
}

tr {
  border-bottom: 1px solid #eee;
}

tr:hover {
  background-color: rgba(0, 0, 0, 0.02);
}

td {
  border-right: 1px solid #eee;
}

.severity-error {
  background-color: rgba(255, 77, 79, 0.1);
}

.severity-warn {
  background-color: rgba(250, 173, 20, 0.1);
}

.severity-info {
  background-color: rgba(24, 144, 255, 0.1);
}

.severity-debug {
  background-color: rgba(144, 147, 153, 0.1);
}
</style>
