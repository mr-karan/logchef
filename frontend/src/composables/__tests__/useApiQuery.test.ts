import { beforeEach, describe, expect, it, vi } from "vitest";
import { useApiQuery } from "../useApiQuery";
import { showErrorToast } from "@/api/error-handler";

vi.mock("@/api/error-handler", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/error-handler")>();
  return {
    ...actual,
    showErrorToast: vi.fn(),
  };
});

describe("useApiQuery error toasts", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("does not duplicate a toast already shown by an interceptor", async () => {
    const { execute } = useApiQuery();
    const error = {
      status: "error" as const,
      message: "Read-only demo",
      error_type: "DemoModeError",
      _toastShown: true,
    };

    const result = await execute(async () => Promise.reject(error));

    expect(result.success).toBe(false);
    expect(showErrorToast).not.toHaveBeenCalled();
  });

  it("shows an ordinary API error once", async () => {
    const { execute } = useApiQuery();
    const error = {
      status: "error" as const,
      message: "Something failed",
      error_type: "GeneralError",
    };

    const result = await execute(async () => Promise.reject(error));

    expect(result.success).toBe(false);
    expect(showErrorToast).toHaveBeenCalledTimes(1);
    expect(showErrorToast).toHaveBeenCalledWith(error, undefined);
  });
});
