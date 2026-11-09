import { existsSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import { isIP, createConnection } from 'node:net';
import os from 'node:os';
import path from 'node:path';

import { field, varint } from './protobuf.js';

const AGENTK_SERVICE = 'klingai-agentk-api';
const SERVICE_DIR = `/_INTERNAL/scheduler/${AGENTK_SERVICE}`;
const SOURCE_DIR = '/_INTERNAL/source';
const SDK_VERSION = 11;
const SOCKET_TIMEOUT_MS = 3_000;

type KessConfig = {
  rootLoc: string;
  localDivision: string;
  controlPort: number;
};

type KessResolverOptions = {
  basePath?: string;
  agentHost?: string;
  random?: () => number;
};

export type AgentKEndpointResolver = () => Promise<URL>;

function encodeSubscription(division: string): Buffer {
  const subscribe = Buffer.concat([
    field(1, Buffer.from(SOURCE_DIR)),
    field(1, Buffer.from(SERVICE_DIR)),
    field(2, Buffer.from(division)),
  ]);
  return Buffer.concat([field(1, subscribe), varint(15 << 3), varint(SDK_VERSION)]);
}

function decodeResponseCode(payload: Buffer): number {
  if (payload.length === 0) return 0;
  let offset = 0;
  const readVarint = (): number => {
    let value = 0;
    let multiplier = 1;
    while (offset < payload.length) {
      const byte = payload[offset++];
      if (byte === undefined) break;
      value += (byte & 0x7f) * multiplier;
      if ((byte & 0x80) === 0) return value;
      multiplier *= 0x80;
    }
    throw new Error('Invalid response from KESS Agent');
  };
  while (offset < payload.length) {
    const tag = readVarint();
    const fieldNumber = tag >> 3;
    const wireType = tag & 7;
    if (wireType === 0) {
      const value = readVarint();
      if (fieldNumber === 1) return value;
    } else if (wireType === 2) {
      offset += readVarint();
    } else {
      throw new Error('Invalid response from KESS Agent');
    }
  }
  return 0;
}

function subscribe(host: string, port: number, division: string): Promise<void> {
  const payload = encodeSubscription(division);
  const frame = Buffer.concat([
    Buffer.from([0xff, 0x00, payload.length >> 8, payload.length & 0xff]),
    payload,
  ]);
  return new Promise((resolve, reject) => {
    const socket = createConnection({ host, port });
    let response = Buffer.alloc(0);
    const fail = (error: Error): void => {
      socket.destroy();
      reject(error);
    };
    socket.setNoDelay();
    socket.setTimeout(SOCKET_TIMEOUT_MS, () => {
      const error = new Error('KESS Agent request timed out');
      error.name = 'TimeoutError';
      fail(error);
    });
    socket.once('error', reject);
    socket.once('connect', () => socket.write(frame));
    socket.on('data', (chunk) => {
      response = Buffer.concat([
        response,
        typeof chunk === 'string' ? Buffer.from(chunk) : chunk,
      ]);
      if (response.length < 4) return;
      if (response[0] !== 0xff || response[1] !== 0x00) {
        fail(new Error('Invalid response from KESS Agent'));
        return;
      }
      const length = response.readUInt16BE(2);
      if (response.length < length + 4) return;
      const code = decodeResponseCode(response.subarray(4, length + 4));
      socket.destroy();
      if (code === 0) resolve();
      else reject(new Error(`KESS Agent rejected subscription with code ${code}`));
    });
  });
}

function routePath(config: KessConfig): string {
  const encodedDirectory = SERVICE_DIR.split('/')
    .filter(Boolean)
    .map((part) => `D_${part}`);
  return path.join(
    config.rootLoc,
    config.localDivision,
    'cache/data2',
    ...encodedDirectory,
    'I_content',
  );
}

function selectEndpoint(contents: string, random: () => number): URL {
  const newline = contents.indexOf('\n');
  if (newline < 0) throw new Error('Invalid AgentK KESS route file');
  const document = JSON.parse(contents.slice(newline + 1)) as {
    items?: Record<string, { content?: string }>;
  };
  const encodedRoute = document.items?.['routeTable.instance-PRODUCTION']?.content;
  if (typeof encodedRoute !== 'string') {
    throw new Error('AgentK KESS route table is missing');
  }
  const route = JSON.parse(encodedRoute) as {
    original?: Record<string, {
      host?: string;
      weight?: number;
      entries?: { grpc?: number };
    }>;
  };
  const instances = Object.values(route.original ?? {}).filter(
    (instance): instance is { host: string; weight: number; entries: { grpc: number } } =>
      typeof instance.host === 'string' &&
      instance.host.length > 0 &&
      typeof instance.weight === 'number' &&
      instance.weight > 0 &&
      Number.isInteger(instance.entries?.grpc) &&
      (instance.entries?.grpc ?? 0) > 0 &&
      (instance.entries?.grpc ?? 0) <= 65_535,
  );
  const totalWeight = instances.reduce((sum, instance) => sum + instance.weight, 0);
  if (instances.length === 0 || totalWeight <= 0) {
    throw new Error('AgentK has no routable KESS instance');
  }
  let ticket = random() * totalWeight;
  const selected = instances.find((instance) => {
    ticket -= instance.weight;
    return ticket < 0;
  }) ?? instances.at(-1);
  if (selected === undefined) throw new Error('AgentK has no routable KESS instance');
  const host = isIP(selected.host) === 6 ? `[${selected.host}]` : selected.host;
  return new URL(`http://${host}:${selected.entries.grpc}/`);
}

export function createAgentKEndpointResolver(
  options: KessResolverOptions = {},
): AgentKEndpointResolver {
  return async () => {
    const basePath = options.basePath ?? (existsSync('/opt/data') ? '/opt/data' : os.homedir());
    const config = JSON.parse(
      await readFile(path.join(basePath, 'kess/config2/meta/kess-config.conf'), 'utf8'),
    ) as KessConfig;
    if (
      typeof config.rootLoc !== 'string' ||
      typeof config.localDivision !== 'string' ||
      !Number.isInteger(config.controlPort) ||
      config.controlPort < 1 ||
      config.controlPort > 65_535
    ) {
      throw new Error('Invalid KESS client config');
    }
    await subscribe(
      options.agentHost ?? process.env.KS_KUBE_HOST_IP ?? 'localhost',
      config.controlPort,
      config.localDivision,
    );
    return selectEndpoint(
      await readFile(routePath(config), 'utf8'),
      options.random ?? Math.random,
    );
  };
}

export const resolveAgentKEndpoint = createAgentKEndpointResolver();
