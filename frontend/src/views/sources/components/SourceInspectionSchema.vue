<script setup lang="ts">
import { computed } from 'vue'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { Source, SourceSchemaInspection } from '@/api/sources'
import { buildSourceFieldGroups, classifySourceField } from '@/lib/sourceFields'

const props = defineProps<{
  source?: Pick<Source, 'source_type' | '_meta_ts_field' | '_meta_severity_field'> | null
  schema?: SourceSchemaInspection | null
}>()

const schemaFields = computed(() => props.schema?.fields ?? [])

const groupedFields = computed(() => buildSourceFieldGroups(schemaFields.value, props.source))

const groupLabelById = computed(() => {
  return new Map(groupedFields.value.map(group => [group.id, group.label]))
})

const sortKeys = computed(() => props.schema?.sort_keys ?? [])

const hasFieldStorageMetrics = computed(() =>
  schemaFields.value.some(field =>
    !!field.compressed ||
    !!field.uncompressed ||
    field.compression_ratio !== undefined ||
    field.avg_row_size !== undefined ||
    field.row_count !== undefined,
  ),
)
</script>

<template>
  <Card v-if="props.schema && schemaFields.length > 0">
    <CardHeader>
      <CardTitle>Schema</CardTitle>
      <CardDescription>
        Field inventory and datasource-specific structure for this source.
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-6">
      <div class="flex flex-wrap gap-2">
        <Badge
          v-for="group in groupedFields"
          :key="group.id"
          variant="secondary"
          class="gap-1 px-2 py-1"
        >
          <span>{{ group.label }}</span>
          <span class="text-xs text-muted-foreground">{{ group.fields.length }}</span>
        </Badge>
      </div>

        <div v-if="sortKeys.length > 0 || props.schema.ttl" class="grid gap-3 lg:grid-cols-2">
        <div v-if="sortKeys.length > 0" class="rounded-md border bg-muted/30 p-3">
          <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">Sort Keys</div>
          <div class="mt-1 font-mono text-sm break-all">
            {{ sortKeys.join(', ') }}
          </div>
        </div>
        <div v-if="props.schema.ttl" class="rounded-md border bg-muted/30 p-3">
          <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">TTL</div>
          <div class="mt-1 font-mono text-sm break-all">
            {{ props.schema.ttl }}
          </div>
        </div>
      </div>

      <div v-if="props.schema.create_query" class="rounded-md border bg-muted/30 p-3">
        <div class="text-xs font-medium uppercase tracking-wider text-muted-foreground">Create Query</div>
        <pre class="mt-2 overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs">{{ props.schema.create_query }}</pre>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Field</TableHead>
            <TableHead>Type</TableHead>
            <TableHead>Role</TableHead>
            <TableHead>Nullable</TableHead>
            <TableHead>Primary Key</TableHead>
            <TableHead>Default</TableHead>
            <TableHead>Comment</TableHead>
            <template v-if="hasFieldStorageMetrics">
              <TableHead>Rows</TableHead>
              <TableHead>Compressed</TableHead>
              <TableHead>Uncompressed</TableHead>
              <TableHead>Ratio</TableHead>
              <TableHead>Avg Row Size</TableHead>
            </template>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="field in schemaFields" :key="field.name">
            <TableCell class="font-medium">{{ field.name }}</TableCell>
            <TableCell class="font-mono text-xs">{{ field.type }}</TableCell>
            <TableCell>
              <Badge variant="outline">
                {{ groupLabelById.get(classifySourceField(props.source, field)) }}
              </Badge>
            </TableCell>
            <TableCell>{{ field.is_nullable === undefined ? '–' : field.is_nullable ? 'Yes' : 'No' }}</TableCell>
            <TableCell>{{ field.is_primary_key === undefined ? '–' : field.is_primary_key ? 'Yes' : 'No' }}</TableCell>
            <TableCell class="text-xs">{{ field.default_expression || '–' }}</TableCell>
            <TableCell class="text-xs">{{ field.comment || '–' }}</TableCell>
            <template v-if="hasFieldStorageMetrics">
              <TableCell class="text-xs">{{ field.row_count === undefined ? '–' : field.row_count.toLocaleString() }}</TableCell>
              <TableCell class="text-xs">{{ field.compressed || '–' }}</TableCell>
              <TableCell class="text-xs">{{ field.uncompressed || '–' }}</TableCell>
              <TableCell class="text-xs">
                {{ field.compression_ratio === undefined ? '–' : `${field.compression_ratio.toFixed(2)}x` }}
              </TableCell>
              <TableCell class="text-xs">
                {{ field.avg_row_size === undefined ? '–' : `${field.avg_row_size.toFixed(2)} bytes` }}
              </TableCell>
            </template>
          </TableRow>
        </TableBody>
      </Table>
    </CardContent>
  </Card>
</template>
