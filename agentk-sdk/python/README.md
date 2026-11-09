# AgentK Python SDK

Requires Python 3.12 or later. Install from the company Python index.

| Package | Purpose |
|---|---|
| `ks-agentk-client` | Asynchronous AgentK API client over the HTTP gateway |
| `ks-agentk-client[grpc]` | Adds the gRPC client with KESS service discovery |
| `ks-agentk-worker` | Temporal Workers that publish AgentK capabilities |
| `ks-agentk-protocol` | Generated API messages and clients |

## API client

```python
from agentk.api.v1.service_pb import WhoAmIRequest
from agentk.client import Options
from agentk.client.grpc import create_client

async def identify():
    async with create_client(Options()) as client:
        return await client.who_am_i(WhoAmIRequest())
```

`agentk.client.grpc.create_client` discovers `klingai-agentk-api` through KESS
on the first request; `agentk.client.http.create_client` uses the stage's HTTP
gateway. Both accept `Options(endpoint=...)` to call a fixed URL instead. Use a
client on one event loop and close it with `async with` or `aclose()`.

The gRPC client forwards the request lane set with `ksresolve.rpc.with_lane`
in the company trace carrier (`trace_context-bin`); KESS endpoint selection
follows that lane. The caller identity comes from the `KWS_SERVICE_NAME` /
`KWS_SERVICE_CREDENTIAL` environment of the deployment.

## Temporal Worker

```python
from agentk.worker import Worker, connect_temporal

async def main():
    client = await connect_temporal()
    worker = Worker(client, task_queue="my-team-queue", workflows=[...], activities=[...])
    worker.publish_workflow(...)
    await worker.run()
```

`connect_temporal()` returns a native `temporalio.Client` for namespace `agentk`
connected through KESS. A running `Worker` follows Temporal endpoint changes and
announces its published capabilities until it stops. Workflows and activities
run with the trace carrier of the originating request, so `ksresolve` calls made
from an activity keep its lane.

The Worker instance lane contract shared by all SDKs is described in
[`protocol/agentk/worker_lane/v1`](../protocol/agentk/worker_lane/v1/README.md).
