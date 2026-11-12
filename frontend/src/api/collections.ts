import { apiClient, type RequestOptions } from "./apiUtils";
import type { SavedQuery } from "./savedQueries";

export type CollectionRole = "owner" | "editor" | "member";

export interface Collection {
  id: number;
  name: string;
  description?: string;
  is_personal: boolean;
  created_by: number;
  caller_role?: CollectionRole;
  /** Direct membership rows, owners included. Team shares are counted separately. */
  member_count: number;
  team_count: number;
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

export interface CollectionTeam {
  collection_id: number;
  team_id: number;
  team_name: string;
  added_by?: number | null;
  created_at: string;
}

/**
 * What a link between a collection and a saved query grants. A pin is a
 * bookmark and grants nothing. A managed link delegates edit on the query to
 * the collection's owners and editors; only the query's creator or a global
 * admin creates, converts or removes one.
 */
export type CollectionLinkKind = "pin" | "managed";

export interface CollectionItem {
  collection_id: number;
  kind: CollectionLinkKind;
  sort_order: number;
  added_by?: number | null;
  item_added_at: string;
  query: SavedQuery;
  runnable: boolean;
}

/** One link row, as POST and PUT on the item routes return it. */
export interface CollectionItemLink {
  collection_id: number;
  saved_query_id: number;
  kind: CollectionLinkKind;
  added_by: number | null;
  sort_order: number;
  created_at: string;
}

/**
 * One pin in a shared collection, for the admin report. Right after the
 * link-kind upgrade these are exactly the links that lost delegation.
 */
export interface CollectionLinkReportRow {
  collection_id: number;
  collection_name: string;
  saved_query_id: number;
  saved_query_name: string;
  query_created_by: number | null;
  query_creator_email?: string;
  added_by: number | null;
  added_by_email?: string;
  created_at: string;
  editors: CollectionMember[];
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

export interface AddCollectionTeamRequest {
  team_id: number;
}

export interface AddCollectionItemRequest {
  saved_query_id: number;
  sort_order?: number;
  /** Omitted means a pin. */
  kind?: CollectionLinkKind;
}

export interface UpdateCollectionItemRequest {
  kind: CollectionLinkKind;
}

export const collectionsApi = {
  list: (options?: RequestOptions) => apiClient.get<Collection[]>("/collections", options),
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

  listTeams: (id: number) =>
    apiClient.get<CollectionTeam[]>(`/collections/${id}/teams`),
  addTeam: (id: number, payload: AddCollectionTeamRequest) =>
    apiClient.post<{ message: string }>(`/collections/${id}/teams`, payload),
  removeTeam: (id: number, teamId: number) =>
    apiClient.delete<{ message: string }>(`/collections/${id}/teams/${teamId}`),

  listItems: (id: number) =>
    apiClient.get<CollectionItem[]>(`/collections/${id}/items`),
  addItem: (id: number, payload: AddCollectionItemRequest) =>
    apiClient.post<CollectionItemLink>(`/collections/${id}/items`, payload),
  updateItem: (id: number, queryId: number, payload: UpdateCollectionItemRequest) =>
    apiClient.put<CollectionItemLink>(`/collections/${id}/items/${queryId}`, payload),
  removeItem: (id: number, queryId: number) =>
    apiClient.delete<{ message: string }>(`/collections/${id}/items/${queryId}`),

  /** Global admins only. */
  linkReport: () =>
    apiClient.get<CollectionLinkReportRow[]>("/admin/collection-links/report"),
};
