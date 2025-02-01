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
const columnWidths = ref<Record<string, string>>({})
const resizing = ref<{ key: string; startX: number; startWidth: number } | null>(null)
const resizeCleanup = ref<(() => void) | null>(null)
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
  // Try to get saved width first
  const savedWidth = columnWidths.value[column.key]
  if (savedWidth) {
    return {
      width: savedWidth,
      minWidth: savedWidth
    }
  }

  // Default widths based on column type
  switch (column.key.toLowerCase()) {
    case 'timestamp':
      return {
        width: '200px',
        minWidth: '200px'
      }
    case 'severity_text':
      return {
        width: '100px',
        minWidth: '100px'
      }
    case 'body':
    case 'message':
      return {
        width: '400px',
        minWidth: '300px'
      }
    default:
      return {
        width: '150px',
        minWidth: '150px'
      }
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
  const th = (event.target as HTMLElement).closest('th')
  if (!th) return

  const startX = event.pageX
  const startWidth = th.offsetWidth

  // Clean up any existing handlers
  resizeCleanup.value?.()

  const handleMouseMove = (e: MouseEvent) => {
    const width = startWidth + (e.pageX - startX)
    th.style.width = `${width}px`
  }

  const handleMouseUp = () => {
    document.removeEventListener('mousemove', handleMouseMove)
    document.removeEventListener('mouseup', handleMouseUp)
    
    // Save the final width
    columnWidths.value[columnKey] = th.style.width
    saveColumnWidths()
    
    resizeCleanup.value = null
  }

  document.addEventListener('mousemove', handleMouseMove)
  document.addEventListener('mouseup', handleMouseUp)

  resizeCleanup.value = () => {
    document.removeEventListener('mousemove', handleMouseMove)
    document.removeEventListener('mouseup', handleMouseUp)
  }

  event.preventDefault()
}

function handleResize(event: MouseEvent) {
  if (!resizing.value) return

  const { key, startX, startWidth } = resizing.value
  const diff = event.pageX - startX
  const newWidth = Math.max(60, startWidth + diff)

  requestAnimationFrame(() => {
    columnWidths.value[key] = newWidth
  })
  
  event.preventDefault()
}

function getDefaultColumnWidth(column: Column): number {
  if (column.width) return column.width
  
  const tableWidth = tableWrapper.value?.clientWidth || window.innerWidth
  const totalColumns = props.columns.length
  const avgColumnWidth = Math.floor(tableWidth / totalColumns)
  
  // Cap minimum width at 120px and maximum at 400px
  const baseWidth = Math.max(120, Math.min(400, avgColumnWidth))
  
  // Give slightly less width to timestamp columns
  if (column.key.toLowerCase().includes('time') || 
      column.key.toLowerCase().includes('date')) {
    return Math.min(200, baseWidth)
  }
  
  return baseWidth
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
    const newWidths: Record<string, number> = {}
    newColumns.forEach(col => {
      // Always use the latest width from props or default calculation
      newWidths[col.key] = col.width || getDefaultColumnWidth(col)
    })
    columnWidths.value = newWidths
    console.log('Column widths initialized:', newWidths)
  },
  { immediate: true, deep: true }
)

// Add debugging on mount
onMounted(() => {
  console.log('Initial column widths:', columnWidths.value)
})
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
  min-width: auto;
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
  padding-right: 12px;
}

.resize-handle {
  position: absolute;
  right: -6px;
  top: 0;
  width: 16px;
  height: 100%;
  cursor: col-resize;
  background: transparent;
  z-index: 2;
}

.resize-handle:hover::after {
  content: '';
  position: absolute;
  right: 6px;
  top: 0;
  bottom: 0;
  width: 2px;
  background: #409eff;
}

/* Column resizing styles */
.log-table :deep(th),
.log-table :deep(td) {
  position: relative;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.log-table :deep(th) {
  position: relative;
  background: white;
  user-select: none;
}

.log-table :deep(th)::after {
  content: '';
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  width: 1px;
  background: #e5e7eb;
}

.resize-handle {
  position: absolute;
  right: -4px;
  top: 0;
  bottom: 0;
  width: 8px;
  cursor: col-resize;
  z-index: 1;
}

.resize-handle:hover::after {
  content: '';
  position: absolute;
  right: 4px;
  top: 0;
  bottom: 0;
  width: 2px;
  background: #409eff;
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
