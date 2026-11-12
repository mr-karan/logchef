import type { Collection, CollectionLinkKind } from "@/api/collections";

export interface LinkCaller {
  id?: number | string | null;
  role?: string | null;
}

export interface LinkedQuery {
  created_by?: number | null;
}

type PersonalFlag = Pick<Collection, "is_personal">;

/**
 * Owner authority over a saved query: its creator or a global admin. Only
 * they create, convert or remove a managed link. The server enforces the
 * same rule; this only decides what the UI offers.
 */
export function canManageLink(query: LinkedQuery, user: LinkCaller | null | undefined): boolean {
  if (!user) return false;
  if (user.role === "admin") return true;
  return query.created_by != null && user.id != null && String(query.created_by) === String(user.id);
}

/** Whether the add flow shows the "Collection editors can edit" toggle. */
export function showsLinkToggle(collection: PersonalFlag, query: LinkedQuery, user: LinkCaller | null | undefined): boolean {
  return !collection.is_personal && canManageLink(query, user);
}

/**
 * The kind to send when adding query to collection. The toggle only applies
 * where the caller has owner authority in a shared collection; everything
 * else is a pin.
 */
export function linkKindForAdd(
  collection: PersonalFlag,
  query: LinkedQuery,
  user: LinkCaller | null | undefined,
  editorsCanEdit: boolean,
): CollectionLinkKind {
  if (!editorsCanEdit || !showsLinkToggle(collection, query, user)) return "pin";
  return "managed";
}

/** A move keeps the source link's kind. A personal target takes pins only. */
export function linkKindForMove(sourceKind: CollectionLinkKind, target: PersonalFlag): CollectionLinkKind {
  return target.is_personal ? "pin" : sourceKind;
}

/**
 * Whether the caller may remove a link: any participant removes a pin, a
 * managed link needs owner authority over the query.
 */
export function canRemoveLink(
  item: { kind: CollectionLinkKind; query: LinkedQuery },
  user: LinkCaller | null | undefined,
  participates: boolean,
): boolean {
  if (!participates) return false;
  return item.kind === "pin" || canManageLink(item.query, user);
}
