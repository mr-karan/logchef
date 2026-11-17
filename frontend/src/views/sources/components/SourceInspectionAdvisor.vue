<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { refAutoReset } from '@vueuse/core'
import { Check, Copy, EyeOff, Info, Play } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import ErrorAlert from '@/components/ui/ErrorAlert.vue'
import { EmptyState, LoadingState, PageSection, RowActionsMenu } from '@/components/layout'
import type { AdvisorImpact, AdvisorInputState, SchemaFinding } from '@/api/sources'
import { isRetryableSourceError, useSourcesStore } from '@/stores/sources'
import { useToast } from '@/composables/useToast'
import { copyText } from '@/utils/clipboard'
import { formatDate } from '@/utils/format'

const props = defineProps<{ sourceId: number }>()
const { t, n } = useI18n()
const { toast } = useToast()
const sourcesStore = useSourcesStore()

const impactLabels: Record<AdvisorImpact, string> = {
  high: 'advisor.impactHigh',
  medium: 'advisor.impactMedium',
  low: 'advisor.impactLow',
  info: 'advisor.impactInfo',
}
const impactVariants: Record<AdvisorImpact, 'destructive' | 'default' | 'secondary' | 'outline'> = {
  high: 'destructive',
  medium: 'default',
  low: 'secondary',
  info: 'outline',
}
const inputStateLabels: Record<AdvisorInputState, string> = {
  ok: 'advisor.inputOk',
  denied: 'advisor.inputDenied',
  limited: 'advisor.inputLimited',
  missing: 'advisor.inputMissing',
  error: 'advisor.inputError',
  skipped: 'advisor.inputSkipped',
}

const advice = computed(() => sourcesStore.getSchemaAdviceById(props.sourceId))
const report = computed(() => advice.value?.report ?? null)
const loading = computed(() => sourcesStore.isLoadingOperation(`getSchemaAdvice-${props.sourceId}`))
const running = computed(() => sourcesStore.isLoadingOperation(`refreshSchemaAdvice-${props.sourceId}`))
const scheduleBusy = computed(() => sourcesStore.isLoadingOperation(`setSchemaAdviceSchedule-${props.sourceId}`))
const failedInputs = computed(() => report.value?.inputs.filter(input => input.state !== 'ok') ?? [])
const dismissals = computed(() => advice.value?.dismissals ?? [])
const activeFindings = computed(() => {
  const dismissed = new Set(dismissals.value.map(d => d.finding_id))
  return report.value?.findings.filter(finding => !dismissed.has(finding.id)) ?? []
})
// A dismissal outlives a refresh, so its finding can be gone from the report.
const dismissedRows = computed(() => dismissals.value.map(dismissal => ({
  dismissal,
  finding: report.value?.findings.find(finding => finding.id === dismissal.finding_id) ?? null,
})))
const showDismissed = ref(false)

const loadError = ref<{ message: string; retryable: boolean } | null>(null)
const runError = ref<{ message: string; retryable: boolean } | null>(null)

async function load() {
  loadError.value = null
  const result = await sourcesStore.getSchemaAdvice(props.sourceId)
  if (!result.success) {
    loadError.value = { message: result.error?.message ?? t('advisor.loadFailed'), retryable: isRetryableSourceError(result.error?.error_type) }
  }
}

async function runAnalysis() {
  runError.value = null
  const result = await sourcesStore.refreshSchemaAdvice(props.sourceId)
  if (!result.success) {
    runError.value = { message: result.error?.message ?? t('advisor.runFailed'), retryable: isRetryableSourceError(result.error?.error_type) }
  }
}

const copiedId = refAutoReset<string | null>(null, 1500)
async function copyDDL(finding: SchemaFinding) {
  try {
    await copyText(finding.ddl.join('\n'))
    copiedId.value = finding.id
  } catch {
    toast({ title: t('pages.copyFailed'), variant: 'destructive' })
  }
}

const maxReasonBytes = 500
const dismissing = ref<SchemaFinding | null>(null)
const reason = ref('')
const reasonTooLong = computed(() => new TextEncoder().encode(reason.value.trim()).length > maxReasonBytes)
const dismissBusy = computed(() => !!dismissing.value && sourcesStore.isLoadingOperation(`dismissSchemaFinding-${props.sourceId}-${dismissing.value.id}`))
const dismissOpen = computed({
  get: () => dismissing.value !== null,
  set: (open: boolean) => { if (!open) dismissing.value = null },
})

