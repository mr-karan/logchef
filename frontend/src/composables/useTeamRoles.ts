import { ref } from "vue";
import { useI18n } from "vue-i18n";

export type TeamRole = "admin" | "editor" | "member";

export function isTeamRole(value: unknown): value is TeamRole {
  return value === "admin" || value === "editor" || value === "member";
}

export interface PendingConfirm {
  title: string;
  description: string;
  confirmText: string;
  destructive?: boolean;
  onConfirm: () => Promise<void>;
  onCancel?: () => void;
}

/** State for one ConfirmDialog shared by several guarded actions. */
export function usePendingConfirm() {
  const pending = ref<PendingConfirm | null>(null);
  const open = ref(false);

  function request(config: PendingConfirm) {
    pending.value = config;
    open.value = true;
  }

  async function confirm() {
    const action = pending.value;
    pending.value = null;
    if (action) await action.onConfirm();
  }

  function cancel() {
    const action = pending.value;
    pending.value = null;
    action?.onCancel?.();
  }

  function dismiss() {
    open.value = false;
    pending.value = null;
  }

  return { pending, open, request, confirm, cancel, dismiss };
}

interface MembershipChange {
  memberName: string;
  teamName: string;
  isLastAdmin: boolean;
  managed: boolean;
}

export interface TeamRoleChange extends MembershipChange {
  from: TeamRole;
  to: TeamRole;
  isSelf: boolean;
}

export interface TeamMemberRemoval extends MembershipChange {
  role: TeamRole;
  /** The caller removes themselves and loses access to the team. */
  removesOwnAccess: boolean;
}

type ConfirmText = Omit<PendingConfirm, "onConfirm" | "onCancel">;

export function useTeamRoles() {
  const { t } = useI18n();

  function roleLabel(role: string): string {
    switch (role) {
      case "admin":
        return t("access.roleAdmin");
      case "editor":
        return t("access.roleEditor");
      case "member":
        return t("access.roleMember");
      default:
        return role;
    }
  }

  /** The dialog a role change needs, or null when it can be applied at once. */
  function roleChangeConfirm(change: TeamRoleChange): ConfirmText | null {
    const demoting = change.from === "admin" && change.to !== "admin";
    const team = change.teamName;
    const warnings: string[] = [];
    if (demoting && change.isLastAdmin) {
      warnings.push(t("access.lastTeamAdminDemoteWarning", { name: change.memberName, team }));
    }
    if (demoting && change.isSelf) {
      warnings.push(t("access.selfTeamDemoteWarning", { team }));
    }
    if (change.managed) {
      warnings.push(t("access.managedTeamChangeWarning", { team }));
    }
    if (warnings.length === 0) return null;
    return {
      title: t("access.changeTeamRoleTitle"),
      description: warnings.join(" "),
      confirmText: t("access.changeTeamRoleConfirm"),
      destructive: demoting,
    };
  }

  function removeMemberConfirm(removal: TeamMemberRemoval): ConfirmText {
    const team = removal.teamName;
    const parts = [t("access.removeMemberConfirmation", { name: removal.memberName, team })];
    if (removal.role === "admin" && removal.isLastAdmin) {
      parts.push(t("access.lastTeamAdminRemoveWarning", { name: removal.memberName, team }));
    }
    if (removal.removesOwnAccess) {
      parts.push(t("access.selfTeamRemoveWarning", { team }));
    }
    if (removal.managed) {
      parts.push(t("access.managedTeamChangeWarning", { team }));
    }
    return {
      title: t("access.removeMemberTitle"),
      description: parts.join(" "),
      confirmText: t("access.removeFromTeam"),
      destructive: true,
    };
  }

  return { roleLabel, roleChangeConfirm, removeMemberConfirm };
}
