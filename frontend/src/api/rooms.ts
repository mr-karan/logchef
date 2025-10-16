import { apiClient } from "./apiUtils";

export type RoomChannelType = "slack" | "webhook";

export interface RoomSummary {
  id: number;
  name: string;
  description?: string;
  member_count: number;
  channel_types: RoomChannelType[];
}

export interface Room {
  id: number;
  team_id: number;
  name: string;
  description?: string;
  created_at: string;
  updated_at: string;
}

export interface RoomMemberDetail {
  room_id: number;
  user_id: number;
  name?: string;
  email?: string;
  role: string;
  added_at: string;
}

export interface RoomChannel {
  id: number;
  room_id: number;
  type: RoomChannelType;
  name?: string;
  config: Record<string, any>;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateRoomRequest {
  name: string;
  description?: string;
}

export interface UpdateRoomRequest {
  name?: string;
  description?: string;
}

export interface AddRoomMemberRequest {
  user_id: number;
  role: string;
}

export interface CreateRoomChannelRequest {
  type: RoomChannelType;
  name?: string;
  config: Record<string, any>;
  enabled?: boolean;
}

export interface UpdateRoomChannelRequest {
  name?: string;
  config?: Record<string, any>;
  enabled?: boolean;
}

export const roomsApi = {
  listRooms: (teamId: number) =>
    apiClient.get<RoomSummary[]>(`/teams/${teamId}/rooms`),
  createRoom: (teamId: number, payload: CreateRoomRequest) =>
    apiClient.post<Room>(`/teams/${teamId}/rooms`, payload),
  updateRoom: (teamId: number, roomId: number, payload: UpdateRoomRequest) =>
    apiClient.put<Room>(`/teams/${teamId}/rooms/${roomId}`, payload),
  deleteRoom: (teamId: number, roomId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/rooms/${roomId}`),

  listMembers: (teamId: number, roomId: number) =>
    apiClient.get<RoomMemberDetail[]>(`/teams/${teamId}/rooms/${roomId}/members`),
  addMember: (teamId: number, roomId: number, payload: AddRoomMemberRequest) =>
    apiClient.post<{ message: string }>(`/teams/${teamId}/rooms/${roomId}/members`, payload),
  removeMember: (teamId: number, roomId: number, userId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/rooms/${roomId}/members/${userId}`),

  listChannels: (teamId: number, roomId: number) =>
    apiClient.get<RoomChannel[]>(`/teams/${teamId}/rooms/${roomId}/channels`),
  createChannel: (teamId: number, roomId: number, payload: CreateRoomChannelRequest) =>
    apiClient.post<RoomChannel>(`/teams/${teamId}/rooms/${roomId}/channels`, payload),
  updateChannel: (teamId: number, roomId: number, channelId: number, payload: UpdateRoomChannelRequest) =>
    apiClient.put<RoomChannel>(`/teams/${teamId}/rooms/${roomId}/channels/${channelId}`, payload),
  deleteChannel: (teamId: number, roomId: number, channelId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/rooms/${roomId}/channels/${channelId}`)
};
