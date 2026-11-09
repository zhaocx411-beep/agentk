# Public Capability Boundary

The Capability Lab is a public console for AgentK Core capabilities, not a
catalog of every capability registered in staging.

- Exclude every capability whose source is `CAPABILITY_SOURCE_CONTRIB`.
- Exclude `agentk.generate_brand_ad`, which belongs to Contrib even though the
  current AgentK catalog labels it as Core.
- Continue excluding `agentk.generate_speech_cosyvoice` by product decision.
- Keep Agent Loop separate from Capability Lab: it starts an execution and
  exposes progress through an SSE `snapshot` stream.
- Keep Narrato separate from both: it starts an asynchronous pipeline execution
  and polls its execution endpoint for progress and output.

Filtering remains server-side so hidden capabilities cannot reappear through a
different front-end rendering path.
