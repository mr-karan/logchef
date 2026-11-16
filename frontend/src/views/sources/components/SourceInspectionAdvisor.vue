<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { refAutoReset } from '@vueuse/core'
import { Check, Copy, Info, Play } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import ErrorAlert from '@/components/ui/ErrorAlert.vue'
import { EmptyState, LoadingState, PageSection } from '@/components/layout'
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
const failedInputs = computed(() => report.value?.inputs.filter(input => input.state !== 'ok') ?? [])

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

onMounted(load)
</script>

<template>
  <PageSection :title="t('advisor.title')" :description="advice?.generated_at ? t('advisor.generatedAt', { time: formatDate(advice.generated_at) }) : undefined">
    <template #actions>
      <Badge v-if="advice?.stale" variant="outline">{{ t('advisor.stale') }}</Badge>
      <Badge v-for="input in failedInputs" :key="input.name" variant="outline" class="font-mono" :title="input.detail">
        {{ input.name }}: {{ t(inputStateLabels[input.state]) }}
      </Badge>
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
        <EmptyState v-if="!report.findings.length" :title="t('advisor.noFindings')" class="py-6" />
        <article v-for="finding in report.findings" :key="finding.id" class="space-y-3 rounded-md border p-4" :data-finding="finding.id">
          <header class="flex flex-wrap items-center gap-2">
            <Badge :variant="impactVariants[finding.impact]">{{ t(impactLabels[finding.impact]) }}</Badge>
            <h3 class="text-sm font-medium">{{ finding.title }}</h3>
            <Badge v-if="finding.rebuild" variant="outline">{{ t('advisor.needsRebuild') }}</Badge>
            <Badge v-if="finding.candidate" variant="outline">{{ t('advisor.validateFirst') }}</Badge>
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
      </div>
    </template>
  </PageSection>
</template>
