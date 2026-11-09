import {execFile} from 'node:child_process';
import fs from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';

const run = promisify(execFile);
const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const outputDir = path.join(rootDir, 'api-reference', 'go');

const protocolPackages = new Set([
  'gen/api/v1',
  'gen/capability/v1',
  'gen/common/v1',
  'gen/invocation/v1',
]);

const protobufMethods = new Set([
  'Descriptor',
  'Enum',
  'EnumDescriptor',
  'Number',
  'ProtoMessage',
  'ProtoReflect',
  'Reset',
  'String',
  'Type',
]);

function parseSdkPath(args) {
  const index = args.indexOf('--sdk');
  if (index < 0 || !args[index + 1]) {
    throw new Error(
      'usage: node scripts/generate-go-api-reference.mjs --sdk /path/to/agentk-sdk',
    );
  }
  return path.resolve(args[index + 1]);
}

function packagePath(model, pkg) {
  const prefix = `${model.modulePath}/`;
  if (!pkg.importPath.startsWith(prefix)) {
    throw new Error(
      `Go package ${pkg.importPath} is outside module ${model.modulePath}`,
    );
  }
  return pkg.importPath.slice(prefix.length);
}

function isGeneratedProtobufValue(value) {
  return value.names.every(
    (name) =>
      name.startsWith('File_') ||
      name.endsWith('_name') ||
      name.endsWith('_value'),
  );
}

function filterProtocolPackage(pkg) {
  return {
    ...pkg,
    vars: pkg.vars.filter((value) => !isGeneratedProtobufValue(value)),
    types: pkg.types.map((type) => ({
      ...type,
      methods: type.methods.filter(
        (method) =>
          !protobufMethods.has(method.name) && !method.name.startsWith('Get'),
      ),
    })),
  };
}

function createView(model, view, include, transform = (pkg) => pkg) {
  const packages = model.packages
    .filter((pkg) => include(packagePath(model, pkg)))
    .map(transform);
  if (packages.length === 0) {
    throw new Error(`Go API reference view ${view} has no packages`);
  }
  return {modulePath: model.modulePath, view, packages};
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
  const goRoot = path.join(sdkRoot, 'go');
  await fs.access(path.join(goRoot, 'internal', 'cmd', 'apiref', 'main.go'));

  const {stdout} = await run('go', ['run', './internal/cmd/apiref'], {
    cwd: goRoot,
    maxBuffer: 16 * 1024 * 1024,
  });
  const model = JSON.parse(stdout);
  if (!model.modulePath || !Array.isArray(model.packages)) {
    throw new Error('Go API reference generator returned an invalid model');
  }

  const views = [
    createView(
      model,
      'client',
      (name) => name === 'client' || name === 'client/grpc',
    ),
    createView(
      model,
      'worker',
      (name) =>
        name === 'temporal' || name === 'worker' || name.startsWith('worker/'),
    ),
    createView(
      model,
      'protocol',
      (name) => protocolPackages.has(name),
      filterProtocolPackage,
    ),
  ];

  await fs.mkdir(outputDir, {recursive: true});
  await Promise.all(views.map((view) => writeJson(view.view, view)));
}

await main();
