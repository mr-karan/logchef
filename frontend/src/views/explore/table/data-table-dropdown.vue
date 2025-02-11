<script setup lang="ts">
import { MoreHorizontal, Copy, ChevronDown } from 'lucide-vue-next'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Button } from '@/components/ui/button'
import { useToast } from '@/components/ui/toast'
import { TOAST_DURATION } from '@/lib/constants'

const props = defineProps<{
  log: Record<string, any>
}>()

const emit = defineEmits<{
  (e: 'expand'): void
  (e: 'copy'): void
}>()

const { toast } = useToast()

const copyToClipboard = () => {
  const text = JSON.stringify(props.log, null, 2)
  navigator.clipboard.writeText(text)
  toast({
    title: 'Copied',
    description: 'Log data copied to clipboard',
    duration: TOAST_DURATION.SUCCESS,
  })
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button variant="ghost" class="w-8 h-8 p-0 actions-dropdown">
        <span class="sr-only">Open menu</span>
        <MoreHorizontal class="w-4 h-4" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="actions-dropdown">
      <DropdownMenuLabel>Actions</DropdownMenuLabel>
      <DropdownMenuItem @click="copyToClipboard">
        <Copy class="w-4 h-4 mr-2" />
        Copy Log
      </DropdownMenuItem>
      <DropdownMenuItem @click="$emit('expand')">
        <ChevronDown class="w-4 h-4 mr-2" />
        Expand/Collapse
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<style>
/* Customize vue-json-pretty theme to match your app's theme */
.vjs-tree {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace !important;
  font-size: 0.875rem !important;
  line-height: 1.25rem !important;
}

.vjs-tree .vjs-value {
  color: var(--foreground) !important;
}

.vjs-tree .vjs-key {
  color: var(--muted-foreground) !important;
}

.vjs-tree .vjs-value__string {
  color: hsl(var(--primary)) !important;
}

.vjs-tree .vjs-value__number {
  color: hsl(var(--success)) !important;
}

.vjs-tree .vjs-value__boolean {
  color: hsl(var(--warning)) !important;
}

.vjs-tree .vjs-value__null {
  color: hsl(var(--destructive)) !important;
}
</style>