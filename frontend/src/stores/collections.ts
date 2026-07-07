import { defineStore } from "pinia";
import { computed } from "vue";
import {
  collectionsApi,
  type Collection,
  type CollectionItem,
  type CollectionMember,
  type CreateCollectionRequest,
  type UpdateCollectionRequest,
  type AddCollectionMemberRequest,
  type AddCollectionItemRequest,
} from "@/api/collections";
import { useBaseStore } from "./base";

interface CollectionsState {
  collections: Collection[];
  selectedId: number | null;
  members: Record<number, CollectionMember[]>;
  items: Record<number, CollectionItem[]>;
}

export const useCollectionsStore = defineStore("collections", () => {
  const state = useBaseStore<CollectionsState>({
    collections: [],
    selectedId: null,
    members: {},
    items: {},
  });

  const collections = computed(() => state.data.value.collections);
  const selectedCollection = computed(() =>
    collections.value.find((c) => c.id === state.data.value.selectedId) ?? null
  );
  const personalCollection = computed(() =>
    collections.value.find((c) => c.is_personal) ?? null
  );
  const sharedCollections = computed(() =>
    collections.value.filter((c) => !c.is_personal)
  );

  function setSelected(id: number | null) {
    state.data.value.selectedId = id;
  }

  async function fetchCollections() {
    return await state.withLoading("listCollections", async () => {
      return await state.callApi<Collection[]>({
        apiCall: () => collectionsApi.list(),
        operationKey: "listCollections",
        onSuccess: (data) => {
          state.data.value.collections = data ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function createCollection(payload: CreateCollectionRequest) {
    return await state.withLoading("createCollection", async () => {
      return await state.callApi<Collection>({
        apiCall: () => collectionsApi.create(payload),
        operationKey: "createCollection",
        successMessage: "Collection created",
        onSuccess: (data) => {
          if (data) state.data.value.collections.unshift(data);
        },
      });
    });
  }

  async function updateCollection(id: number, payload: UpdateCollectionRequest) {
    return await state.withLoading(`updateCollection-${id}`, async () => {
      return await state.callApi<Collection>({
        apiCall: () => collectionsApi.update(id, payload),
        operationKey: `updateCollection-${id}`,
        successMessage: "Collection updated",
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
    });
  }

  async function deleteCollection(id: number) {
    return await state.withLoading(`deleteCollection-${id}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => collectionsApi.delete(id),
        operationKey: `deleteCollection-${id}`,
        successMessage: "Collection deleted",
        onSuccess: () => {
          state.data.value.collections = state.data.value.collections.filter((c) => c.id !== id);
          delete state.data.value.members[id];
          delete state.data.value.items[id];
          if (state.data.value.selectedId === id) state.data.value.selectedId = null;
        },
      });
    });
  }

  async function fetchMembers(id: number) {
    return await state.withLoading(`listMembers-${id}`, async () => {
      return await state.callApi<CollectionMember[]>({
        apiCall: () => collectionsApi.listMembers(id),
        operationKey: `listMembers-${id}`,
        onSuccess: (data) => {
          state.data.value.members[id] = data ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function addMember(id: number, payload: AddCollectionMemberRequest) {
    return await state.withLoading(`addMember-${id}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => collectionsApi.addMember(id, payload),
        operationKey: `addMember-${id}`,
        successMessage: "Member added",
        onSuccess: async () => {
          await fetchMembers(id);
        },
      });
    });
  }

  async function removeMember(id: number, userId: number) {
    return await state.withLoading(`removeMember-${id}-${userId}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => collectionsApi.removeMember(id, userId),
        operationKey: `removeMember-${id}-${userId}`,
        successMessage: "Member removed",
        onSuccess: async () => {
          await fetchMembers(id);
        },
      });
    });
  }

  async function fetchItems(id: number) {
    return await state.withLoading(`listItems-${id}`, async () => {
      return await state.callApi<CollectionItem[]>({
        apiCall: () => collectionsApi.listItems(id),
        operationKey: `listItems-${id}`,
        onSuccess: (data) => {
          state.data.value.items[id] = data ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function addItem(id: number, payload: AddCollectionItemRequest) {
    return await state.withLoading(`addItem-${id}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => collectionsApi.addItem(id, payload),
        operationKey: `addItem-${id}`,
        successMessage: "Added to collection",
        onSuccess: async () => {
          await fetchItems(id);
          await fetchCollections();
        },
      });
    });
  }

  async function removeItem(id: number, queryId: number) {
    return await state.withLoading(`removeItem-${id}-${queryId}`, async () => {
      return await state.callApi<{ message: string }>({
        apiCall: () => collectionsApi.removeItem(id, queryId),
        operationKey: `removeItem-${id}-${queryId}`,
        successMessage: "Removed from collection",
        onSuccess: async () => {
          await fetchItems(id);
          await fetchCollections();
        },
      });
    });
  }

  return {
    isLoading: state.isLoading,
    error: state.error,
    data: state.data,
    isLoadingOperation: state.isLoadingOperation,

    collections,
    selectedCollection,
    personalCollection,
    sharedCollections,

    setSelected,
    fetchCollections,
    createCollection,
    updateCollection,
    deleteCollection,
    fetchMembers,
    addMember,
    removeMember,
    fetchItems,
    addItem,
    removeItem,
  };
});
