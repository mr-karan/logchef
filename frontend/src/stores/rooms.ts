import { defineStore } from "pinia";
import { computed } from "vue";
import { useBaseStore } from "./base";
import {
  roomsApi,
  type RoomSummary,
  type RoomMemberDetail,
  type RoomChannel,
  type CreateRoomRequest,
  type UpdateRoomRequest,
  type AddRoomMemberRequest,
  type CreateRoomChannelRequest,
  type UpdateRoomChannelRequest
} from "@/api/rooms";

interface RoomsState {
  rooms: RoomSummary[];
  membersByRoom: Record<number, RoomMemberDetail[]>;
  channelsByRoom: Record<number, RoomChannel[]>;
}

export const useRoomsStore = defineStore("rooms", () => {
  const state = useBaseStore<RoomsState>({
    rooms: [],
    membersByRoom: {},
    channelsByRoom: {},
  });

  const rooms = computed(() => state.data.value.rooms);
  const membersByRoom = computed(() => state.data.value.membersByRoom);
  const channelsByRoom = computed(() => state.data.value.channelsByRoom);

  async function fetchRooms(teamId: number) {
    return await state.withLoading(`fetchRooms-${teamId}`, async () => {
      return await state.callApi<RoomSummary[]>({
        apiCall: () => roomsApi.listRooms(teamId),
        operationKey: `fetchRooms-${teamId}`,
        onSuccess: (response) => {
          state.data.value.rooms = response ?? [];
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function createRoom(teamId: number, payload: CreateRoomRequest) {
    return await state.withLoading(`createRoom-${teamId}`, async () => {
      return await state.callApi({
        apiCall: () => roomsApi.createRoom(teamId, payload),
        operationKey: `createRoom-${teamId}`,
        successMessage: "Room created",
        onSuccess: (room) => {
          if (room) {
            state.data.value.rooms.unshift({
              id: room.id,
              name: room.name,
              description: room.description,
              member_count: 0,
              channel_types: [],
            });
          }
        },
      });
    });
  }

  async function updateRoom(teamId: number, roomId: number, payload: UpdateRoomRequest) {
    return await state.withLoading(`updateRoom-${roomId}`, async () => {
      return await state.callApi({
        apiCall: () => roomsApi.updateRoom(teamId, roomId, payload),
        operationKey: `updateRoom-${roomId}`,
        successMessage: "Room updated",
        onSuccess: (room) => {
          if (!room) return;
          state.data.value.rooms = state.data.value.rooms.map((summary) =>
            summary.id === room.id
              ? {
                  ...summary,
                  name: room.name,
                  description: room.description,
                }
              : summary
          );
        },
      });
    });
  }

  async function deleteRoom(teamId: number, roomId: number) {
    return await state.withLoading(`deleteRoom-${roomId}`, async () => {
      return await state.callApi({
        apiCall: () => roomsApi.deleteRoom(teamId, roomId),
        operationKey: `deleteRoom-${roomId}`,
        successMessage: "Room deleted",
        onSuccess: () => {
          state.data.value.rooms = state.data.value.rooms.filter((room) => room.id !== roomId);
          delete state.data.value.membersByRoom[roomId];
          delete state.data.value.channelsByRoom[roomId];
        },
      });
    });
  }

  async function fetchMembers(teamId: number, roomId: number) {
    return await state.withLoading(`fetchRoomMembers-${roomId}`, async () => {
      return await state.callApi<RoomMemberDetail[]>({
        apiCall: () => roomsApi.listMembers(teamId, roomId),
        operationKey: `fetchRoomMembers-${roomId}`,
        onSuccess: (members) => {
          state.data.value.membersByRoom[roomId] = members ?? [];
          const summary = state.data.value.rooms.find((r) => r.id === roomId);
          if (summary) {
            summary.member_count = members?.length ?? 0;
          }
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function addMember(teamId: number, roomId: number, payload: AddRoomMemberRequest) {
    return await state.withLoading(`addRoomMember-${roomId}`, async () => {
      return await state.callApi({
        apiCall: () => roomsApi.addMember(teamId, roomId, payload),
        operationKey: `addRoomMember-${roomId}`,
        successMessage: "Member added",
        onSuccess: () => {
          fetchMembers(teamId, roomId);
        },
      });
    });
  }

  async function removeMember(teamId: number, roomId: number, userId: number) {
    return await state.withLoading(`removeRoomMember-${roomId}-${userId}`, async () => {
      return await state.callApi({
        apiCall: () => roomsApi.removeMember(teamId, roomId, userId),
        operationKey: `removeRoomMember-${roomId}-${userId}`,
        successMessage: "Member removed",
        onSuccess: () => {
          fetchMembers(teamId, roomId);
        },
      });
    });
  }

  async function fetchChannels(teamId: number, roomId: number) {
    return await state.withLoading(`fetchRoomChannels-${roomId}`, async () => {
      return await state.callApi<RoomChannel[]>({
        apiCall: () => roomsApi.listChannels(teamId, roomId),
        operationKey: `fetchRoomChannels-${roomId}`,
        onSuccess: (channels) => {
          state.data.value.channelsByRoom[roomId] = channels ?? [];
          const summary = state.data.value.rooms.find((r) => r.id === roomId);
          if (summary) {
            summary.channel_types = Array.from(new Set((channels ?? []).map((c) => c.type)));
          }
        },
        defaultData: [],
        showToast: false,
      });
    });
  }

  async function createChannel(teamId: number, roomId: number, payload: CreateRoomChannelRequest) {
    return await state.withLoading(`createRoomChannel-${roomId}`, async () => {
      return await state.callApi({
        apiCall: () => roomsApi.createChannel(teamId, roomId, payload),
        operationKey: `createRoomChannel-${roomId}`,
        successMessage: "Channel created",
        onSuccess: () => {
          fetchChannels(teamId, roomId);
        },
      });
    });
  }

  async function updateChannel(teamId: number, roomId: number, channelId: number, payload: UpdateRoomChannelRequest) {
    return await state.withLoading(`updateRoomChannel-${channelId}`, async () => {
      return await state.callApi({
        apiCall: () => roomsApi.updateChannel(teamId, roomId, channelId, payload),
        operationKey: `updateRoomChannel-${channelId}`,
        successMessage: "Channel updated",
        onSuccess: () => {
          fetchChannels(teamId, roomId);
        },
      });
    });
  }

  async function deleteChannel(teamId: number, roomId: number, channelId: number) {
    return await state.withLoading(`deleteRoomChannel-${channelId}`, async () => {
      return await state.callApi({
        apiCall: () => roomsApi.deleteChannel(teamId, roomId, channelId),
        operationKey: `deleteRoomChannel-${channelId}`,
        successMessage: "Channel deleted",
        onSuccess: () => {
          fetchChannels(teamId, roomId);
        },
      });
    });
  }

  return {
    data: state.data,
    error: state.error,
    isLoading: state.isLoading,
    loadingStates: state.loadingStates,
    isLoadingOperation: state.isLoadingOperation,
    rooms,
    membersByRoom,
    channelsByRoom,
    fetchRooms,
    createRoom,
    updateRoom,
    deleteRoom,
    fetchMembers,
    addMember,
    removeMember,
    fetchChannels,
    createChannel,
    updateChannel,
    deleteChannel,
  };
});
