import { defineStore } from "pinia";
import { computed } from "vue";
import type { User } from "@/types";
import { useBaseStore } from "./base";
import {
  serviceAccountsApi,
  type AddServiceAccountToTeamRequest,
  type CreateServiceAccountRequest,
} from "@/api/serviceAccounts";
import type { APIToken, CreateAPITokenRequest } from "@/api/apiTokens";
import type { UserTeamMembership } from "@/api/teams";
import { i18n } from "@/i18n";

interface ServiceAccountsState {
  accounts: User[];
  tokensByAccount: Record<string, APIToken[]>;
  teamsByAccount: Record<string, UserTeamMembership[]>;
}

export const useServiceAccountsStore = defineStore("serviceAccounts", () => {
  const state = useBaseStore<ServiceAccountsState>({
    accounts: [],
    tokensByAccount: {},
    teamsByAccount: {},
  });

  const accounts = computed(() => state.data.value.accounts || []);
  const tokensByAccount = computed(() => state.data.value.tokensByAccount || {});
  const teamsByAccount = computed(() => state.data.value.teamsByAccount || {});

  async function loadAccounts(forceReload = false) {
    if (!forceReload && state.data.value.accounts.length > 0) {
      return { success: true, data: state.data.value.accounts };
    }
    return await state.callApi({
      apiCall: () => serviceAccountsApi.listServiceAccounts(),
      operationKey: "loadServiceAccounts",
      showToast: false,
      onSuccess: (response) => {
        state.data.value.accounts = (response as User[]) || [];
      },
    });
  }

  async function createAccount(data: CreateServiceAccountRequest) {
    return await state.callApi({
      apiCall: () => serviceAccountsApi.createServiceAccount(data),
      successMessage: i18n.global.t("access.serviceAccountCreated"),
      operationKey: "createServiceAccount",
      onSuccess: async () => {
        await loadAccounts(true);
      },
    });
  }

  async function deleteAccount(id: string) {
    return await state.callApi({
      apiCall: () => serviceAccountsApi.deleteServiceAccount(id),
      successMessage: i18n.global.t("access.serviceAccountDeleted"),
      operationKey: `deleteServiceAccount-${id}`,
      onSuccess: async () => {
        delete state.data.value.tokensByAccount[id];
        await loadAccounts(true);
      },
    });
  }

  async function loadTokens(accountId: string, forceReload = false) {
    if (!forceReload && state.data.value.tokensByAccount[accountId]) {
      return { success: true, data: state.data.value.tokensByAccount[accountId] };
    }
    return await state.callApi({
      apiCall: () => serviceAccountsApi.listTokens(accountId),
      operationKey: `loadServiceAccountTokens-${accountId}`,
      showToast: false,
      onSuccess: (response) => {
        state.data.value.tokensByAccount[accountId] = (response as APIToken[]) || [];
      },
    });
  }

  async function createToken(accountId: string, data: CreateAPITokenRequest) {
    return await state.callApi({
      apiCall: () => serviceAccountsApi.createToken(accountId, data),
      successMessage: i18n.global.t("access.serviceTokenCreated"),
      operationKey: `createServiceAccountToken-${accountId}`,
      onSuccess: async () => {
        await loadTokens(accountId, true);
      },
    });
  }

  async function deleteToken(accountId: string, tokenId: number) {
    return await state.callApi({
      apiCall: () => serviceAccountsApi.deleteToken(accountId, tokenId),
      successMessage: i18n.global.t("access.serviceTokenDeleted"),
      operationKey: `deleteServiceAccountToken-${tokenId}`,
      onSuccess: async () => {
        await loadTokens(accountId, true);
      },
    });
  }

  async function loadTeams(accountId: string, forceReload = false) {
    if (!forceReload && state.data.value.teamsByAccount[accountId]) {
      return { success: true, data: state.data.value.teamsByAccount[accountId] };
    }
    return await state.callApi({
      apiCall: () => serviceAccountsApi.listTeams(accountId),
      operationKey: `loadServiceAccountTeams-${accountId}`,
      showToast: false,
      onSuccess: (response) => {
        state.data.value.teamsByAccount[accountId] = (response as UserTeamMembership[]) || [];
      },
    });
  }

  async function addToTeam(accountId: string, data: AddServiceAccountToTeamRequest) {
    return await state.callApi({
      apiCall: () => serviceAccountsApi.addToTeam(accountId, data),
      successMessage: i18n.global.t("access.serviceAccountAddedToTeam"),
      operationKey: `addServiceAccountToTeam-${accountId}`,
      onSuccess: async () => {
        await loadTeams(accountId, true);
      },
    });
  }

  async function removeFromTeam(accountId: string, teamId: number) {
    return await state.callApi({
      apiCall: () => serviceAccountsApi.removeFromTeam(accountId, teamId),
      successMessage: i18n.global.t("access.serviceAccountRemovedFromTeam"),
      operationKey: `removeServiceAccountFromTeam-${accountId}-${teamId}`,
      onSuccess: async () => {
        await loadTeams(accountId, true);
      },
    });
  }

  return {
    accounts,
    tokensByAccount,
    teamsByAccount,
    isLoading: state.isLoading,
    error: state.error,
    isLoadingOperation: state.isLoadingOperation,
    loadAccounts,
    createAccount,
    deleteAccount,
    loadTokens,
    createToken,
    deleteToken,
    loadTeams,
    addToTeam,
    removeFromTeam,
  };
});
