import {execFile} from 'node:child_process';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';

const run = promisify(execFile);
const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const outputDir = path.join(rootDir, 'api-reference', 'python');

const pythonStandardLibraryLinks = {
  'typing.Any': 'https://docs.python.org/3/library/typing.html#typing.Any',
  'typing.AsyncIterator': 'https://docs.python.org/3/library/typing.html#typing.AsyncIterator',
  'typing.Callable': 'https://docs.python.org/3/library/typing.html#typing.Callable',
  None: 'https://docs.python.org/3/library/constants.html#None',
  'typing.Self': 'https://docs.python.org/3/library/typing.html#typing.Self',
  'typing.Sequence': 'https://docs.python.org/3/library/typing.html#typing.Sequence',
  dict: 'https://docs.python.org/3/library/stdtypes.html#dict',
  int: 'https://docs.python.org/3/library/functions.html#int',
  object: 'https://docs.python.org/3/library/functions.html#object',
  str: 'https://docs.python.org/3/library/stdtypes.html#str',
  'datetime.timedelta': 'https://docs.python.org/3/library/datetime.html#datetime.timedelta',
  type: 'https://docs.python.org/3/library/functions.html#type',
};

const pythonThirdPartyLinks = {
  'temporalio.client.Client':
    'https://python.temporal.io/temporalio.client.Client.html',
  'temporalio.converter.DataConverter':
    'https://python.temporal.io/temporalio.converter.DataConverter.html',
};

function parseSdkPath(args) {
  const index = args.indexOf('--sdk');
  if (index < 0 || !args[index + 1]) {
    throw new Error(
      'usage: node scripts/generate-python-api-reference.mjs --sdk /path/to/agentk-sdk',
    );
  }
  return path.resolve(args[index + 1]);
}

function buildSymbolIndex(model) {
  const index = new Map();
  for (const [moduleName, module] of Object.entries(model.modules)) {
    for (const [name, member] of Object.entries(module.members)) {
      index.set(`${moduleName}.${name}`, member);
    }
  }
  return index;
}

function resolveMember(member, symbols, visited = new Set()) {
  if (member.kind !== 'alias') return structuredClone(member);
  if (!member.target || visited.has(member.target)) {
    throw new Error(`cannot resolve API alias ${member.target ?? '<missing>'}`);
  }
  const target = symbols.get(member.target);
  if (!target) throw new Error(`missing API alias target ${member.target}`);
  return {
    ...resolveMember(target, symbols, new Set([...visited, member.target])),
    definedAt: member.target,
  };
}

function createView(model, view, moduleNames, symbols) {
  const modules = {};
  for (const moduleName of moduleNames) {
    const module = model.modules[moduleName];
    if (!module) throw new Error(`missing Python API module ${moduleName}`);
    modules[moduleName] = {
      ...module,
      members: Object.fromEntries(
        Object.entries(module.members)
          .filter(
            ([name]) => !(moduleName.endsWith('_pb') && name === 'desc'),
          )
          .map(([name, member]) => [
            name,
            resolveMember(member, symbols),
          ]),
      ),
    };
  }
  return {package: model.package, view, modules};
}

function createLinkIndex(views) {
  const candidates = new Map();
  const add = (name, href) => {
    const targets = candidates.get(name) ?? new Set();
    targets.add(href);
    candidates.set(name, targets);
  };

  for (const view of views) {
    const route = `/reference/python/${view.view}`;
    for (const [moduleName, module] of Object.entries(view.modules)) {
      add(moduleName, `${route}#${moduleName.replace(/[^a-zA-Z0-9_-]/g, '-')}`);
      for (const [name, member] of Object.entries(module.members)) {
        const path = `${moduleName}.${name}`;
        const href = `${route}#${path.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
        add(name, href);
        add(path, href);
        if (member.definedAt) add(member.definedAt, href);

        for (const [childName, child] of Object.entries(member.members ?? {})) {
          if (child.kind !== 'function') continue;
          const childPath = `${path}.${childName}`;
          const childHref = `${route}#${childPath.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
          add(childName, childHref);
          add(childPath, childHref);
        }
      }
    }
  }

  const localLinks = Object.fromEntries(
    [...candidates.entries()]
      .filter(([, targets]) => targets.size === 1)
      .map(([name, targets]) => [name, [...targets][0]])
      .sort(([left], [right]) => left.localeCompare(right)),
  );
  return {...pythonStandardLibraryLinks, ...pythonThirdPartyLinks, ...localLinks};
}

async function writeJson(name, value) {
  await fs.writeFile(
    path.join(outputDir, `${name}.json`),
    `${JSON.stringify(value, null, 2)}\n`,
    'utf8',
  );
}

async function main() {
  const sdkRoot = parseSdkPath(process.argv.slice(2));
  const pythonRoot = path.join(sdkRoot, 'python');
  const generator = path.join(pythonRoot, 'tools', 'generate_api_reference.py');
  await fs.access(generator);

  const temporaryDir = await fs.mkdtemp(
    path.join(os.tmpdir(), 'agentk-python-api-reference-'),
  );
  const rawOutput = path.join(temporaryDir, 'python.json');
  try {
    await run(
      'uv',
      [
        'run',
        'python',
        'tools/generate_api_reference.py',
        '--output',
        rawOutput,
      ],
      {
        cwd: pythonRoot,
        env: process.env,
      },
    );

    const model = JSON.parse(await fs.readFile(rawOutput, 'utf8'));
    const symbols = buildSymbolIndex(model);
    const protocolModules = Object.keys(model.modules)
      .filter((name) => name.endsWith('_pb'))
      .sort();

    await fs.mkdir(outputDir, {recursive: true});
    const views = [
      createView(
        model,
        'client',
        ['agentk.client', 'agentk.client.grpc'],
        symbols,
      ),
      createView(model, 'worker', ['agentk.worker'], symbols),
      createView(model, 'protocol', protocolModules, symbols),
    ];

    await Promise.all([
      ...views.map((view) => writeJson(view.view, view)),
      writeJson('symbols', createLinkIndex(views)),
    ]);
  } finally {
    await fs.rm(temporaryDir, {recursive: true, force: true});
  }
}

await main();
