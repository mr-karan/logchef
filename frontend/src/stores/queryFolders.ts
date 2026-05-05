import { defineStore } from "pinia";
import { computed } from "vue";
import {
  queryFoldersApi,
  type QueryFolder,
  type QueryFolderPayload,
  type QueryFolderBulkPayload,
} from "@/api/queryFolders";
import type { SavedTeamQuery } from "@/api/savedQueries";
import { useBaseStore } from "./base";

interface QueryFoldersState {
  folders: QueryFolder[];
  folderQueries: SavedTeamQuery[];
}

export const useQueryFoldersStore = defineStore("queryFolders", () => {
  const state = useBaseStore<QueryFoldersState>({
    folders: [],
    folderQueries: [],
  });

  const folders = computed(() => state.data.value.folders);
  const folderQueries = computed(() => state.data.value.folderQueries);

  async function fetchFolders(teamId: number) {
    return await state.withLoading(`queryFolders-${teamId}`, async () => {
      return await state.callApi<QueryFolder[]>({
        apiCall: () => queryFoldersApi.listFolders(teamId),
        operationKey: `queryFolders-${teamId}`,
        onSuccess: (response) => {
          state.data.value.folders = response ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function createFolder(teamId: number, payload: QueryFolderPayload) {
    return await state.withLoading(`createQueryFolder-${teamId}`, async () => {
      return await state.callApi<QueryFolder>({
        apiCall: () => queryFoldersApi.createFolder(teamId, payload),
        operationKey: `createQueryFolder-${teamId}`,
        successMessage: "Folder created successfully",
        onSuccess: (response) => {
          if (response) {
            state.data.value.folders = [...state.data.value.folders, response]
              .sort((a, b) => a.name.localeCompare(b.name));
          }
        },
      });
    });
  }

  async function updateFolder(teamId: number, folderId: number, payload: QueryFolderPayload) {
    return await state.withLoading(`updateQueryFolder-${teamId}-${folderId}`, async () => {
      return await state.callApi<QueryFolder>({
        apiCall: () => queryFoldersApi.updateFolder(teamId, folderId, payload),
        operationKey: `updateQueryFolder-${teamId}-${folderId}`,
        successMessage: "Folder updated successfully",
        onSuccess: (response) => {
          if (!response) return;
          const index = state.data.value.folders.findIndex((folder) => folder.id === folderId);
          if (index >= 0) {
            state.data.value.folders[index] = response;
          }
        },
      });
    });
  }

  async function deleteFolder(teamId: number, folderId: number) {
    return await state.withLoading(`deleteQueryFolder-${teamId}-${folderId}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => queryFoldersApi.deleteFolder(teamId, folderId),
        operationKey: `deleteQueryFolder-${teamId}-${folderId}`,
        successMessage: "Folder deleted successfully",
        onSuccess: () => {
          state.data.value.folders = state.data.value.folders.filter((folder) => folder.id !== folderId);
          state.data.value.folderQueries = [];
        },
      });
    });
  }

  async function fetchFolderCollections(teamId: number, folderId: number) {
    return await state.withLoading(`queryFolderCollections-${teamId}-${folderId}`, async () => {
      return await state.callApi<SavedTeamQuery[]>({
        apiCall: () => queryFoldersApi.listFolderCollections(teamId, folderId),
        operationKey: `queryFolderCollections-${teamId}-${folderId}`,
        onSuccess: (response) => {
          state.data.value.folderQueries = response ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function bulkUpdateCollections(teamId: number, folderId: number, payload: QueryFolderBulkPayload) {
    return await state.withLoading(`bulkQueryFolderCollections-${teamId}-${folderId}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => queryFoldersApi.bulkUpdateCollections(teamId, folderId, payload),
        operationKey: `bulkQueryFolderCollections-${teamId}-${folderId}`,
        successMessage: "Folder collections updated",
      });
    });
  }

  function resetFolderQueries() {
    state.data.value.folderQueries = [];
  }

  function resetFolders() {
    state.data.value.folders = [];
    state.data.value.folderQueries = [];
  }

  return {
    isLoading: state.isLoading,
    error: state.error,
    data: state.data.value,
    folders,
    folderQueries,
    fetchFolders,
    createFolder,
    updateFolder,
    deleteFolder,
    fetchFolderCollections,
    bulkUpdateCollections,
    resetFolderQueries,
    resetFolders,
    isLoadingOperation: state.isLoadingOperation,
  };
});
