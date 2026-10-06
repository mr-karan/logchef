<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useInvestigation } from './useInvestigation'
import { display, formatTime } from './data'

const {
  sources, selectedSource, source, query, start, end, ready, pending, error, notice,
  evidence, histogram, selectedRows, selectedBucket, context, comparison, executed,
  canAttach, displayMode, canExpand, initialize, runQuery, selectRow, selectBucket,
  runSelectedInterval, surrounding, compare, ask, reset, clearSelection,
  toggleDisplayMode, setRecentRange,
} = useInvestigation()
const compact = computed(() => displayMode.value !== 'fullscreen')
const previewRows = computed(() => evidence.value?.logs.slice(0, 5) ?? [])
const maxCount = computed(() => Math.max(1, ...histogram.value.buckets.map((bucket) => bucket.log_count)))
const visibleColumns = computed(() => {
  const columns = evidence.value?.columns ?? []
  const timestamp = columns.find((column) => column.name === source.value?.timestampField)
  return timestamp ? [timestamp, ...columns.filter((column) => column !== timestamp)].slice(0, 4) : columns.slice(0, 4)
})
const sampleLimited = computed(() => Boolean(evidence.value && (evidence.value.truncated || evidence.value.logs.length >= (evidence.value.limit || 100))))
const selectionLabel = computed(() => {
  const rows = selectedRows.value.length
  return `${rows} ${rows === 1 ? 'row' : 'rows'}${selectedBucket.value !== null ? ' + time interval' : ''} selected`
})
onMounted(initialize)
</script>

