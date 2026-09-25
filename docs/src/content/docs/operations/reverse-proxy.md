---
title: Reverse Proxy
description: Run Logchef behind nginx, Caddy, or Traefik, on its own domain or under a subpath such as /logchef
---

Logchef serves the web UI and the API from one HTTP port (`8125` by default).
You can put a reverse proxy in front of it to terminate TLS, and serve it on its
own domain (`https://logs.example.com/`) or under a subpath of an existing domain
(`https://example.com/logchef/`).

## On its own domain

Forward every request to Logchef. No extra Logchef configuration is necessary.

```nginx
server {
  listen 443 ssl;
  server_name logs.example.com;

  location / {
    proxy_pass http://127.0.0.1:8125;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;
  }
}
```

Set `server.frontend_url` to `https://logs.example.com` so that share links use
the public URL.

## Under a subpath

To serve Logchef under a subpath, do these two steps:

1. Tell Logchef its public URL, including the subpath.
2. Configure the proxy to remove the subpath before it forwards the request.

Logchef itself always serves from `/`. When a request comes in, the proxy
changes `/logchef/logs/explore` to `/logs/explore`. Logchef reads the subpath
from `server.frontend_url` and uses it to build the links it sends back to the
browser.

### Configure Logchef

```toml
[server]
# Public URL of the web UI, including the subpath.
frontend_url = "https://example.com/logchef"

[oidc]
# Only when you use OIDC. Register the same URL with your OIDC provider.
redirect_url = "https://example.com/logchef/api/v1/auth/callback"
```

With `frontend_url` set:

- The web UI loads its assets and calls the API under `/logchef/`.
- Login redirects and share links point to `https://example.com/logchef/...`.
- Session cookies use `Path=/logchef`. The browser does not send them to other
  applications on the same domain.

Logchef reads `frontend_url` when it starts. Restart Logchef after you change it.
The value in **Administration → System Settings → Server** takes precedence over
`config.toml` when it is not empty.

For alert notifications, set **Frontend URL** under **Administration → System
Settings → Alerts** to the same URL (`https://example.com/logchef`). Alert links
then open under the subpath.

### nginx

```nginx
location /logchef/ {
  # The trailing slash in proxy_pass removes the /logchef prefix.
  proxy_pass http://127.0.0.1:8125/;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-For $remote_addr;
  proxy_set_header X-Forwarded-Proto $scheme;
}
```

nginx redirects `/logchef` to `/logchef/`.

### Caddy

```text title="Caddyfile"
example.com {
	redir /logchef /logchef/
	# handle_path removes the /logchef prefix.
	handle_path /logchef/* {
		reverse_proxy 127.0.0.1:8125
	}
}
```

### Traefik

```yaml
http:
  routers:
    logchef:
      rule: "Host(`example.com`) && PathPrefix(`/logchef`)"
      middlewares: [logchef-strip-prefix]
      service: logchef
  middlewares:
    logchef-strip-prefix:
      stripPrefix:
        prefixes: ["/logchef"]
  services:
    logchef:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:8125"
```

A `PathPrefix` rule without the `stripPrefix` middleware does not work. Logchef
does not serve routes under `/logchef/`.

### Other proxies

Any proxy works if it removes the subpath before it forwards the request. To
make sure that the proxy is correct, open
`https://example.com/logchef/logs/explore` directly. The login page must load.
A blank page means that the proxy does not remove the subpath.

### CLI

Include the subpath in the server URL:

```bash
logchef auth --server https://example.com/logchef
```

## Live tail

Live tail uses Server-Sent Events. Logchef sends the `X-Accel-Buffering: no`
header, which stops nginx from buffering the stream. It also sends a heartbeat
every 15 seconds, so the default proxy read timeouts do not close an idle stream.
Caddy and Traefik stream the events without extra configuration.

## Client IP

To make the proxy's `X-Forwarded-For` header available for client-IP features such
as per-IP rate limiting, set `server.trusted_proxies` to the proxy's address.
Refer to [Configuration](/getting-started/configuration#server-settings).
