<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed, onMounted, shallowRef } from "vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { SearchableSelect, type SearchableItem } from "@/components/ui/searchable-select";
import ConfirmDialog from "@/components/ui/ConfirmDialog.vue";
import { EmptyState, LoadingState, PageHeader, PageSection } from "@/components/layout";
import TokenScopePicker from "@/components/tokens/TokenScopePicker.vue";
import { useServiceAccountsStore } from "@/stores/serviceAccounts";
import { useTeamsStore } from "@/stores/teams";
import { useToast } from "@/composables/useToast";
import { formatDate } from "@/utils/format";
import { formatScopes, READ_ONLY_SCOPES, type TokenScope } from "@/lib/tokenScopes";
import { getExpiryStatus, isTokenExpired } from "@/lib/tokenExpiry";
import { AlertTriangle, Bot, Calendar, Clock, Copy, KeyRound, Loader2, Plus, Trash2, Shield, Users, X } from "lucide-vue-next";
import type { User } from "@/types";
import type { UserTeamMembership } from "@/api/teams";

interface CreatedTokenData {
  token: string;
  api_token: {
    id: number;
    name: string;
    expires_at?: string;
    scopes: TokenScope[];
  };
}

const serviceAccountsStore = useServiceAccountsStore();
const teamsStore = useTeamsStore();
const { t } = useI18n();
const { toast } = useToast();

const showCreateAccountDialog = shallowRef(false);
const showCreateTokenDialog = shallowRef(false);
const showTokenDisplay = shallowRef(false);
const newAccountName = shallowRef("");
const newTokenName = shallowRef("");
const newTokenExpiry = shallowRef("30d");
const selectedScopes = shallowRef<TokenScope[]>([...READ_ONLY_SCOPES]);
const selectedAccount = shallowRef<User | null>(null);
const createdTokenData = shallowRef<CreatedTokenData | null>(null);
const accountToDelete = shallowRef<User | null>(null);
const tokenToDelete = shallowRef<{ account: User; tokenId: number; tokenName: string } | null>(null);
const teamsDialogAccount = shallowRef<User | null>(null);
const newTeamId = shallowRef("");
const newTeamRole = shallowRef<'admin' | 'member' | 'editor'>('member');

const formatScopeSummary = (scopes: TokenScope[] | undefined) =>
  formatScopes(scopes, (key, count) => count === undefined ? t(key) : t(key, { count }, count));

function expiryText(expiresAt: string | null | undefined) {
  const status = getExpiryStatus(expiresAt);
  switch (status.kind) {
    case "never": return t("access.tokenNeverExpires");
    case "expired": return t("access.tokenExpired", { date: status.date });
    case "expiringSoon": return t("access.tokenExpiresSoon", { days: status.days }, status.days);
    case "expires": return t("access.tokenExpiresOn", { date: status.date });
  }
}

function translateTeamRole(role: string) {
  switch (role) {
    case 'admin': return t('access.roleAdmin')
    case 'editor': return t('access.roleEditor')
    case 'member': return t('access.roleMember')
    default: return role
  }
}

const expiryOptions = [
  { value: "7d", labelKey: "access.tokenExpiry7Days", hours: 7 * 24 },
  { value: "30d", labelKey: "access.tokenExpiry30Days", hours: 30 * 24 },
  { value: "90d", labelKey: "access.tokenExpiry90Days", hours: 90 * 24 },
  { value: "never", labelKey: "access.tokenNeverExpires", hours: null },
];

const accounts = computed(() => serviceAccountsStore.accounts);

onMounted(async () => {
  await loadAccountsAndTokens();
});

async function loadAccountsAndTokens() {
  const result = await serviceAccountsStore.loadAccounts(true);
  if (!result.success) return;
  await Promise.all(
    accounts.value.flatMap((account) => [
      serviceAccountsStore.loadTokens(account.id, true),
      serviceAccountsStore.loadTeams(account.id, true),
    ])
  );
}

