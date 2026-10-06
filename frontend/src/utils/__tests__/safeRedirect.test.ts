import { describe, expect, it } from "vitest";
import { isSafeLocalPath, safeRedirectPath } from "../safeRedirect";

describe("isSafeLocalPath", () => {
  it.each([
    "/logs/explore",
    "/oauth/consent?request=abc",
    "/",
  ])("accepts %s", (path) => {
    expect(isSafeLocalPath(path)).toBe(true);
  });

  it.each([
    "",
    "logs",
    "//evil.example",
    "/\\evil.example",
    "https://evil.example",
    "/redirect?to=https://evil.example",
    "javascript:alert(1)",
    undefined,
    null,
    ["/logs"],
  ])("rejects %s", (path) => {
    expect(isSafeLocalPath(path)).toBe(false);
  });

  it("falls back for unsafe paths", () => {
    expect(safeRedirectPath("//evil.example", "/logs/explore")).toBe("/logs/explore");
    expect(safeRedirectPath("/settings", "/logs/explore")).toBe("/settings");
  });
});
