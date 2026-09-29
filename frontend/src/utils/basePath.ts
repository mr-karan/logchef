/**
 * Path the UI is served under, with a trailing slash: "/" or a subpath such as
 * "/logchef/". The server sets it through `<base href>` in index.html, from
 * the path of `server.frontend_url`, so the UI works behind a reverse proxy on
 * a subpath.
 */
export const basePath = new URL(document.baseURI).pathname;