function tokensFor(account: User) {
  return serviceAccountsStore.tokensByAccount[account.id] || [];
}

function teamsFor(account: User): UserTeamMembership[] {
  return serviceAccountsStore.teamsByAccount[account.id] || [];
}

const availableTeamsForAccount = computed(() => {
  const account = teamsDialogAccount.value;
  if (!account) return [];
  const existing = new Set(teamsFor(account).map((t) => t.id));
  return (teamsStore.adminTeams || []).filter((team) => !existing.has(team.id));
});
const availableTeamItemsForAccount = computed<SearchableItem[]>(() =>
  availableTeamsForAccount.value.map((team) => ({ value: String(team.id), label: team.name }))
);

async function openTeamsDialog(account: User) {
  teamsDialogAccount.value = account;
  newTeamId.value = "";
  newTeamRole.value = "member";
  if (!teamsStore.adminTeams || teamsStore.adminTeams.length === 0) {
    await teamsStore.loadAdminTeams();
  }
  await serviceAccountsStore.loadTeams(account.id, true);
}

function closeTeamsDialog() {
  teamsDialogAccount.value = null;
}

async function addAccountToTeam() {
  if (!teamsDialogAccount.value || !newTeamId.value) return;
  const teamId = Number(newTeamId.value);
  if (!Number.isFinite(teamId) || teamId <= 0) return;
  const result = await serviceAccountsStore.addToTeam(teamsDialogAccount.value.id, {
    team_id: teamId,
    role: newTeamRole.value,
  });
  if (result.success) {
    newTeamId.value = "";
    newTeamRole.value = "member";
  }
}

async function removeAccountFromTeam(account: User, teamId: number) {
  await serviceAccountsStore.removeFromTeam(account.id, teamId);
}

function openTokenDialog(account: User) {
  selectedAccount.value = account;
  newTokenName.value = `${account.full_name} token`;
  newTokenExpiry.value = "30d";
  selectedScopes.value = [...READ_ONLY_SCOPES];
  showCreateTokenDialog.value = true;
}

async function createAccount() {
  const name = newAccountName.value.trim();
  if (!name) {
    toast({ title: t('ui.error'), description: t('access.enterServiceAccountName'), variant: "destructive" });
    return;
  }
  const result = await serviceAccountsStore.createAccount({ name });
  if (result.success) {
    newAccountName.value = "";
    showCreateAccountDialog.value = false;
    await loadAccountsAndTokens();
  }
}

async function createToken() {
  if (!selectedAccount.value) return;
  if (!newTokenName.value.trim()) {
    toast({ title: t('ui.error'), description: t('access.enterTokenName'), variant: "destructive" });
    return;
  }
  if (selectedScopes.value.length === 0) {
    toast({ title: t('ui.error'), description: t('access.selectTokenScope'), variant: "destructive" });
    return;
  }

  const selectedOption = expiryOptions.find((option) => option.value === newTokenExpiry.value);
  const expiresAt = selectedOption?.hours
    ? new Date(Date.now() + selectedOption.hours * 60 * 60 * 1000).toISOString()
    : undefined;

  const result = await serviceAccountsStore.createToken(selectedAccount.value.id, {
    name: newTokenName.value.trim(),
    expires_at: expiresAt,
    scopes: selectedScopes.value,
  });
  if (result.success && result.data) {
    createdTokenData.value = result.data as CreatedTokenData;
    showCreateTokenDialog.value = false;
    showTokenDisplay.value = true;
  }
}

async function confirmDeleteAccount() {
  const target = accountToDelete.value;
  accountToDelete.value = null;
  if (!target) return;
  await serviceAccountsStore.deleteAccount(target.id);
}

async function confirmDeleteToken() {
  const target = tokenToDelete.value;
  tokenToDelete.value = null;
  if (!target) return;
  await serviceAccountsStore.deleteToken(target.account.id, target.tokenId);
}

async function copyToClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    toast({ title: t('ui.error'), description: t('pages.copyFailed'), variant: "destructive" });
  }
}

