import assert from 'node:assert/strict';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { createServer, type Server, type Socket } from 'node:net';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { createAgentKEndpointResolver } from './kess.js';

const SERVICE_CACHE_PATH = path.join(
  'central/cache/data2',
  'D__INTERNAL/D_scheduler/D_klingai-agentk-api/I_content',
);

async function listen(server: Server): Promise<number> {
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  return address.port;
}

async function close(server: Server): Promise<void> {
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
}

function reply(socket: Socket, code = 0): void {
  const payload = code === 0 ? Buffer.alloc(0) : Buffer.from([0x08, code]);
  socket.end(Buffer.from([0xff, 0x00, 0x00, payload.length, ...payload]));
}

test('resolves AgentK from a direct KESS Agent subscription and main route file', async () => {
  let requestFrame = Buffer.alloc(0);
  const agent = createServer((socket) => {
    socket.on('data', (chunk) => {
      requestFrame = Buffer.concat([
        requestFrame,
        typeof chunk === 'string' ? Buffer.from(chunk) : chunk,
      ]);
      if (requestFrame.length >= 4 + requestFrame.readUInt16BE(2)) reply(socket);
    });
  });
  const port = await listen(agent);
  const basePath = await mkdtemp(path.join(os.tmpdir(), 'agentk-kess-'));
  const rootLoc = path.join(basePath, 'main');
  try {
    await mkdir(path.join(basePath, 'kess/config2/meta'), { recursive: true });
    await writeFile(
      path.join(basePath, 'kess/config2/meta/kess-config.conf'),
      JSON.stringify({ rootLoc, localDivision: 'central', controlPort: port }),
    );
    const routePath = path.join(rootLoc, SERVICE_CACHE_PATH);
    await mkdir(path.dirname(routePath), { recursive: true });
    await writeFile(
      routePath,
      `metadata\n${JSON.stringify({
        items: {
          'routeTable.instance-PRODUCTION': {
            content: JSON.stringify({
              original: {
                first: { host: '10.0.0.1', weight: 1, entries: { grpc: 8080 } },
                second: { host: '2001:db8::1', weight: 3, entries: { grpc: 9090 } },
              },
            }),
          },
        },
      })}`,
    );

    const resolve = createAgentKEndpointResolver({
      basePath,
      agentHost: '127.0.0.1',
      random: () => 0.5,
    });
    assert.equal((await resolve()).href, 'http://[2001:db8::1]:9090/');
    assert.equal(requestFrame[0], 0xff);
    assert.equal(requestFrame[1], 0x00);
    assert.equal(requestFrame.readUInt16BE(2), requestFrame.length - 4);
    assert.ok(requestFrame.includes(Buffer.from('/_INTERNAL/source')));
    assert.ok(requestFrame.includes(Buffer.from('/_INTERNAL/scheduler/klingai-agentk-api')));
    assert.deepEqual([...requestFrame.subarray(-2)], [0x78, 0x0b]);
  } finally {
    await close(agent);
    await rm(basePath, { recursive: true, force: true });
  }
});

test('does not read a route when KESS Agent rejects the subscription', async () => {
  const agent = createServer((socket) => socket.once('data', () => reply(socket, 3)));
  const port = await listen(agent);
  const basePath = await mkdtemp(path.join(os.tmpdir(), 'agentk-kess-'));
  try {
    await mkdir(path.join(basePath, 'kess/config2/meta'), { recursive: true });
    await writeFile(
      path.join(basePath, 'kess/config2/meta/kess-config.conf'),
      JSON.stringify({ rootLoc: path.join(basePath, 'main'), localDivision: 'central', controlPort: port }),
    );
    await assert.rejects(
      createAgentKEndpointResolver({ basePath, agentHost: '127.0.0.1' })(),
      /rejected subscription with code 3/u,
    );
  } finally {
    await close(agent);
    await rm(basePath, { recursive: true, force: true });
  }
});
