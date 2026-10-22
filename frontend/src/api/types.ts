export interface APISuccessResponse<T> {
  status: "success";
  data: T | null;
}

export interface APIErrorResponse {
  status: "error";
  message: string;
  error_type: string;
  data?: any;
  _toastShown?: boolean;
}

export type APIResponse<T = unknown> = APISuccessResponse<T> | APIErrorResponse;

export function isSuccessResponse<T>(
  response: APIResponse<T>
): response is APISuccessResponse<T> {
  return response.status === "success";
}

export function unwrap<T>(resp: APIResponse<T>): T {
  if (isSuccessResponse(resp)) {
    return resp.data as T;
  }
  // Non-2xx responses reject before this; a 200 with status:"error" lands here.
  throw resp;
}

// Import from our new error handler utility
import { formatErrorMessage } from "@/api/error-handler";

export function getErrorMessage(error: unknown): string {
  return formatErrorMessage(error);
}
