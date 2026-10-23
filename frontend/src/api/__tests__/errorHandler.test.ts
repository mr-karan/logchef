import { describe, it, expect } from "vitest";
import { formatErrorMessage, isAbortError } from "../error-handler";

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

describe("formatErrorMessage", () => {
  it.each([
    [new Error("boom"), "boom"],
    [{ status: "error", message: "bad query", error_type: "ValidationError" }, "bad query"],
    [{ message: "plain object" }, "plain object"],
  ])("returns the message of %j", (err, expected) => {
    expect(formatErrorMessage(err, "fallback")).toBe(expected);
  });

  it.each([new Error(""), { message: 42 }, null, undefined, "boom"])("falls back for %j", (err) => {
    expect(formatErrorMessage(err, "fallback")).toBe("fallback");
  });

  it("uses the default fallback when none is given", () => {
    expect(formatErrorMessage(null)).toBe("An unexpected error occurred");
  });
});
