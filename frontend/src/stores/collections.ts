import { defineStore } from "pinia";
import { computed } from "vue";
import {
  collectionsApi,
  type Collection,
  type CollectionItem,
  type CollectionMember,
  type CollectionTeam,
  type CreateCollectionRequest,
  type UpdateCollectionRequest,
  type AddCollectionMemberRequest,
  type AddCollectionTeamRequest,
  type AddCollectionItemRequest,
} from "@/api/collections";
import { useBaseStore } from "./base";
import { i18n } from "@/i18n";

interface CollectionsState {
  collections: Collection[];
  members: Record<number, CollectionMember[]>;
  teams: Record<number, CollectionTeam[]>;
  items: Record<number, CollectionItem[]>;
}

export const useCollectionsStore = defineStore("collections", () => {
  const state = useBaseStore<CollectionsState>({
    collections: [],
    members: {},
    teams: {},
    items: {},
  });

  const collections = computed(() => state.data.value.collections);
  const personalCollection = computed(() =>
    collections.value.find((c) => c.is_personal) ?? null
  );
  const sharedCollections = computed(() =>
    collections.value.filter((c) => !c.is_personal)
  );

  async function fetchCollections() {
    return await state.callApi<Collection[]>({
      apiCall: () => collectionsApi.list(),
      operationKey: "listCollections",
      onSuccess: (data) => {
        state.data.value.collections = data ?? [];
      },
      defaultData: [],
      showToast: false,
    });
  }

  async function createCollection(payload: CreateCollectionRequest) {
    return await state.callApi<Collection>({
      apiCall: () => collectionsApi.create(payload),
      operationKey: "createCollection",
      successMessage: i18n.global.t("library.collectionCreated"),
      onSuccess: (data) => {
        if (data) state.data.value.collections.unshift(data);
      },
    });
  }

  async function updateCollection(id: number, payload: UpdateCollectionRequest) {
    return await state.callApi<Collection>({
      apiCall: () => collectionsApi.update(id, payload),
      operationKey: `updateCollection-${id}`,
      successMessage: i18n.global.t("library.collectionUpdated"),
      onSuccess: (data) => {
        if (!data) return;
        const idx = state.data.value.collections.findIndex((c) => c.id === id);
        if (idx >= 0) {
          state.data.value.collections[idx] = {
            ...state.data.value.collections[idx],
            ...data,
          };
        }
      },
    });
  }

  async function deleteCollection(id: number) {
    return await state.callApi<{ message: string }>({
      apiCall: () => collectionsApi.delete(id),
      operationKey: `deleteCollection-${id}`,
      successMessage: i18n.global.t("library.collectionDeleted"),
      onSuccess: () => {
        state.data.value.collections = state.data.value.collections.filter((c) => c.id !== id);
        delete state.data.value.members[id];
        delete state.data.value.teams[id];
        delete state.data.value.items[id];
      },
    });
  }

  async function fetchMembers(id: number) {
    return await state.callApi<CollectionMember[]>({
      apiCall: () => collectionsApi.listMembers(id),
      operationKey: `listMembers-${id}`,
      onSuccess: (data) => {
        state.data.value.members[id] = data ?? [];
      },
      defaultData: [],
      showToast: false,
    });
  }

  async function addMember(id: number, payload: AddCollectionMemberRequest) {
    return await state.callApi<{ message: string }>({
      apiCall: () => collectionsApi.addMember(id, payload),
      operationKey: `addMember-${id}`,
      successMessage: i18n.global.t("library.memberAdded"),
      onSuccess: async () => {
        await Promise.all([fetchMembers(id), fetchCollections()]);
      },
    });
  }

  async function removeMember(id: number, userId: number) {
    return await state.callApi<{ message: string }>({
      apiCall: () => collectionsApi.removeMember(id, userId),
      operationKey: `removeMember-${id}-${userId}`,
      successMessage: i18n.global.t("library.memberRemoved"),
      onSuccess: async () => {
        await Promise.all([fetchMembers(id), fetchCollections()]);
      },
    });
  }

  async function fetchTeams(id: number) {
    return await state.callApi<CollectionTeam[]>({
      apiCall: () => collectionsApi.listTeams(id),
      operationKey: `listTeams-${id}`,
      onSuccess: (data) => {
        state.data.value.teams[id] = data ?? [];
      },
      defaultData: [],
      showToast: false,
    });
  }

  async function addTeam(id: number, payload: AddCollectionTeamRequest) {
    return await state.callApi<{ message: string }>({
      apiCall: () => collectionsApi.addTeam(id, payload),
      operationKey: `addTeam-${id}`,
      successMessage: i18n.global.t("library.teamAdded"),
      onSuccess: async () => {
        await Promise.all([fetchTeams(id), fetchCollections()]);
      },
    });
  }

  async function removeTeam(id: number, teamId: number) {
    return await state.callApi<{ message: string }>({
      apiCall: () => collectionsApi.removeTeam(id, teamId),
      operationKey: `removeTeam-${id}-${teamId}`,
      successMessage: i18n.global.t("library.teamRemoved"),
      onSuccess: async () => {
        await Promise.all([fetchTeams(id), fetchCollections()]);
      },
    });
  }

  async function fetchItems(id: number) {
    return await state.callApi<CollectionItem[]>({
      apiCall: () => collectionsApi.listItems(id),
      operationKey: `listItems-${id}`,
      onSuccess: (data) => {
        state.data.value.items[id] = data ?? [];
      },
      defaultData: [],
      showToast: false,
    });
  }

  async function addItem(id: number, payload: AddCollectionItemRequest, { notify = true } = {}) {
    return await state.callApi<{ message: string }>({
      apiCall: () => collectionsApi.addItem(id, payload),
      operationKey: `addItem-${id}`,
      successMessage: notify ? i18n.global.t("library.queryAdded") : undefined,
      onSuccess: async () => {
        await fetchItems(id);
        await fetchCollections();
      },
    });
  }

  async function removeItem(id: number, queryId: number, { notify = true } = {}) {
    return await state.callApi<{ message: string }>({
      apiCall: () => collectionsApi.removeItem(id, queryId),
      operationKey: `removeItem-${id}-${queryId}`,
      successMessage: notify ? i18n.global.t("library.queryRemoved") : undefined,
      onSuccess: async () => {
        await fetchItems(id);
        await fetchCollections();
      },
    });
  }

  return {
    isLoading: state.isLoading,
    error: state.error,
    data: state.data,
    isLoadingOperation: state.isLoadingOperation,

    collections,
    personalCollection,
    sharedCollections,

    fetchCollections,
    createCollection,
    updateCollection,
    deleteCollection,
    fetchMembers,
    addMember,
    removeMember,
    fetchTeams,
    addTeam,
    removeTeam,
    fetchItems,
    addItem,
    removeItem,
  };
});
