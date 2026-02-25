import { defineConfig } from '@hey-api/openapi-ts';
import { existsSync, readFileSync } from 'fs';
import * as path from 'path';
import { fileURLToPath } from 'url';

const configDir = path.dirname(fileURLToPath(import.meta.url));
const localSpecPath = path.join(configDir, 'openapi.json');
const httpMethods = new Set(['delete', 'get', 'head', 'options', 'patch', 'post', 'put', 'trace']);

const loadLocalSpec = () => {
  if (!existsSync(localSpecPath)) return null;
  try {
    return JSON.parse(readFileSync(localSpecPath, 'utf-8'));
  } catch {
    return null;
  }
};

const stripApiPrefix = (segments: string[]) => {
  const trimmed = [...segments];
  if (trimmed[0] === 'api') trimmed.shift();
  if (trimmed[0] === 'v1') trimmed.shift();
  return trimmed;
};

const createOperationId = (method: string, rawPath: string) => {
  const normalizedPath = rawPath.replace(/{(.*?)}/g, 'by-$1');
  const segments = normalizedPath
    .split('/')
    .map(segment => segment.replace(/[{}]/g, ''))
    .filter(Boolean);
  const filteredSegments = stripApiPrefix(segments);
  return [method.toLowerCase(), ...filteredSegments].join('-');
};

const buildOperationIdPatch = (spec: { paths?: Record<string, Record<string, unknown>> } | null) => {
  if (!spec?.paths) return undefined;
  const operations: Record<string, (operation: { operationId?: string }) => void> = {};
  for (const [pathKey, pathItem] of Object.entries(spec.paths)) {
    if (!pathItem || typeof pathItem !== 'object') continue;
    for (const [method, operation] of Object.entries(pathItem)) {
      if (!httpMethods.has(method)) continue;
      if (!operation || typeof operation !== 'object') continue;
      const operationId = createOperationId(method, pathKey);
      operations[`${method.toUpperCase()} ${pathKey}`] = op => {
        op.operationId = operationId;
      };
    }
  }
  return operations;
};

const localSpec = loadLocalSpec();
const operationIdPatch = buildOperationIdPatch(localSpec);

export default defineConfig({
  input: existsSync(localSpecPath) ? localSpecPath : 'http://localhost:3003/openapi.json',
  parser: operationIdPatch
    ? {
        patch: {
          operations: operationIdPatch,
        },
      }
    : undefined,
  output: {
    path: path.join(configDir, 'src/api/generated'),
    postProcess: ['prettier', 'eslint'],
  },
  plugins: [
    {
      name: '@hey-api/client-fetch',
      baseUrl: false,
      bundle: false,
    },
    '@hey-api/typescript',
    '@hey-api/schemas',
    {
      name: '@hey-api/sdk',
      operations: {
        strategy: 'flat',
        nesting: 'operationId',
        nestingDelimiters: /[-./]/,
      },
    },
    {
      name: '@tanstack/vue-query',
      exportFromIndex: true,
      queryKeys: true,
      queryOptions: true,
      infiniteQueryOptions: false,
    },
  ],
});
