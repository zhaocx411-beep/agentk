import type { RequestHandler } from 'express';
import { z } from 'zod/v4';

import {
  resolveAgentKEndpoint,
  type AgentKEndpointResolver,
} from './kess.js';
import { field } from './protobuf.js';

const REQUEST_LIMIT_BYTES = 8 * 1024;
const RESPONSE_LIMIT_BYTES = 2 * 1024 * 1024;
const CAPABILITIES_PATH =
  '/agentk.api.v1.AgentKService/ListCapabilities';

// KWS metadata is a protobuf message whose field 1 carries the service name.
function encodeKwsBinaryMetadata(serviceName: string): string {
  return field(1, Buffer.from(serviceName, 'utf8'))
    .toString('base64')
    .replace(/=+$/u, '');
}

const listCapabilitiesRequestSchema = z
  .object({
    pageSize: z.int().min(1).max(100).optional(),
    pageToken: z.string().max(2_048).optional(),
  })
  .strict();

async function readResponseWithLimit(response: Response): Promise<Buffer> {
  if (response.body === null) {
    return Buffer.alloc(0);
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > RESPONSE_LIMIT_BYTES) {
      await reader.cancel();
      throw new Error('AgentK response exceeds the configured limit');
    }
    chunks.push(value);
  }
  return Buffer.concat(chunks, size);
}

export function createCapabilityHandler(options: {
  kwsServiceName: string;
  kwsServiceCredential?: string;
  fetch?: typeof fetch;
  resolveAgentKEndpoint?: AgentKEndpointResolver;
}): RequestHandler {
  const fetchImplementation = options.fetch ?? fetch;
  const endpointResolver = options.resolveAgentKEndpoint ?? resolveAgentKEndpoint;
  const identityHeaders = new Headers({
    'content-type': 'application/json',
    accept: 'application/json',
    'connect-protocol-version': '1',
    'kws-bin': encodeKwsBinaryMetadata(options.kwsServiceName),
  });
  if (options.kwsServiceCredential !== undefined) {
    identityHeaders.set('kwsCredential', options.kwsServiceCredential);
  }
  return async (request, response) => {
    const contentLength = Number(request.header('content-length') ?? '0');
    if (Number.isFinite(contentLength) && contentLength > REQUEST_LIMIT_BYTES) {
      response.status(413).json({ error: 'Capability request is too large' });
      return;
    }
    const listRequest = listCapabilitiesRequestSchema.safeParse(request.body);
    if (!listRequest.success) {
      response.status(400).json({ error: 'Invalid capability list request' });
      return;
    }

    let upstream: Response;
    try {
      const agentkBaseUrl = await endpointResolver();
      upstream = await fetchImplementation(
        new URL(CAPABILITIES_PATH, agentkBaseUrl),
        {
          method: 'POST',
          headers: new Headers(identityHeaders),
          body: JSON.stringify(listRequest.data),
          signal: AbortSignal.timeout(5_000),
        },
      );
    } catch (error) {
      const timedOut =
        error instanceof Error &&
        ['AbortError', 'TimeoutError'].includes(error.name);
      response
        .status(timedOut ? 504 : 502)
        .json({
          error: timedOut
            ? 'AgentK request timed out'
            : 'AgentK request failed',
        });
      return;
    }

    let body: Buffer;
    try {
      body = await readResponseWithLimit(upstream);
      JSON.parse(body.toString('utf8'));
    } catch {
      response.status(502).json({ error: 'AgentK returned an invalid response' });
      return;
    }

    if (!upstream.ok) {
      response.status(502).json({ error: 'AgentK rejected the capability request' });
      return;
    }
    response.status(200).type('application/json').send(body);
  };
}