function closeTokenDisplay() {
  showTokenDisplay.value = false;
  createdTokenData.value = null;
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader :title="t('routes.serviceTokens')" :description="t('access.serviceTokensDescription')" />

    <PageSection :title="t('access.serviceAccounts')" :description="t('access.serviceAccountsDescription')">
      <template #actions>
        <Dialog v-model:open="showCreateAccountDialog">
          <DialogTrigger asChild>
            <Button class="gap-2">
              <Plus class="h-4 w-4" />
              {{ t('access.createServiceAccount') }}
            </Button>
          </DialogTrigger>
          <DialogContent class="sm:max-w-[425px]">
            <DialogHeader>
              <DialogTitle>{{ t('access.createServiceAccount') }}</DialogTitle>
              <DialogDescription>
                {{ t('access.serviceAccountLoginDescription') }}
              </DialogDescription>
            </DialogHeader>
            <div class="grid gap-2 py-4">
              <Label for="service-account-name">{{ t('ui.name') }}</Label>
              <Input id="service-account-name" v-model="newAccountName" :placeholder="t('access.serviceAccountNameExample')" @keydown.enter="createAccount" />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" @click="showCreateAccountDialog = false">{{ t('ui.cancel') }}</Button>
              <Button type="button" @click="createAccount" :disabled="serviceAccountsStore.isLoadingOperation('createServiceAccount') || !newAccountName.trim()">
                <Loader2 v-if="serviceAccountsStore.isLoadingOperation('createServiceAccount')" class="mr-2 h-4 w-4 animate-spin" />
                {{ t('ui.create') }}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </template>

      <LoadingState v-if="serviceAccountsStore.isLoading && accounts.length === 0" :label="t('access.loadingServiceAccounts')" />
      <EmptyState v-else-if="accounts.length === 0" :icon="Bot" :title="t('access.noServiceAccounts')" :description="t('access.noServiceAccountsDescription')" />

      <div v-else class="space-y-4">
        <article v-for="account in accounts" :key="account.id" class="rounded-md border p-4 space-y-4">
          <div class="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
            <div class="space-y-1">
              <div class="flex items-center gap-2">
                <Bot class="h-4 w-4 text-muted-foreground" />
                <h3 class="font-medium">{{ account.full_name }}</h3>
                <Badge variant="secondary">{{ t('access.serviceAccount') }}</Badge>
              </div>
              <p class="text-sm text-muted-foreground font-mono">{{ account.email }}</p>
              <p class="text-xs text-muted-foreground">{{ t('access.createdOn', { date: formatDate(account.created_at) }) }}</p>
            </div>
            <div class="flex gap-2">
              <Button size="sm" variant="outline" class="gap-2" @click="openTeamsDialog(account)">
                <Users class="h-4 w-4" />
                {{ t('access.manageTeams') }}
              </Button>
              <Button size="sm" variant="outline" class="gap-2" @click="openTokenDialog(account)">
                <KeyRound class="h-4 w-4" />
                {{ t('access.createToken') }}
              </Button>
              <Button size="sm" variant="ghost" class="text-destructive hover:text-destructive" @click="accountToDelete = account">
                <Trash2 class="h-4 w-4" />
              </Button>
            </div>
          </div>

          <div class="space-y-2">
            <h4 class="text-sm font-medium">{{ t('routes.teams') }}</h4>
            <Alert v-if="teamsFor(account).length === 0" variant="destructive" class="py-2">
              <AlertTriangle class="h-4 w-4" />
              <AlertDescription>
                {{ t('access.notInTeamDescription') }}
                <button type="button" class="ml-1 underline" @click="openTeamsDialog(account)">{{ t('access.manageTeams') }}</button>
              </AlertDescription>
            </Alert>
            <div v-else class="flex flex-wrap gap-2">
              <Badge v-for="team in teamsFor(account)" :key="team.id" variant="outline" class="gap-1.5">
                <span>{{ team.name }}</span>
                <span class="text-muted-foreground">·</span>
                <span class="text-muted-foreground">{{ translateTeamRole(team.role) }}</span>
              </Badge>
            </div>
          </div>

          <div class="space-y-2">
            <h4 class="text-sm font-medium">{{ t('access.tokens') }}</h4>
            <div v-if="tokensFor(account).length === 0" class="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
              {{ t('access.noTokensYet') }}
            </div>
            <div v-else class="space-y-2">
              <div v-for="token in tokensFor(account)" :key="token.id" class="flex items-center justify-between rounded-md border p-3">
                <div class="space-y-1 min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="font-medium" :class="{ 'text-muted-foreground line-through': isTokenExpired(token.expires_at) }">{{ token.name }}</span>
                    <Badge variant="outline" class="font-mono">{{ token.prefix }}</Badge>
                    <Badge
                      :variant="getExpiryStatus(token.expires_at).variant"
                      :class="{
                        'bg-destructive text-destructive-foreground': getExpiryStatus(token.expires_at).isExpired,
                        'border-amber-500 text-amber-700': getExpiryStatus(token.expires_at).variant === 'outline'
                      }"
                    >
                      <AlertTriangle v-if="getExpiryStatus(token.expires_at).isExpired" class="h-3 w-3 mr-1" />
                      {{ expiryText(token.expires_at) }}
                    </Badge>
                    <Badge variant="secondary">{{ formatScopeSummary(token.scopes) }}</Badge>
                  </div>
                  <div class="flex flex-wrap gap-3 text-xs text-muted-foreground">
                    <span class="inline-flex items-center gap-1"><Calendar class="h-3 w-3" />{{ t('access.createdOn', { date: formatDate(token.created_at) }) }}</span>
                    <span class="inline-flex items-center gap-1"><Clock class="h-3 w-3" />{{ token.last_used_at ? t('access.lastUsedOn', { date: formatDate(token.last_used_at) }) : t('access.neverUsed') }}</span>
                  </div>
                </div>
                <Button variant="ghost" size="sm" class="text-destructive hover:text-destructive" @click="tokenToDelete = { account, tokenId: token.id, tokenName: token.name }">
                  <Trash2 class="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
        </article>
      </div>
    </PageSection>

    <Dialog v-model:open="showCreateTokenDialog">
      <DialogContent class="sm:max-w-[760px] max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{{ t('access.createServiceToken') }}</DialogTitle>
          <DialogDescription>
            {{ t('access.createServiceTokenDescription', { name: selectedAccount?.full_name ?? '' }) }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4 py-4">
          <div class="grid gap-2">
            <Label for="service-token-name">{{ t('access.tokenName') }}</Label>
            <Input id="service-token-name" v-model="newTokenName" />
          </div>
          <div class="grid gap-2">
            <Label for="service-token-expiry">{{ t('access.expiration') }}</Label>
            <Select v-model="newTokenExpiry">
              <SelectTrigger id="service-token-expiry"><SelectValue :placeholder="t('access.selectExpiration')" /></SelectTrigger>
              <SelectContent>
                <SelectItem v-for="option in expiryOptions" :key="option.value" :value="option.value">{{ t(option.labelKey) }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <TokenScopePicker v-model="selectedScopes" />
          <Alert>
            <Shield class="h-4 w-4" />
            <AlertDescription>{{ t('access.tokenShownOnce') }}</AlertDescription>
          </Alert>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" @click="showCreateTokenDialog = false">{{ t('ui.cancel') }}</Button>
          <Button type="button" @click="createToken" :disabled="!newTokenName.trim() || selectedScopes.length === 0">
            {{ t('access.createToken') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="showTokenDisplay">
      <DialogContent class="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{{ t('access.serviceTokenCreated') }}</DialogTitle>
          <DialogDescription>{{ t('access.copyTokenNow') }}</DialogDescription>
        </DialogHeader>
        <div class="space-y-4">
          <div>
            <Label>{{ t('access.token') }}</Label>
            <div class="mt-2 flex items-center gap-2 rounded-md bg-muted p-3">
              <code class="min-w-0 flex-1 break-all text-sm">{{ createdTokenData?.token }}</code>
              <Button size="sm" variant="outline" @click="copyToClipboard(createdTokenData?.token || '')">
                <Copy class="h-4 w-4" />
              </Button>
            </div>
          </div>
          <Alert>
            <Shield class="h-4 w-4" />
            <AlertDescription>{{ t('access.tokenSecretWarning') }}</AlertDescription>
          </Alert>
        </div>
        <DialogFooter>
          <Button class="w-full" @click="closeTokenDisplay">{{ t('access.copiedToken') }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog :open="teamsDialogAccount !== null" @update:open="(open) => { if (!open) closeTeamsDialog() }">
      <DialogContent class="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{{ t('access.manageTeamMembership') }}</DialogTitle>
          <DialogDescription>
            {{ t('access.serviceAccountTeamAccessDescription', { name: teamsDialogAccount?.full_name ?? '' }) }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4 py-2">
          <div class="space-y-2">
            <Label class="text-sm">{{ t('access.currentTeams') }}</Label>
            <div v-if="teamsDialogAccount && teamsFor(teamsDialogAccount).length === 0"
              class="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
              {{ t('access.notInAnyTeam') }}
            </div>
            <div v-else-if="teamsDialogAccount" class="space-y-2">
              <div v-for="team in teamsFor(teamsDialogAccount)" :key="team.id"
                class="flex items-center justify-between rounded-md border p-2.5">
                <div class="flex flex-col">
                  <span class="font-medium">{{ team.name }}</span>
                  <span class="text-xs text-muted-foreground">{{ translateTeamRole(team.role) }}</span>
                </div>
                <Button variant="ghost" size="icon" class="text-destructive hover:text-destructive"
                  @click="removeAccountFromTeam(teamsDialogAccount, team.id)">
                  <X class="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>

          <div class="space-y-2 border-t pt-4">
            <Label class="text-sm">{{ t('access.addToTeam') }}</Label>
            <div class="grid grid-cols-[1fr_auto] gap-2">
              <SearchableSelect
                v-model="newTeamId"
                :items="availableTeamItemsForAccount"
                :placeholder="t('access.selectTeam')"
                :search-placeholder="t('access.searchTeams')"
                :empty-text="t('access.noTeamsAvailable')" />
              <Select v-model="newTeamRole">
                <SelectTrigger class="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">{{ t('access.roleMember') }}</SelectItem>
                  <SelectItem value="editor">{{ t('access.roleEditor') }}</SelectItem>
                  <SelectItem value="admin">{{ t('access.roleAdmin') }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Button type="button" class="w-full gap-2" :disabled="!newTeamId"
              @click="addAccountToTeam">
              <Plus class="h-4 w-4" />
              {{ t('access.addToTeam') }}
            </Button>
          </div>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" @click="closeTeamsDialog">{{ t('access.done') }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDialog
      :open="accountToDelete !== null"
      :title="t('access.deleteServiceAccountTitle')"
      :description="accountToDelete ? t('access.deleteServiceAccountConfirmation', { name: accountToDelete.full_name }) : undefined"
      :confirm-text="t('ui.delete')"
      destructive
      @update:open="(open) => { if (!open) accountToDelete = null }"
      @confirm="confirmDeleteAccount"
    />

    <ConfirmDialog
      :open="tokenToDelete !== null"
      :title="t('access.deleteServiceTokenTitle')"
      :description="tokenToDelete ? t('access.deleteServiceTokenConfirmation', { name: tokenToDelete.tokenName }) : undefined"
      :confirm-text="t('ui.delete')"
      destructive
      @update:open="(open) => { if (!open) tokenToDelete = null }"
      @confirm="confirmDeleteToken"
    />
  </div>
</template>
