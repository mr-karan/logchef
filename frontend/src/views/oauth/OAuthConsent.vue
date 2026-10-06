<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { AlertCircle, Loader2, ShieldAlert } from "lucide-vue-next";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { oauthApi, type OAuthConsentRequest } from "@/api/oauth";
import { TOKEN_SCOPE_OPTIONS } from "@/lib/tokenScopes";

const { t } = useI18n();
const route = useRoute();

type ViewState =
  | { kind: "loading" }
  | { kind: "ready"; request: OAuthConsentRequest }
  | { kind: "deciding"; request: OAuthConsentRequest; approve: boolean }
  | { kind: "redirecting" }
  | { kind: "error"; message: string };

const view = ref<ViewState>({ kind: "loading" });

const requestId = computed(() => {
  const value = route.query.request;
  return typeof value === "string" ? value : "";
});

const request = computed(() =>
  view.value.kind === "ready" || view.value.kind === "deciding" ? view.value.request : null,
);

// The host the browser returns to after the decision. A phishing link would
// show an unexpected host here.
const redirectHost = computed(() => {
  if (!request.value) return "";
  try {
    return new URL(request.value.redirect_uri).host;
  } catch {
    return request.value.redirect_uri;
  }
});

const resourceLabel = computed(() =>
  request.value?.resource_kind === "api" ? t("pages.oauthResourceApi") : t("pages.oauthResourceMcp"),
);

const scopeRows = computed(() =>
  (request.value?.scopes ?? []).map((item) => {
    const option = TOKEN_SCOPE_OPTIONS.find((o) => o.value === item.scope);
    return {
      scope: item.scope,
      label: option ? t(option.labelKey) : item.scope,
      description: option ? t(option.descriptionKey) : item.description,
    };
  }),
);

function errorMessage(err: unknown): string {
  if (err && typeof err === "object" && "message" in err && typeof err.message === "string") {
    return err.message;
  }
  return t("pages.unexpectedError");
}

onMounted(async () => {
  if (!requestId.value) {
    view.value = { kind: "error", message: t("pages.oauthRequestMissing") };
    return;
  }
  try {
    const response = await oauthApi.getRequest(requestId.value);
    if (response.status === "success" && response.data) {
      view.value = { kind: "ready", request: response.data };
    } else {
      view.value = { kind: "error", message: t("pages.oauthRequestExpired") };
    }
  } catch {
    view.value = { kind: "error", message: t("pages.oauthRequestExpired") };
  }
});

async function decide(approve: boolean) {
  if (view.value.kind !== "ready") return;
  const current = view.value.request;
  view.value = { kind: "deciding", request: current, approve };
  try {
    const response = await oauthApi.decide(current.id, approve);
    if (response.status === "success" && response.data?.redirect_url) {
      view.value = { kind: "redirecting" };
      window.location.assign(response.data.redirect_url);
      return;
    }
    view.value = { kind: "error", message: t("pages.oauthDecisionFailed") };
  } catch (err) {
    view.value = { kind: "error", message: errorMessage(err) };
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-background p-4">
    <Card class="mx-auto w-full max-w-lg">
      <template v-if="view.kind === 'loading' || view.kind === 'redirecting'">
        <CardContent class="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
          <Loader2 class="h-4 w-4 animate-spin" />
          {{ view.kind === "loading" ? t("pages.oauthLoading") : t("pages.oauthRedirecting") }}
        </CardContent>
      </template>

      <template v-else-if="view.kind === 'error'">
        <CardHeader>
          <CardTitle class="text-xl">{{ t("pages.oauthErrorTitle") }}</CardTitle>
        </CardHeader>
        <CardContent>
          <Alert variant="destructive">
            <AlertCircle class="h-4 w-4" />
            <AlertDescription>{{ view.message }}</AlertDescription>
          </Alert>
        </CardContent>
      </template>

      <template v-else-if="request">
        <CardHeader>
          <CardTitle class="text-xl">
            {{ t("pages.oauthConsentTitle", { client: request.client.name }) }}
          </CardTitle>
          <p v-if="request.client.host" class="text-sm" data-testid="oauth-client-host">
            {{ t("pages.oauthPublishedBy") }}
            <span class="font-mono font-semibold break-all">{{ request.client.host }}</span>
          </p>
          <CardDescription>{{ t("pages.oauthConsentDescription") }}</CardDescription>
        </CardHeader>

        <CardContent class="space-y-5">
          <Alert class="border-amber-500/60 bg-amber-50 text-amber-950 dark:bg-amber-950/30 dark:text-amber-100" data-testid="oauth-phishing-warning">
            <ShieldAlert class="h-4 w-4" />
            <AlertTitle>{{ t("pages.oauthPhishingTitle") }}</AlertTitle>
            <AlertDescription>
              {{ t("pages.oauthPhishingWarning") }}
              <template v-if="request.client.host">
                {{ t("pages.oauthCimdNotice", { client: request.client.name, host: request.client.host }) }}
              </template>
            </AlertDescription>
          </Alert>

          <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
            <dt class="text-muted-foreground">{{ t("pages.oauthApplication") }}</dt>
            <dd class="font-medium break-all">
              {{ request.client.name }}
              <span class="font-mono text-xs text-muted-foreground">({{ request.client.id }})</span>
            </dd>
            <dt class="text-muted-foreground">{{ t("pages.oauthReturnsTo") }}</dt>
            <dd class="font-mono font-medium break-all" data-testid="oauth-redirect-host">{{ redirectHost }}</dd>
            <dt class="text-muted-foreground">{{ t("pages.oauthAccessType") }}</dt>
            <dd>{{ resourceLabel }}</dd>
            <dt class="text-muted-foreground">{{ t("pages.oauthInstance") }}</dt>
            <dd class="font-mono break-all">{{ request.instance }}</dd>
            <dt class="text-muted-foreground">{{ t("pages.oauthSignedInAs") }}</dt>
            <dd class="break-all">{{ request.user.email }}</dd>
          </dl>

          <div class="space-y-2">
            <p class="text-sm font-medium">{{ t("pages.oauthPermissions") }}</p>
            <ul class="space-y-2" data-testid="oauth-scopes">
              <li v-for="row in scopeRows" :key="row.scope" class="rounded-md border px-3 py-2">
                <div class="flex items-center gap-2 text-sm font-medium">
                  {{ row.label }}
                  <Badge variant="outline" class="font-mono text-xs">{{ row.scope }}</Badge>
                </div>
                <p class="text-xs text-muted-foreground">{{ row.description }}</p>
              </li>
            </ul>
            <p class="text-xs text-muted-foreground">
              {{ request.offline_access ? t("pages.oauthOfflineAccess") : t("pages.oauthShortAccess") }}
            </p>
          </div>
        </CardContent>

        <CardFooter class="grid grid-cols-2 gap-3">
          <Button
            variant="secondary"
            size="lg"
            class="w-full"
            :disabled="view.kind === 'deciding'"
            data-testid="oauth-deny"
            @click="decide(false)"
          >
            <Loader2 v-if="view.kind === 'deciding' && !view.approve" class="mr-2 h-4 w-4 animate-spin" />
            {{ t("pages.oauthDeny") }}
          </Button>
          <Button
            size="lg"
            class="w-full"
            :disabled="view.kind === 'deciding'"
            data-testid="oauth-approve"
            @click="decide(true)"
          >
            <Loader2 v-if="view.kind === 'deciding' && view.approve" class="mr-2 h-4 w-4 animate-spin" />
            {{ t("pages.oauthApprove") }}
          </Button>
        </CardFooter>
      </template>
    </Card>
  </div>
</template>
