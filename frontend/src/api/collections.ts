import { apiClient } from "./apiUtils";
import type { SavedQuery } from "./savedQueries";

export type CollectionRole = "owner" | "editor" | "member";

export interface Collection {
  id: number;
  name: string;
  description?: string;
  is_personal: boolean;
  created_by: number;
  caller_role?: CollectionRole;
  member_count: number;
  item_count: number;
  created_at: string;
  updated_at: string;
}

export interface CollectionMember {
  collection_id: number;
  user_id: number;
  role: CollectionRole;
  added_by?: number | null;
  created_at: string;
  email?: string;
  full_name?: string;
}

export interface CollectionItem {
  collection_id: number;
  sort_order: number;
  added_by?: number | null;
  item_added_at: string;
  query: SavedQuery;
  runnable: boolean;
}

export interface CreateCollectionRequest {
  name: string;
  description?: string;
}

export interface UpdateCollectionRequest {
  name: string;
  description?: string;
}

export interface AddCollectionMemberRequest {
  user_id: number;
  role: CollectionRole;
}

export interface AddCollectionItemRequest {
  saved_query_id: number;
  sort_order?: number;
}

export const collectionsApi = {
  list: () => apiClient.get<Collection[]>("/collections"),
  get: (id: number) => apiClient.get<Collection>(`/collections/${id}`),
  create: (payload: CreateCollectionRequest) =>
    apiClient.post<Collection>("/collections", payload),
  update: (id: number, payload: UpdateCollectionRequest) =>
    apiClient.put<Collection>(`/collections/${id}`, payload),
  delete: (id: number) =>
    apiClient.delete<{ message: string }>(`/collections/${id}`),

  listMembers: (id: number) =>
    apiClient.get<CollectionMember[]>(`/collections/${id}/members`),
  addMember: (id: number, payload: AddCollectionMemberRequest) =>
    apiClient.post<{ message: string }>(`/collections/${id}/members`, payload),
  removeMember: (id: number, userId: number) =>
    apiClient.delete<{ message: string }>(`/collections/${id}/members/${userId}`),

  listItems: (id: number) =>
    apiClient.get<CollectionItem[]>(`/collections/${id}/items`),
  addItem: (id: number, payload: AddCollectionItemRequest) =>
    apiClient.post<{ message: string }>(`/collections/${id}/items`, payload),
  removeItem: (id: number, queryId: number) =>
    apiClient.delete<{ message: string }>(`/collections/${id}/items/${queryId}`),
};
