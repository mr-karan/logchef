import { describe, it, expect } from "vitest";
import { isAbortError } from "../error-handler";

describe("isAbortError", () => {
  it("accepts a DOM-style AbortError", () => {
    const err = new Error("aborted");
    err.name = "AbortError";
    expect(isAbortError(err)).toBe(true);
  });

  it("accepts a CanceledError payload", () => {
    expect(isAbortError({ error_type: "CanceledError", message: "canceled" })).toBe(true);
  });

  it("accepts a silent-flagged error", () => {
    expect(isAbortError({ _silent: true })).toBe(true);
  });

  it.each([new Error("boom"), { error_type: "ServerError" }, null, undefined, "AbortError"])(
    "rejects %j",
    (err) => {
      expect(isAbortError(err)).toBe(false);
    },
  );
});
