import { describe, expect, it } from "vitest";
import { canManageLink, canRemoveLink, linkKindForAdd, linkKindForMove, showsLinkToggle } from "../collectionLinks";

const shared = { is_personal: false };
const personal = { is_personal: true };
const creator = { id: 7, role: "member" };
const other = { id: 8, role: "member" };
const admin = { id: 9, role: "admin" };
const query = { created_by: 7 };
const orphan = { created_by: null };

describe("collection link policy", () => {
  it("gives owner authority to the creator and to global admins only", () => {
    expect(canManageLink(query, creator)).toBe(true);
    expect(canManageLink(query, { id: "7", role: "member" })).toBe(true);
    expect(canManageLink(query, admin)).toBe(true);
    expect(canManageLink(query, other)).toBe(false);
    expect(canManageLink(orphan, creator)).toBe(false);
    expect(canManageLink(orphan, admin)).toBe(true);
    expect(canManageLink(query, null)).toBe(false);
  });

  it("shows the toggle to the creator or an admin in a shared collection", () => {
    expect(showsLinkToggle(shared, query, creator)).toBe(true);
    expect(showsLinkToggle(shared, query, admin)).toBe(true);
    expect(showsLinkToggle(shared, query, other)).toBe(false);
    expect(showsLinkToggle(personal, query, creator)).toBe(false);
  });

  it("sends managed only when the toggle is on and the caller has authority", () => {
    expect(linkKindForAdd(shared, query, creator, true)).toBe("managed");
    expect(linkKindForAdd(shared, query, admin, true)).toBe("managed");
    expect(linkKindForAdd(shared, query, creator, false)).toBe("pin");
    expect(linkKindForAdd(shared, query, other, true)).toBe("pin");
    expect(linkKindForAdd(personal, query, creator, true)).toBe("pin");
  });

  it("keeps the kind on a move except into a personal collection", () => {
    expect(linkKindForMove("managed", shared)).toBe("managed");
    expect(linkKindForMove("pin", shared)).toBe("pin");
    expect(linkKindForMove("managed", personal)).toBe("pin");
  });

  it("lets a participant remove a pin and only an owner remove a managed link", () => {
    expect(canRemoveLink({ kind: "pin", query }, other, true)).toBe(true);
    expect(canRemoveLink({ kind: "managed", query }, other, true)).toBe(false);
    expect(canRemoveLink({ kind: "managed", query }, creator, true)).toBe(true);
    expect(canRemoveLink({ kind: "managed", query }, admin, true)).toBe(true);
    expect(canRemoveLink({ kind: "pin", query }, other, false)).toBe(false);
  });
});
