<script setup lang="ts">
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

const expiryOptions = [
  { value: "7d", label: "7 days", hours: 7 * 24 },
  { value: "30d", label: "30 days", hours: 30 * 24 },
  { value: "90d", label: "90 days", hours: 90 * 24 },
  { value: "never", label: "Never expires", hours: null },
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
    toast({ title: "Error", description: "Enter a service account name", variant: "destructive" });
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
    toast({ title: "Error", description: "Enter a token name", variant: "destructive" });
    return;
  }
  if (selectedScopes.value.length === 0) {
    toast({ title: "Error", description: "Select at least one scope", variant: "destructive" });
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
    toast({ title: "Error", description: "Failed to copy to clipboard", variant: "destructive" });
  }
}

function closeTokenDisplay() {
  showTokenDisplay.value = false;
  createdTokenData.value = null;
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader title="Service tokens" description="Create non-login service principals and issue scoped tokens for automation." />

    <PageSection title="Service accounts" description="Add these principals to teams, then issue tokens with explicit scopes.">
      <template #actions>
        <Dialog v-model:open="showCreateAccountDialog">
          <DialogTrigger asChild>
            <Button class="gap-2">
              <Plus class="h-4 w-4" />
              Create service account
            </Button>
          </DialogTrigger>
          <DialogContent class="sm:max-w-[425px]">
            <DialogHeader>
              <DialogTitle>Create service account</DialogTitle>
              <DialogDescription>
                Service accounts cannot log in. They authenticate only through service tokens.
              </DialogDescription>
            </DialogHeader>
            <div class="grid gap-2 py-4">
              <Label for="service-account-name">Name</Label>
              <Input id="service-account-name" v-model="newAccountName" placeholder="e.g. CI log reader" @keydown.enter="createAccount" />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" @click="showCreateAccountDialog = false">Cancel</Button>
              <Button type="button" @click="createAccount" :disabled="serviceAccountsStore.isLoadingOperation('createServiceAccount') || !newAccountName.trim()">
                <Loader2 v-if="serviceAccountsStore.isLoadingOperation('createServiceAccount')" class="mr-2 h-4 w-4 animate-spin" />
                Create
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </template>

      <LoadingState v-if="serviceAccountsStore.isLoading && accounts.length === 0" label="Loading service accounts…" />
      <EmptyState v-else-if="accounts.length === 0" :icon="Bot" title="No service accounts" description="Create a service account for automation, then add it to teams for source access." />

      <div v-else class="space-y-4">
        <article v-for="account in accounts" :key="account.id" class="rounded-md border p-4 space-y-4">
          <div class="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
            <div class="space-y-1">
              <div class="flex items-center gap-2">
                <Bot class="h-4 w-4 text-muted-foreground" />
                <h3 class="font-medium">{{ account.full_name }}</h3>
                <Badge variant="secondary">Service account</Badge>
              </div>
              <p class="text-sm text-muted-foreground font-mono">{{ account.email }}</p>
              <p class="text-xs text-muted-foreground">Created {{ formatDate(account.created_at) }}</p>
            </div>
            <div class="flex gap-2">
              <Button size="sm" variant="outline" class="gap-2" @click="openTeamsDialog(account)">
                <Users class="h-4 w-4" />
                Manage teams
              </Button>
              <Button size="sm" variant="outline" class="gap-2" @click="openTokenDialog(account)">
                <KeyRound class="h-4 w-4" />
                Create token
              </Button>
              <Button size="sm" variant="ghost" class="text-destructive hover:text-destructive" @click="accountToDelete = account">
                <Trash2 class="h-4 w-4" />
              </Button>
            </div>
          </div>

          <div class="space-y-2">
            <h4 class="text-sm font-medium">Teams</h4>
            <Alert v-if="teamsFor(account).length === 0" variant="destructive" class="py-2">
              <AlertTriangle class="h-4 w-4" />
              <AlertDescription>
                Not in any team — tokens for this account can authenticate but won't reach any source.
                <button type="button" class="ml-1 underline" @click="openTeamsDialog(account)">Manage teams</button>
              </AlertDescription>
            </Alert>
            <div v-else class="flex flex-wrap gap-2">
              <Badge v-for="team in teamsFor(account)" :key="team.id" variant="outline" class="gap-1.5">
                <span>{{ team.name }}</span>
                <span class="text-muted-foreground">·</span>
                <span class="capitalize text-muted-foreground">{{ team.role }}</span>
              </Badge>
            </div>
          </div>

          <div class="space-y-2">
            <h4 class="text-sm font-medium">Tokens</h4>
            <div v-if="tokensFor(account).length === 0" class="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
              No tokens yet.
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
                      {{ getExpiryStatus(token.expires_at).text }}
                    </Badge>
                    <Badge variant="secondary">{{ formatScopes(token.scopes) }}</Badge>
                  </div>
                  <div class="flex flex-wrap gap-3 text-xs text-muted-foreground">
                    <span class="inline-flex items-center gap-1"><Calendar class="h-3 w-3" />Created {{ formatDate(token.created_at) }}</span>
                    <span class="inline-flex items-center gap-1"><Clock class="h-3 w-3" />{{ token.last_used_at ? `Last used ${formatDate(token.last_used_at)}` : 'Never used' }}</span>
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
          <DialogTitle>Create service token</DialogTitle>
          <DialogDescription>
            Select a preset or choose exact scopes. The token still only reaches sources available to {{ selectedAccount?.full_name }} through team membership.
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4 py-4">
          <div class="grid gap-2">
            <Label for="service-token-name">Token name</Label>
            <Input id="service-token-name" v-model="newTokenName" />
          </div>
          <div class="grid gap-2">
            <Label for="service-token-expiry">Expiration</Label>
            <Select v-model="newTokenExpiry">
              <SelectTrigger id="service-token-expiry"><SelectValue placeholder="Select expiration" /></SelectTrigger>
              <SelectContent>
                <SelectItem v-for="option in expiryOptions" :key="option.value" :value="option.value">{{ option.label }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <TokenScopePicker v-model="selectedScopes" />
          <Alert>
            <Shield class="h-4 w-4" />
            <AlertDescription>The token is shown once. Store it securely before closing the next dialog.</AlertDescription>
          </Alert>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" @click="showCreateTokenDialog = false">Cancel</Button>
          <Button type="button" @click="createToken" :disabled="!newTokenName.trim() || selectedScopes.length === 0">
            Create token
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="showTokenDisplay">
      <DialogContent class="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>Service token created</DialogTitle>
          <DialogDescription>Copy this token now. It will not be shown again.</DialogDescription>
        </DialogHeader>
        <div class="space-y-4">
          <div>
            <Label>Token</Label>
            <div class="mt-2 flex items-center gap-2 rounded-md bg-muted p-3">
              <code class="min-w-0 flex-1 break-all text-sm">{{ createdTokenData?.token }}</code>
              <Button size="sm" variant="outline" @click="copyToClipboard(createdTokenData?.token || '')">
                <Copy class="h-4 w-4" />
              </Button>
            </div>
          </div>
          <Alert>
            <Shield class="h-4 w-4" />
            <AlertDescription>Treat this value like a password. Delete and recreate the token if it is lost.</AlertDescription>
          </Alert>
        </div>
        <DialogFooter>
          <Button class="w-full" @click="closeTokenDisplay">I've copied my token</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog :open="teamsDialogAccount !== null" @update:open="(open) => { if (!open) closeTeamsDialog() }">
      <DialogContent class="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>Manage team membership</DialogTitle>
          <DialogDescription>
            {{ teamsDialogAccount?.full_name }} can only reach sources owned by teams it belongs to.
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4 py-2">
          <div class="space-y-2">
            <Label class="text-sm">Current teams</Label>
            <div v-if="teamsDialogAccount && teamsFor(teamsDialogAccount).length === 0"
              class="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
              Not in any team yet.
            </div>
            <div v-else-if="teamsDialogAccount" class="space-y-2">
              <div v-for="team in teamsFor(teamsDialogAccount)" :key="team.id"
                class="flex items-center justify-between rounded-md border p-2.5">
                <div class="flex flex-col">
                  <span class="font-medium">{{ team.name }}</span>
                  <span class="text-xs text-muted-foreground capitalize">{{ team.role }}</span>
                </div>
                <Button variant="ghost" size="icon" class="text-destructive hover:text-destructive"
                  @click="removeAccountFromTeam(teamsDialogAccount, team.id)">
                  <X class="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>

          <div class="space-y-2 border-t pt-4">
            <Label class="text-sm">Add to team</Label>
            <div class="grid grid-cols-[1fr_auto] gap-2">
              <SearchableSelect
                v-model="newTeamId"
                :items="availableTeamItemsForAccount"
                placeholder="Select a team"
                search-placeholder="Search teams…"
                empty-text="No teams available." />
              <Select v-model="newTeamRole">
                <SelectTrigger class="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">Member</SelectItem>
                  <SelectItem value="editor">Editor</SelectItem>
                  <SelectItem value="admin">Admin</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Button type="button" class="w-full gap-2" :disabled="!newTeamId"
              @click="addAccountToTeam">
              <Plus class="h-4 w-4" />
              Add to team
            </Button>
          </div>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" @click="closeTeamsDialog">Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDialog
      :open="accountToDelete !== null"
      title="Delete service account?"
      :description="accountToDelete ? `Delete &quot;${accountToDelete.full_name}&quot; and revoke all of its tokens? This cannot be undone.` : undefined"
      confirm-text="Delete"
      destructive
      @update:open="(open) => { if (!open) accountToDelete = null }"
      @confirm="confirmDeleteAccount"
    />

    <ConfirmDialog
      :open="tokenToDelete !== null"
      title="Delete service token?"
      :description="tokenToDelete ? `Delete &quot;${tokenToDelete.tokenName}&quot;? Any automation using it will lose access.` : undefined"
      confirm-text="Delete"
      destructive
      @update:open="(open) => { if (!open) tokenToDelete = null }"
      @confirm="confirmDeleteToken"
    />
  </div>
</template>
