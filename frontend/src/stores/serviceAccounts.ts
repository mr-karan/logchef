import { defineStore } from "pinia";
import { computed } from "vue";
import type { User } from "@/types";
import { useBaseStore } from "./base";
import { serviceAccountsApi, type CreateServiceAccountRequest } from "@/api/serviceAccounts";
import type { APIToken, CreateAPITokenRequest } from "@/api/apiTokens";

interface ServiceAccountsState {
  accounts: User[];
  tokensByAccount: Record<string, APIToken[]>;
}

export const useServiceAccountsStore = defineStore("serviceAccounts", () => {
  const state = useBaseStore<ServiceAccountsState>({
    accounts: [],
    tokensByAccount: {},
  });

  const accounts = computed(() => state.data.value.accounts || []);
  const tokensByAccount = computed(() => state.data.value.tokensByAccount || {});

  async function loadAccounts(forceReload = false) {
    return await state.withLoading("loadServiceAccounts", async () => {
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
    });
  }

  async function createAccount(data: CreateServiceAccountRequest) {
    return await state.withLoading("createServiceAccount", async () => {
      const result = await state.callApi({
        apiCall: () => serviceAccountsApi.createServiceAccount(data),
        successMessage: "Service account created successfully",
        operationKey: "createServiceAccount",
      });
      if (result.success) {
        await loadAccounts(true);
      }
      return result;
    });
  }

  async function deleteAccount(id: string) {
    return await state.withLoading(`deleteServiceAccount-${id}`, async () => {
      const result = await state.callApi({
        apiCall: () => serviceAccountsApi.deleteServiceAccount(id),
        successMessage: "Service account deleted successfully",
        operationKey: `deleteServiceAccount-${id}`,
      });
      if (result.success) {
        delete state.data.value.tokensByAccount[id];
        await loadAccounts(true);
      }
      return result;
    });
  }

  async function loadTokens(accountId: string, forceReload = false) {
    return await state.withLoading(`loadServiceAccountTokens-${accountId}`, async () => {
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
    });
  }

  async function createToken(accountId: string, data: CreateAPITokenRequest) {
    return await state.withLoading(`createServiceAccountToken-${accountId}`, async () => {
      const result = await state.callApi({
        apiCall: () => serviceAccountsApi.createToken(accountId, data),
        successMessage: "Service token created successfully",
        operationKey: `createServiceAccountToken-${accountId}`,
      });
      if (result.success) {
        await loadTokens(accountId, true);
      }
      return result;
    });
  }

  async function deleteToken(accountId: string, tokenId: number) {
    return await state.withLoading(`deleteServiceAccountToken-${tokenId}`, async () => {
      const result = await state.callApi({
        apiCall: () => serviceAccountsApi.deleteToken(accountId, tokenId),
        successMessage: "Service token deleted successfully",
        operationKey: `deleteServiceAccountToken-${tokenId}`,
      });
      if (result.success) {
        await loadTokens(accountId, true);
      }
      return result;
    });
  }

  return {
    accounts,
    tokensByAccount,
    isLoading: state.isLoading,
    error: state.error,
    isLoadingOperation: state.isLoadingOperation,
    loadAccounts,
    createAccount,
    deleteAccount,
    loadTokens,
    createToken,
    deleteToken,
  };
});
