<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { refAutoReset } from '@vueuse/core'
import { ChevronDown, Copy, Check } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { PageSection } from '@/components/layout'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { Source, SourceSchemaInspection } from '@/api/sources'
import { classifySourceField } from '@/lib/sourceFields'
import { useToast } from '@/composables/useToast'
import { copyText } from '@/utils/clipboard'

const props = defineProps<{
  source?: Pick<Source, 'source_type' | '_meta_ts_field' | '_meta_severity_field'> | null
  schema?: SourceSchemaInspection | null
}>()
const { t } = useI18n()
const { toast } = useToast()
const schemaFields = computed(() => props.schema?.fields ?? [])
const hasDefaults = computed(() => schemaFields.value.some(field => field.default_expression))
const hasComments = computed(() => schemaFields.value.some(field => field.comment))
const copied = refAutoReset(false, 1500)
async function copyCreateQuery() {
  if (!props.schema?.create_query) return
  try {
    await copyText(props.schema.create_query)
    copied.value = true
  } catch {
    toast({ title: t('pages.copyFailed'), variant: 'destructive' })
  }
}
</script>

<template>
  <div v-if="schema" class="space-y-6">
    <PageSection v-if="schemaFields.length" :title="t('sources.schema')" flush>
      <template #actions>
        <span class="text-sm text-muted-foreground">{{ t('inspection.fieldCount', { count: schemaFields.length }, schemaFields.length) }}</span>
      </template>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t('sources.field') }}</TableHead>
            <TableHead>{{ t('sources.type') }}</TableHead>
            <TableHead>{{ t('ui.role') }}</TableHead>
            <TableHead v-if="hasDefaults">{{ t('sources.default') }}</TableHead>
            <TableHead v-if="hasComments">{{ t('sources.comment') }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="field in schemaFields" :key="field.name">
            <TableCell>
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-mono text-xs font-medium">{{ field.name }}</span>
                <Badge v-if="field.is_primary_key" variant="secondary" class="text-[10px]">{{ t('sources.primaryKey') }}</Badge>
                <Badge v-if="field.is_nullable" variant="outline" class="text-[10px]">{{ t('sources.nullable') }}</Badge>
              </div>
            </TableCell>
            <TableCell class="font-mono text-xs">{{ field.type }}</TableCell>
            <TableCell class="text-xs text-muted-foreground">{{ t(`fields.${classifySourceField(source, field)}.label`) }}</TableCell>
            <TableCell v-if="hasDefaults" class="font-mono text-xs">{{ field.default_expression }}</TableCell>
            <TableCell v-if="hasComments" class="text-xs">{{ field.comment }}</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </PageSection>
    <details v-if="schema.create_query" class="group rounded-md border">
      <summary class="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-sm font-medium [&::-webkit-details-marker]:hidden">
        {{ t('sources.createQuery') }}
        <ChevronDown class="size-4 text-muted-foreground transition-transform group-open:rotate-180" />
      </summary>
      <div class="space-y-2 border-t bg-muted/30 p-4">
        <div class="flex justify-end">
          <Button variant="outline" size="sm" :aria-label="t('inspection.copyQuery')" @click="copyCreateQuery">
            <Check v-if="copied" class="size-3.5" /><Copy v-else class="size-3.5" />
            {{ copied ? t('pages.copied') : t('pages.copy') }}
          </Button>
        </div>
        <pre class="overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs leading-relaxed">{{ schema.create_query }}</pre>
      </div>
    </details>
  </div>
</template>
