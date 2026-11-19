<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

// The pages of the alerts area. Switching keeps the team and source query.
const { t } = useI18n();
const route = useRoute();
const router = useRouter();

function go(name: string | number) {
  if (name !== route.name) void router.push({ name: String(name), query: route.query });
}
</script>

<template>
  <Tabs :model-value="String(route.name)" @update:model-value="go">
    <TabsList>
      <TabsTrigger value="AlertsOverview">{{ t("alerts.tabRules") }}</TabsTrigger>
      <TabsTrigger value="AlertSilences">{{ t("alerts.tabSilences") }}</TabsTrigger>
      <TabsTrigger value="NotificationDestinations">{{ t("alerts.destinations") }}</TabsTrigger>
    </TabsList>
  </Tabs>
</template>
