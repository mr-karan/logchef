import { describe, it, expect } from "vitest";
import { unwrap } from "../types";

describe("unwrap", () => {
  it("returns the data of a success response", () => {
    expect(unwrap({ status: "success", data: { n: 1 } })).toEqual({ n: 1 });
  });

  it("throws an error response as-is", () => {
    const resp = { status: "error" as const, message: "bad", error_type: "ValidationError" };
    let thrown: unknown;
    try {
      unwrap(resp);
    } catch (e) {
      thrown = e;
    }
    expect(thrown).toBe(resp);
  });
});
