# @ks-agentk/protocol

AgentK API v1 messages, enums, schemas, and service descriptors for Node ESM and browser bundlers.

## Install

```sh
pnpm add @ks-agentk/protocol @bufbuild/protobuf --registry=https://npm.corp.kuaishou.com
```

## Imports

- `@ks-agentk/protocol/api/v1`: API messages, schemas, enums, and `AgentKService`.
- `@ks-agentk/protocol/common/v1`: `Artifact` and `ArtifactSchema`.
- `@ks-agentk/protocol/invocation/v1`: invocation messages, schemas, and enums.

Google standard types and helpers are available from `@bufbuild/protobuf/wkt`.
The package provides ESM JavaScript and TypeScript declarations. It has no root export or transport client.

Each generated file is also available through short and long subpaths:

```ts
import { SessionSchema } from "@ks-agentk/protocol/api/v1/model_pb.js";
import { SessionSchema as GeneratedSessionSchema } from "@ks-agentk/protocol/agentk/api/v1/model_pb.js";
```

Both paths and the aggregate entrypoint reference the same generated objects.
The physical `gen/` directory is not a public package subpath.

For downstream proto generation, provide the core proto sources to Buf and select
only the downstream proto files for generation. Configure Protobuf-ES with:

```yaml
opt:
  - target=ts
  - import_extension=js
  - "rewrite_imports=./agentk/api/**/*_pb.js:@ks-agentk/protocol"
  - "rewrite_imports=./agentk/common/**/*_pb.js:@ks-agentk/protocol"
  - "rewrite_imports=./agentk/invocation/**/*_pb.js:@ks-agentk/protocol"
```

Leave `include_imports` disabled for the downstream plugin. Core types and file
descriptors then import from the package's long subpaths without generating a
second copy. The core proto source revision must match the protocol package used
by the consumer.

## Messages and serialization

```ts
import { create, fromBinary, fromJson, toBinary, toJson } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { SessionSchema, type Session } from "@ks-agentk/protocol/api/v1";

const session: Session = create(SessionSchema, {
  id: "session-1",
  title: "Example",
  createdAt: timestampFromDate(new Date()),
});
const bytes = toBinary(SessionSchema, session);
const decoded = fromBinary(SessionSchema, bytes);
const json = toJson(SessionSchema, decoded);
const restored = fromJson(SessionSchema, json);
```

Message fields use the native Protobuf-ES representation: camelCase names,
`bigint` for int64, and `{ case, value }` unions for oneof fields.
Use the schema-aware JSON functions for Protobuf JSON; the message representation
is not the JSON wire format.

```ts
import type { Event } from "@ks-agentk/protocol/api/v1";

function text(event: Event): string {
  return event.payload.case === "assistantText" ? event.payload.value.text : "";
}
```

## Development

From the SDK repository's `typescript/` directory, with Node >=22, pnpm 11.24.0,
and Buf CLI available. Generation also requires the local Go, Python, and Java
plugins configured in the SDK's root `buf.gen.yaml`:

```sh
pnpm install --frozen-lockfile
pnpm gen
pnpm typecheck
pnpm test
pnpm --filter @ks-agentk/protocol pack --pack-destination /tmp
```

`pnpm gen` runs the SDK's root `buf generate`, updating all configured languages.
Generation reads the SDK's `protocol/` source through its root Buf module.
Generated JavaScript and declarations live in `protocol/gen/` and are versioned.
The public entrypoints live outside that generated directory.
The generator runs locally; no remote generation plugin is used.