<template>
  <main :aria-busy="pending" :class="{ compact }">
    <header>
      <div class="brand"><div><h1>Investigate logs</h1><p class="subtitle">Read-only investigation</p></div></div>
      <button v-if="canExpand" :disabled="pending" @click="toggleDisplayMode">{{ displayMode === 'fullscreen' ? 'Back to chat' : 'Expand' }}</button>
    </header>

    <form class="query-form" @submit.prevent="runQuery()">
      <label>Log source
        <select v-model="selectedSource" :disabled="pending || !ready" @change="reset">
          <option value="" disabled>{{ ready ? 'Select a source' : 'Loading sources…' }}</option>
          <option v-for="item in sources" :key="item.id" :value="String(item.id)">{{ item.name }} · {{ item.sourceType || 'unknown backend' }}</option>
        </select>
      </label>
      <label>LogchefQL filter<input v-model="query" :disabled="pending" placeholder="Leave empty to inspect all logs" spellcheck="false"></label>
      <div class="range-heading"><span>Time range <span class="muted">· UTC</span></span><div class="presets" aria-label="Recent time ranges"><button v-for="minutes in [15, 60, 360]" :key="minutes" type="button" :disabled="pending" @click="setRecentRange(minutes)">{{ minutes < 60 ? `${minutes}m` : `${minutes / 60}h` }}</button></div></div>
      <details class="range-editor" :open="!compact"><summary>Edit exact UTC range</summary><div class="time-range"><label>Start<input v-model="start" :disabled="pending" aria-label="Start time UTC" placeholder="RFC3339 timestamp"></label><label>End<input v-model="end" :disabled="pending" aria-label="End time UTC" placeholder="RFC3339 timestamp"></label></div></details>
      <div class="toolbar"><span class="muted">{{ source?.teams.map((team) => team.name).join(', ') || 'Your accessible sources' }} · 100 row limit</span><button type="submit" class="primary" :disabled="pending || !ready || !source">{{ pending ? 'Working…' : 'Run query' }}</button></div>
    </form>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <p v-if="notice" role="status" class="notice">{{ notice }}</p>

    <section v-if="evidence && executed" aria-label="Query results">
      <div class="section-heading"><h2>Query results</h2><span class="badge">{{ sampleLimited ? 'Limited sample' : 'Returned rows' }}</span></div>
      <div class="metrics"><div><strong>{{ evidence.logs.length.toLocaleString() }}</strong><span>rows returned</span></div><div><strong>{{ evidence.duration.toFixed(2) }} <small>ms</small></strong><span>query duration</span></div><div><strong>{{ selectedRows.length }} <small>/ 20</small></strong><span>rows selected</span></div></div>
      <p class="muted">Filter used: <code>{{ executed.query || 'All logs' }}</code></p>
      <p class="muted range">{{ executed.start_time }} → {{ executed.end_time }}</p>
      <p v-if="sampleLimited" class="notice">This query reached its sample limit. Returned rows are not the total matching logs.</p>
      <p v-for="warning in evidence.warnings" :key="warning" class="notice">{{ warning }}</p>
      <div v-if="histogram.buckets.length" class="chart-card">
        <div class="section-heading"><h3>Log activity</h3><span class="muted">Select a bar to investigate its interval</span></div>
        <div class="histogram" role="group" aria-label="Log histogram">
          <button v-for="(bucket, index) in histogram.buckets" :key="bucket.bucket" :disabled="pending" :aria-pressed="selectedBucket === index" :aria-label="`${bucket.bucket}: ${bucket.log_count} logs`" :title="`${bucket.bucket}: ${bucket.log_count} logs`" @click="selectBucket(index)" :class="{ selected: selectedBucket === index }"><span :style="{ height: `${Math.max(2, bucket.log_count / maxCount * 100)}%` }"></span></button>
        </div>
        <div class="chart-axis"><span>{{ formatTime(executed.start_time) }} UTC</span><span>Peak {{ maxCount.toLocaleString() }} logs / bucket</span><span>{{ formatTime(executed.end_time) }} UTC</span></div>
      </div>
      <p v-if="histogram.notice" class="notice">{{ histogram.notice }}</p>
      <div v-if="!compact" class="toolbar actions"><button v-if="selectedBucket !== null" :disabled="pending" @click="runSelectedInterval">Query selected interval</button><button :disabled="pending" @click="compare">Compare preceding window</button></div>
      <div class="selection-bar"><span role="status">{{ selectionLabel }}</span><div><button :disabled="pending || !canAttach" class="primary" @click="ask">Ask ChatGPT</button><button :disabled="pending || (!selectedRows.length && selectedBucket === null)" @click="clearSelection">Clear</button></div></div>
      <p v-if="!canAttach" class="muted">This host cannot attach evidence. You can still inspect and compare logs.</p>
      <div v-if="!evidence.logs.length" class="empty"><h3>No matching logs</h3><p>Try a wider time range or remove the filter.</p></div>
      <div v-else-if="compact" class="log-preview">
        <label v-for="(row, index) in previewRows" :key="index" class="preview-row" :class="{ selected: selectedRows.includes(index) }">
          <input type="checkbox" :checked="selectedRows.includes(index)" :disabled="pending" :aria-label="`Select log row ${index + 1}`" @change="selectRow(index)">
          <span><span class="muted">{{ display(row[source?.timestampField || 'timestamp']) || `Row ${index + 1}` }}</span><span class="preview-message">{{ display(row.message || row._msg || Object.entries(row).filter(([key]) => key !== source?.timestampField).map(([key, value]) => `${key}=${display(value)}`).join(' · ')) }}</span></span>
        </label>
        <p v-if="evidence.logs.length > previewRows.length" class="muted">Showing {{ previewRows.length }} of {{ evidence.logs.length }} returned rows. {{ canExpand ? 'Expand to inspect all rows.' : 'Ask in the conversation for further inspection.' }}</p>
      </div>
      <div v-else class="table-wrap" tabindex="0" aria-label="Scrollable log results"><table><thead><tr><th scope="col">Select</th><th v-for="column in visibleColumns" :key="column.name" scope="col">{{ column.name }}</th><th scope="col">Details</th></tr></thead><tbody><tr v-for="(row, index) in evidence.logs" :key="index" :class="{ selected: selectedRows.includes(index) }"><td><input type="checkbox" :checked="selectedRows.includes(index)" :disabled="pending" :aria-label="`Select log row ${index + 1}`" @change="selectRow(index)"></td><td v-for="column in visibleColumns" :key="column.name"><span class="cell" :title="display(row[column.name])">{{ display(row[column.name]) }}</span></td><td><details><summary>Inspect row {{ index + 1 }}</summary><dl class="fields"><template v-for="(value, key) in row" :key="key"><dt>{{ key }}</dt><dd>{{ display(value) }}</dd></template></dl><button v-if="source?.capabilities.includes('log_context')" :disabled="pending" @click="surrounding(index)">Surrounding logs</button></details></td></tr></tbody></table></div>
      <details v-if="!compact" class="provenance"><summary>Query details</summary><dl class="fields"><dt>Source</dt><dd>{{ source?.name }} · {{ executed.source_id }}</dd><dt>Filter</dt><dd>{{ executed.query || 'All logs' }}</dd><dt>Query ID</dt><dd>{{ evidence.queryID || 'Unavailable' }}</dd></dl></details>
    </section>

    <section v-if="comparison" class="comparison-card" aria-label="Window comparison">
      <div class="section-heading"><h2>Window comparison</h2><span class="badge">Returned samples</span></div>
      <div class="metrics"><div><strong>{{ comparison.previous.rows }}</strong><span>preceding window {{ comparison.previous.truncated ? '· limited' : '' }}</span></div><div><strong>{{ comparison.current.rows }}</strong><span>current window {{ comparison.current.truncated ? '· limited' : '' }}</span></div><div><strong>{{ comparison.percent === null ? 'Unavailable' : `${comparison.percent > 0 ? '+' : ''}${comparison.percent.toFixed(1)}%` }}</strong><span>sample change</span></div></div>
      <p class="muted">{{ comparison.previous.start }} → {{ comparison.previous.end }}<br>{{ comparison.current.start }} → {{ comparison.current.end }}</p>
      <p class="muted">These samples do not measure error rates. Change is unavailable for incomplete samples or a zero baseline.</p>
    </section>
    <section v-if="context && !compact" aria-label="Surrounding logs"><h2>Surrounding logs</h2><p class="muted">{{ context.length }} nearby rows, without the investigation filter.</p><details v-for="(row, index) in context" :key="index" class="context-row"><summary>{{ index + 1 }} · {{ display(row[source?.timestampField || 'timestamp']) || 'Inspect log' }}</summary><dl class="fields"><template v-for="(value, key) in row" :key="key"><dt>{{ key }}</dt><dd>{{ display(value) }}</dd></template></dl></details></section>
    <div v-if="ready && !evidence && !pending && !error" class="empty"><h2>Start with a source and time range</h2><p>Inspect log activity, select relevant rows, then ask ChatGPT to investigate the evidence.</p><span class="muted">Queries run only when you choose Run query.</span></div>
  </main>
</template>