function openDismiss(finding: SchemaFinding) {
  reason.value = ''
  dismissing.value = finding
}

async function confirmDismiss() {
  const finding = dismissing.value
  if (!finding || reasonTooLong.value) return
  const result = await sourcesStore.dismissSchemaFinding(props.sourceId, finding.id, reason.value.trim())
  if (result.success) dismissing.value = null
}

function setSchedule(enabled: boolean) {
  return sourcesStore.setSchemaAdviceSchedule(props.sourceId, enabled)
}

function restore(findingId: string) {
  return sourcesStore.restoreSchemaFinding(props.sourceId, findingId)
}

onMounted(load)
</script>

<template>
  <PageSection :title="t('advisor.title')" :description="advice?.generated_at ? t('advisor.generatedAt', { time: formatDate(advice.generated_at) }) : undefined">
    <template #actions>
      <Badge v-if="advice?.stale" variant="outline">{{ t('advisor.stale') }}</Badge>
      <Badge v-for="input in failedInputs" :key="input.name" variant="outline" class="font-mono" :title="input.detail">
        {{ input.name }}: {{ t(inputStateLabels[input.state]) }}
      </Badge>
      <div v-if="advice" class="flex items-center gap-2">
        <Switch id="advisor-refresh-weekly" :model-value="advice.scheduled_refresh" :disabled="scheduleBusy" @update:model-value="setSchedule" />
        <Label for="advisor-refresh-weekly" class="text-sm font-normal">{{ t('advisor.refreshWeekly') }}</Label>
      </div>
      <Button size="sm" :disabled="running || loading || !!loadError" @click="runAnalysis">
        <Play class="size-3.5" :class="{ 'animate-pulse': running }" />
        {{ running ? t('advisor.running') : t('advisor.runAnalysis') }}
      </Button>
    </template>

    <ErrorAlert v-if="loadError" :error="loadError.message" :title="t('advisor.loadFailed')" :retry-label="t('ui.retry')" :retryable="loadError.retryable" @retry="load" />
    <LoadingState v-else-if="loading && !advice" :label="t('ui.loading')" />
    <template v-else-if="advice">
      <ErrorAlert v-if="runError" class="mb-4" :error="runError.message" :title="t('advisor.runFailed')" :retry-label="t('ui.retry')" :retryable="runError.retryable && !running" @retry="runAnalysis" />
      <LoadingState v-if="running && !report" :label="t('advisor.running')" />
      <EmptyState v-else-if="!report" :title="t('advisor.noReport')" />
      <EmptyState v-else-if="report.status === 'unavailable'" :title="t('advisor.unavailable')" :description="report.reason" />
      <div v-else class="space-y-4">
        <p class="text-sm text-muted-foreground">
          {{ t('advisor.queriesAnalyzed', { count: n(report.queries_analyzed), days: advice.window_days ?? 0 }, report.queries_analyzed) }}
        </p>
        <ul v-if="report.notes?.length" class="space-y-1 text-sm text-muted-foreground">
          <li v-for="note in report.notes" :key="note" class="flex items-start gap-2">
            <Info class="mt-0.5 size-4 shrink-0" />{{ note }}
          </li>
        </ul>
        <div v-if="dismissals.length" class="flex items-center gap-2">
          <Switch id="advisor-show-dismissed" v-model="showDismissed" />
          <Label for="advisor-show-dismissed" class="text-sm font-normal">{{ t('advisor.showDismissed', { count: dismissals.length }) }}</Label>
        </div>
        <EmptyState v-if="!activeFindings.length" :title="t('advisor.noFindings')" class="py-6" />
        <article v-for="finding in activeFindings" :key="finding.id" class="space-y-3 rounded-md border p-4" :data-finding="finding.id">
          <header class="flex flex-wrap items-center gap-2">
            <Badge :variant="impactVariants[finding.impact]">{{ t(impactLabels[finding.impact]) }}</Badge>
            <h3 class="text-sm font-medium">{{ finding.title }}</h3>
            <Badge v-if="finding.rebuild" variant="outline">{{ t('advisor.needsRebuild') }}</Badge>
            <Badge v-if="finding.candidate" variant="outline">{{ t('advisor.validateFirst') }}</Badge>
            <div class="ml-auto">
              <RowActionsMenu>
                <DropdownMenuItem @click="openDismiss(finding)">
                  <EyeOff class="mr-2 h-4 w-4" />
                  {{ t('advisor.dismiss') }}
                </DropdownMenuItem>
              </RowActionsMenu>
            </div>
          </header>
          <dl v-if="finding.evidence.length" class="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[minmax(0,14rem)_1fr]">
            <template v-for="(item, index) in finding.evidence" :key="index">
              <dt class="text-muted-foreground">{{ item.label }}</dt>
              <dd class="tabular-nums">{{ item.value }} <span class="font-mono text-xs text-muted-foreground">{{ item.source }}</span></dd>
            </template>
          </dl>
          <div v-if="finding.ddl.length" class="space-y-2">
            <div class="flex items-center justify-between gap-2">
              <span class="text-xs font-medium text-muted-foreground">{{ t('advisor.ddl') }}</span>
              <Button variant="outline" size="sm" :aria-label="t('advisor.copyDdl')" @click="copyDDL(finding)">
                <Check v-if="copiedId === finding.id" class="size-3.5" /><Copy v-else class="size-3.5" />
                {{ copiedId === finding.id ? t('pages.copied') : t('pages.copy') }}
              </Button>
            </div>
            <pre class="overflow-x-auto rounded-md bg-muted/50 p-3 font-mono text-xs"><code>{{ finding.ddl.join('\n') }}</code></pre>
          </div>
          <dl class="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[minmax(0,14rem)_1fr]">
            <dt class="text-muted-foreground">{{ t('advisor.gain') }}</dt>
            <dd>{{ finding.gain }}</dd>
            <dt class="text-muted-foreground">{{ t('advisor.cost') }}</dt>
            <dd>{{ finding.cost }}</dd>
            <template v-if="finding.verify">
              <dt class="text-muted-foreground">{{ t('advisor.verify') }}</dt>
              <dd><code class="break-all font-mono text-xs">{{ finding.verify }}</code></dd>
            </template>
          </dl>
        </article>
        <section v-if="showDismissed && dismissedRows.length" class="space-y-2">
          <h3 class="text-sm font-medium">{{ t('advisor.dismissed') }}</h3>
          <div v-for="row in dismissedRows" :key="row.dismissal.finding_id" class="flex items-start justify-between gap-3 rounded-md border border-dashed p-3" :data-dismissed="row.dismissal.finding_id">
            <div class="min-w-0 space-y-1">
              <p class="text-sm font-medium">{{ row.finding?.title ?? t('advisor.findingGone', { id: row.dismissal.finding_id }) }}</p>
              <p class="text-xs text-muted-foreground">{{ t('advisor.dismissedAt', { time: formatDate(row.dismissal.dismissed_at) }) }}</p>
              <p v-if="row.dismissal.reason" class="whitespace-pre-wrap break-words text-sm">{{ row.dismissal.reason }}</p>
            </div>
            <Button variant="outline" size="sm" :disabled="sourcesStore.isLoadingOperation(`restoreSchemaFinding-${sourceId}-${row.dismissal.finding_id}`)" @click="restore(row.dismissal.finding_id)">
              {{ t('advisor.restore') }}
            </Button>
          </div>
        </section>
      </div>
    </template>
  </PageSection>

  <Dialog v-model:open="dismissOpen">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t('advisor.dismissTitle') }}</DialogTitle>
      </DialogHeader>
      <form class="grid gap-4" @submit.prevent="confirmDismiss">
        <p class="text-sm">{{ dismissing?.title }}</p>
        <div class="grid gap-1.5">
          <Label for="advisor-dismiss-reason">{{ t('advisor.dismissReason') }}</Label>
          <Textarea id="advisor-dismiss-reason" v-model="reason" rows="3" :aria-invalid="reasonTooLong" />
          <p v-if="reasonTooLong" class="text-xs text-destructive">{{ t('advisor.reasonTooLong', { max: maxReasonBytes }) }}</p>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" @click="dismissing = null">{{ t('ui.cancel') }}</Button>
          <Button type="submit" :disabled="reasonTooLong || dismissBusy">{{ t('advisor.dismiss') }}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
