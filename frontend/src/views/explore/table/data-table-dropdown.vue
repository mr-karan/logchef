<script setup lang="ts">
import { MoreHorizontal, Copy, ChevronDown } from 'lucide-vue-next'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Button } from '@/components/ui/button'

defineProps<{
  log: Record<string, any>
}>()

defineEmits<{
  (e: 'expand'): void
  (e: 'copy'): void
}>()
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
      <DropdownMenuItem @click="$emit('copy')">
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