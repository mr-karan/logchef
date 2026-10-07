// isSafeLocalPath mirrors the server's isSafeLocalPath
// (internal/server/auth_handlers.go): a same-origin absolute path that is not
// protocol-relative ("//host", "/\host") and carries no scheme.
export function isSafeLocalPath(path: unknown): path is string {
  if (typeof path !== "string" || !path.startsWith("/")) return false;
  if (path.startsWith("//") || path.startsWith("/\\")) return false;
  return !path.includes("://");
}

export function safeRedirectPath(path: unknown, fallback: string): string {
  return isSafeLocalPath(path) ? path : fallback;
}
