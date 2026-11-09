import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';

import express from 'express';

import { createCapabilityHandler } from './capabilities.js';

const KWS_SERVICE_NAME = 'agentk.sdk.fixture/原样';
const KWS_BINARY_FIXTURE = 'ChlhZ2VudGsuc2RrLmZpeHR1cmUv5Y6f5qC3';

async function withHandler(
  fetchImplementation: typeof fetch,
  run: (origin: string) => Promise<void>,
  identity: { serviceName: string; credential?: string } = {
    serviceName: KWS_SERVICE_NAME,
    credential: 'ksn-credential',
  },
): Promise<void> {
  const app = express();
  app.post(
    '/_agentk/capabilities',
    express.json({ limit: '8kb' }),
    createCapabilityHandler({
      kwsServiceName: identity.serviceName,
      ...(identity.credential === undefined
        ? {}
        : { kwsServiceCredential: identity.credential }),
      fetch: fetchImplementation,
      resolveAgentKEndpoint: async () => new URL('http://agentk.internal/'),
    }),
  );
  const server = createServer(app);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  try {
    await run(`http://127.0.0.1:${address.port}`);
  } finally {
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
  }
}

test('Catalog forwards only controlled JSON and KSN identity', async () => {
  let upstreamRequest: Request | undefined;
  const fakeFetch: typeof fetch = async (input, init) => {
    upstreamRequest = new Request(input, init);
    return Response.json({ capabilities: [] });
  };
  await withHandler(fakeFetch, async (origin) => {
    const response = await fetch(`${origin}/_agentk/capabilities`, {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        cookie: 'accessproxy_session=secret',
        authorization: 'Bearer browser-token',
        'x-ksap-authn-token': 'ap-token',
      },
      body: JSON.stringify({ pageSize: 100, pageToken: 'next' }),
    });
    assert.equal(response.status, 200);
    assert.ok(upstreamRequest);
    assert.equal(
      upstreamRequest.url,
      'http://agentk.internal/agentk.api.v1.AgentKService/ListCapabilities',
    );
    assert.equal(upstreamRequest.headers.get('kws-bin'), KWS_BINARY_FIXTURE);
    assert.equal(upstreamRequest.headers.get('kwsCredential'), 'ksn-credential');
    assert.equal(upstreamRequest.headers.get('cookie'), null);
    assert.equal(upstreamRequest.headers.get('authorization'), null);
    assert.equal(upstreamRequest.headers.get('x-ksap-authn-token'), null);
    assert.deepEqual(await upstreamRequest.json(), {
      pageSize: 100,
      pageToken: 'next',
    });
  });
});

test('Catalog forwards declared KSN without a credential', async () => {
  let upstreamRequest: Request | undefined;
  await withHandler(
    async (input, init) => {
      upstreamRequest = new Request(input, init);
      return Response.json({ capabilities: [] });
    },
    async (origin) => {
      const response = await fetch(`${origin}/_agentk/capabilities`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ pageSize: 10 }),
      });
      assert.equal(response.status, 200);
      assert.ok(upstreamRequest);
      assert.equal(upstreamRequest.headers.get('kws-bin'), KWS_BINARY_FIXTURE);
      assert.equal(upstreamRequest.headers.get('kwsCredential'), null);
    },
    { serviceName: KWS_SERVICE_NAME },
  );
});

test('Catalog encodes protobuf lengths beyond one byte', async () => {
  let upstreamRequest: Request | undefined;
  const serviceName = 'x'.repeat(128);
  await withHandler(
    async (input, init) => {
      upstreamRequest = new Request(input, init);
      return Response.json({ capabilities: [] });
    },
    async (origin) => {
      await fetch(`${origin}/_agentk/capabilities`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ pageSize: 10 }),
      });
      assert.ok(upstreamRequest);
      const kwsBinary = upstreamRequest.headers.get('kws-bin');
      assert.ok(kwsBinary);
      const protobuf = Buffer.from(kwsBinary, 'base64');
      assert.deepEqual([...protobuf.subarray(0, 3)], [0x0a, 0x80, 0x01]);
      assert.equal(protobuf.subarray(3).toString('utf8'), serviceName);
    },
    { serviceName },
  );
});

test('Catalog rejects unknown fields and normalizes upstream failures', async () => {
  await withHandler(
    async () => new Response('not-json', { status: 200 }),
    async (origin) => {
      const invalid = await fetch(`${origin}/_agentk/capabilities`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ pageSize: 10, extra: true }),
      });
      assert.equal(invalid.status, 400);
      const badUpstream = await fetch(`${origin}/_agentk/capabilities`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ pageSize: 10 }),
      });
      assert.equal(badUpstream.status, 502);
      assert.deepEqual(await badUpstream.json(), {
        error: 'AgentK returned an invalid response',
      });
    },
  );
});
