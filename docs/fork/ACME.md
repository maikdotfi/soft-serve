# Automatic TLS (Let's Encrypt)

The HTTP server can obtain and renew its own certificate from Let's Encrypt, the way Caddy does. Git over HTTPS, LFS, and `/ui` all share that one TLS listener.

## Requirements

- A DNS `A`/`AAAA` record for your hostname pointing at the server.
- **Port 443 reachable from the internet.** Certificates are validated with the TLS-ALPN-01 challenge, which Let's Encrypt only performs on port 443. Either bind soft-serve to `:443`, or keep `:23232` and forward 443 → 23232 at your firewall/router.

## Configuration

```yaml
http:
  listen_addr: ":443"
  public_url: "https://git.example.com"
  acme:
    enabled: true
    domains: ["git.example.com"]
    email: "you@example.com"   # optional
    # ca_url: "https://acme-staging-v02.api.letsencrypt.org/directory"
    # cache_path: "acme"        # relative to the data path
```

Or with environment variables:

| Variable | Meaning |
| --- | --- |
| `SOFT_SERVE_HTTP_ACME_ENABLED` | `true` to enable |
| `SOFT_SERVE_HTTP_ACME_DOMAINS` | comma-separated hostnames |
| `SOFT_SERVE_HTTP_ACME_EMAIL` | optional ACME account contact |
| `SOFT_SERVE_HTTP_ACME_CA_URL` | ACME directory; default Let's Encrypt production |
| `SOFT_SERVE_HTTP_ACME_CACHE_PATH` | cert/account storage; default `$SOFT_SERVE_DATA_PATH/acme` |

The first certificate is obtained on the first TLS connection for the hostname and cached, then renewed automatically before it expires. Enabling ACME accepts the CA's terms of service.

ACME can't be combined with `tls_cert_path`/`tls_key_path`. Wildcards and IP addresses are rejected because TLS-ALPN-01 can't validate them. Point `ca_url` at the Let's Encrypt staging directory while testing, so you don't hit production rate limits.

## systemd

To bind `:443` as the unprivileged `soft-serve` user, uncomment `AmbientCapabilities=CAP_NET_BIND_SERVICE` in `deploy/soft-serve.service`. `deploy/soft-serve.conf` has a commented example.

## Code layout

- `pkg/acme`: domain config validation, host policy, the `Cache` port and its sentinel errors.
- `pkg/acme/acmetest`: the `Cache` contract suite.
- `pkg/acme/adapters/fake`, `pkg/acme/adapters/dircache`: `Cache` adapters (in-memory, on-disk).
- `pkg/acme/adapters/autocert`: the ACME adapter over `golang.org/x/crypto/acme/autocert`.
- `cmd/acme.go`: `WireACME`, called from a `// fork` hook in `cmd/soft/serve/server.go`.
