import { describe, expect, it } from "vitest";
import { isVNode, ref } from "vue";
import { formatLogContent, getStandardSeverityClasses, valueUpdater } from "@/lib/utils";

describe("log formatting", () => {
  it("preserves plain and empty content", () => {
    expect(formatLogContent("plain content")).toEqual(["plain content"]);
    expect(formatLogContent("")).toEqual([]);
  });

  it("styles HTTP methods, timestamps, and status columns", () => {
    const [method, separator, timestamp] = formatLogContent("GET 2026-07-14T10:00:00Z");
    expect(isVNode(method) && method.props?.class).toBe("http-method http-method-get");
    expect(separator).toBe(" ");
    expect(isVNode(timestamp) && timestamp.props?.class).toBe("timestamp");
    expect(formatLogContent("200")).toEqual(["200"]);
    const [status] = formatLogContent("200", true);
    expect(isVNode(status) && status.props?.class).toBe("status-code status-success");
  });

  it("matches standard severity names without case sensitivity", () => {
    expect(getStandardSeverityClasses("ERROR")).toBe(getStandardSeverityClasses("error"));
    expect(getStandardSeverityClasses("unknown")).toBeUndefined();
  });
});

it("updates table state from a value or a function", () => {
  const state = ref(1);
  expect(valueUpdater(2, state)).toBe(2);
  expect(valueUpdater(value => value + 1, state)).toBe(3);
  expect(state.value).toBe(3);
});
