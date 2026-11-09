# AgentK Docs Service deployment contract

The production entry point is `dist/service.js`. Local unauthenticated MCP entry
points (`dist/http.js` and `dist/stdio.js`) remain development-only.

## Required environment

- `AGENTK_DOCS_PUBLIC_URL`: the complete public Docs URL. It must use HTTPS and
  end with the exact path `/docs/`, for example
  `https://agentk.staging.kuaishou.com/docs/`.
- `KWS_SERVICE_NAME`: the registered Docs workload KSN. Capability Catalog
  requests send it as KWS binary metadata.
- `KSAP_JWKS_URL`: the official AccessProxy JWKS URL from the Node SDK manual.
- `AUTO_PORT0` or `PORT`: optional listener port; defaults to `8080`.

`KWS_SERVICE_CREDENTIAL` is optional at process startup and is sent alongside
the declared KSN when present. AgentK staging may accept the declared KSN by
itself; production AgentK requires a valid credential. The Docs service does not
inspect or branch on the AgentK stage.

The container build also requires `--build-arg AGENTK_DOCS_PUBLIC_URL=...` so
the Docusaurus canonical URLs match the deployed origin.

Capability Catalog resolves the fixed KESS service `klingai-agentk-api` on
every request. The workload must have access to its local KESS Agent control
port and KESS cache (`/opt/data/kess/config2`); `KS_KUBE_HOST_IP` selects the
Agent host when the platform provides it, otherwise `localhost` is used.

## AccessProxy locations

| Path | AccessProxy policy |
|---|---|
| `/docs`, `/docs/*` | APSSO required; preserve the original request path when proxying to Docs Service |
| `/oauth/authorize` | APSSO required; remove any client-provided `x-ksap-authn-token` and inject the verified identity header |
| `/oauth/register`, `/oauth/token`, `/oauth/revoke` | no AP authentication |
| `/.well-known/*` | no AP authentication |
| `/mcp` | no AP authentication or redirect; Docs validates Bearer tokens |
| `/healthz` | no authentication or probe-network-only |

Run a single replica. OAuth clients, codes, access tokens, and refresh tokens
are process memory only, so every restart intentionally requires registration
and login again. Enable AP HTTP/2 where possible and honor
`X-Accel-Buffering: no` for `/mcp` responses.

## Acceptance

1. An unauthenticated `/mcp` request returns the Docs `401` and OAuth metadata,
   never an AP `302`.
2. `/oauth/authorize` redirects to APSSO without a browser session and reuses an
   existing session when present.
3. Codex Desktop, Cursor, Claude Code, and MCP Inspector complete DCR, PKCE,
   token exchange, and all three tools. Every returned page identifier is an
   absolute `AGENTK_DOCS_PUBLIC_URL` page URL that `read_doc` accepts back.
4. Capability Catalog works while AgentK observes the Docs KSN identity and no
   browser Cookie, Authorization, or AP identity header is forwarded.
5. MCP streaming is not buffered by AP.
6. Direct Docs Service requests use the same public paths as AccessProxy:
   `/docs/*` is never rewritten or stripped.

The AgentK API remains on its separate service/domain with its generated RPC
paths. This repository does not configure the gateway, AccessProxy, the old
Docs-domain redirect, or any future `/admin` route. OAuth and MCP clients hard
cut to the paths above and register/login again after deployment.
