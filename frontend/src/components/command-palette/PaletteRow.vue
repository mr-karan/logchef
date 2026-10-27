<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { Clock, Play, RotateCcw, LoaderCircle } from 'lucide-vue-next';
import { Kbd } from '@/components/ui/kbd';
import { PALETTE_ICONS } from './icons';
import type { PaletteItem } from './palette';
import PaletteText from './PaletteText.vue';
defineProps<{ item: PaletteItem; selected: boolean; modifier: string }>();
const { t } = useI18n();
</script>

<template>
  <template v-if="item.shape === 'run'">
    <span class="flex size-7 shrink-0 items-center justify-center rounded bg-primary text-primary-foreground"><Play class="size-3.5 fill-current" /></span>
    <span class="min-w-0 flex-1">
      <span class="flex items-center justify-between gap-2 font-medium">{{ t('palette.run.title') }}<Kbd>↵</Kbd></span>
      <span class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
        <span class="text-blue-600 dark:text-blue-400">{{ item.run.team.name }}</span><span class="text-foreground">{{ item.run.source.name }}</span><span>{{ item.run.source.typeLabel }}</span>
        <span class="inline-flex items-center gap-1 text-orange-600 dark:text-orange-400"><Clock class="size-3" />{{ item.run.rangeLabel }}</span><span>{{ item.run.span }}</span>
        <span class="max-w-full truncate font-mono"><PaletteText :text="item.run.query" :tokens="item.run.queryTokens" /></span>
      </span>
    </span>
  </template>
  <template v-else-if="item.shape === 'row'">
    <component :is="PALETTE_ICONS[item.icon]" class="size-4 shrink-0" :class="item.icon === 'askChef' ? 'text-violet-600 dark:text-violet-400' : 'text-muted-foreground'" />
    <span class="min-w-0 flex-1" :class="{ 'font-mono': item.mono }">
      <span class="block truncate"><PaletteText :text="item.label.text" :marked="item.label" /></span>
      <span v-if="item.detail && (item.id === 'fallback' || item.id === 'ask')" class="mt-0.5 block truncate text-[11px] text-muted-foreground">{{ item.detail }}</span>
    </span>
    <span v-if="item.detail && item.id !== 'fallback' && item.id !== 'ask'" class="hidden max-w-[42%] truncate text-[11px] text-muted-foreground sm:block">{{ item.detail }}</span>
    <span v-if="item.badge" class="text-[10px] text-muted-foreground">{{ t('palette.admin') }}</span>
    <span v-if="item.meta" class="shrink-0 text-[11px] text-muted-foreground">{{ item.meta }}</span>
    <span v-if="item.shortcut === 'modEnter' || item.shortcut === 'modShiftEnter'" class="flex shrink-0 gap-1"><Kbd>{{ modifier }}</Kbd><Kbd v-if="item.shortcut === 'modShiftEnter'">⇧</Kbd><Kbd>↵</Kbd></span>
    <Kbd v-else-if="item.tab">Tab</Kbd><Kbd v-else-if="selected && item.enter">↵</Kbd>
  </template>
  <template v-else-if="item.shape === 'blocked'">
    <component :is="PALETTE_ICONS[item.icon]" class="size-4 shrink-0" :class="item.icon === 'askChef' ? 'text-violet-600 dark:text-violet-400' : 'text-muted-foreground'" />
    <span class="min-w-0 flex-1"><span class="block font-medium">{{ item.label }}</span><span class="mt-0.5 block text-[11px] text-muted-foreground">{{ item.reason }}</span></span><Kbd v-if="item.tab">Tab</Kbd>
  </template>
  <template v-else>
    <LoaderCircle v-if="item.state === 'loading'" class="size-4 animate-spin text-muted-foreground" /><RotateCcw v-else class="size-4 text-muted-foreground" />
    <span class="flex-1 text-muted-foreground">{{ item.label }}</span><span v-if="item.state === 'failed'" class="text-[11px]">{{ t('palette.retry') }}</span>
  </template>
</template>
