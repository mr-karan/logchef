import { apiClient } from "./apiUtils";
import type { SavedTeamQuery } from "./savedQueries";

export const QUERY_FOLDER_COLORS = [
  "gray",
  "red",
  "orange",
  "amber",
  "yellow",
  "green",
  "teal",
  "cyan",
  "blue",
  "indigo",
  "violet",
  "pink",
] as const;

export type QueryFolderColor = typeof QUERY_FOLDER_COLORS[number];

export interface QueryFolder {
  id: number;
  team_id: number;
  name: string;
  description: string;
  color: QueryFolderColor;
  sort_order: number;
  created_by?: number;
  query_count: number;
  created_at: string;
  updated_at: string;
}

export interface QueryFolderPayload {
  name: string;
  description: string;
  color: QueryFolderColor;
}

export interface QueryFolderBulkPayload {
  add?: number[];
  remove?: number[];
}

export const queryFoldersApi = {
  listFolders: (teamId: number) =>
    apiClient.get<QueryFolder[]>(`/teams/${teamId}/folders`),

  createFolder: (teamId: number, payload: QueryFolderPayload) =>
    apiClient.post<QueryFolder>(`/teams/${teamId}/folders`, payload),

  updateFolder: (teamId: number, folderId: number, payload: QueryFolderPayload) =>
    apiClient.put<QueryFolder>(`/teams/${teamId}/folders/${folderId}`, payload),

  deleteFolder: (teamId: number, folderId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/folders/${folderId}`),

  listFolderCollections: (teamId: number, folderId: number) =>
    apiClient.get<SavedTeamQuery[]>(`/teams/${teamId}/folders/${folderId}/collections`),

  addCollection: (teamId: number, folderId: number, collectionId: number) =>
    apiClient.post<{ message: string }>(`/teams/${teamId}/folders/${folderId}/collections/${collectionId}`, {}),

  removeCollection: (teamId: number, folderId: number, collectionId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/folders/${folderId}/collections/${collectionId}`),

  bulkUpdateCollections: (teamId: number, folderId: number, payload: QueryFolderBulkPayload) =>
    apiClient.post<{ message: string }>(`/teams/${teamId}/folders/${folderId}/collections/bulk`, payload),
};
