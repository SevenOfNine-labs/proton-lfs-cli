#!/usr/bin/env node

/**
 * Mock proton-drive-cli bridge subprocess
 * Simulates the proton-drive-cli bridge command protocol:
 *   node <script> bridge <command>
 * Reads JSON from stdin, writes JSON to stdout
 *
 * For E2E testing, set MOCK_BRIDGE_STORAGE_DIR to a directory path.
 * Upload will store file content keyed by OID; download will write it
 * to the requested outputPath.
 */

const fs = require('fs');
const path = require('path');

// argv: [node, script, 'bridge', command]
const subcommand = process.argv[2];
const command = process.argv[3] || '';

const STORAGE_DIR = (process.env.MOCK_BRIDGE_STORAGE_DIR || '/tmp/mock-bridge-storage').trim();

// Only respond to 'bridge' subcommand
if (subcommand !== 'bridge') {
  process.stdout.write(JSON.stringify({
    ok: false,
    code: 400,
    error: `expected 'bridge' subcommand, got: ${subcommand}`
  }));
  process.exit(1);
}

const input = fs.readFileSync(0, 'utf8');
const request = input.trim() ? JSON.parse(input) : {};

function write(payload, exitCode = 0) {
  if (process.env.MOCK_BRIDGE_STDOUT_NOISE === '1') {
    process.stdout.write('Initializing...\n');
  }
  if (process.env.MOCK_BRIDGE_STDERR_NOISE === '1') {
    process.stderr.write('mock drive-cli warning\n');
  }
  process.stdout.write(JSON.stringify(payload));
  process.exit(exitCode);
}

// Simulate process crash
if (process.env.MOCK_BRIDGE_CRASH === '1') {
  process.exit(137);
}

// Simulate timeout (hang)
if (process.env.MOCK_BRIDGE_HANG === '1') {
  // Do nothing, let timeout kill us
  setTimeout(() => {}, 999999);
  return;
}

function storagePath(oid) {
  const prefix = String(oid).slice(0, 2);
  const second = String(oid).slice(2, 4);
  return path.join(STORAGE_DIR, prefix, second, String(oid));
}

if (command === 'auth') {
  if (request.username === 'bad-user') {
    write({ ok: false, code: 401, error: 'invalid credentials' }, 1);
  }
  write({
    ok: true,
    payload: {
      authenticated: true,
      username: request.username
    }
  });
}

if (command === 'init') {
  // Ensure the storage base directory exists
  fs.mkdirSync(STORAGE_DIR, { recursive: true });
  write({
    ok: true,
    payload: {
      initialized: true,
      storageBase: request.storageBase || 'LFS'
    }
  });
}

if (command === 'exists') {
  const objectPath = storagePath(request.oid);
  const exists = fs.existsSync(objectPath);
  write({
    ok: true,
    payload: {
      exists: exists,
      oid: request.oid
    }
  });
}

if (command === 'upload') {
  if (request.oid === 'missing') {
    write({ ok: false, code: 404, error: 'source object missing' }, 1);
  }

  // Store file content for E2E roundtrip
  let fileSize = 123;
  const sourcePath = request.path;
  if (sourcePath && fs.existsSync(sourcePath)) {
    const objectPath = storagePath(request.oid);
    fs.mkdirSync(path.dirname(objectPath), { recursive: true });
    fs.copyFileSync(sourcePath, objectPath);
    fileSize = fs.statSync(objectPath).size;
  }

  write({
    ok: true,
    payload: {
      oid: request.oid,
      size: fileSize,
      fileId: 'mock-file-id',
      revisionId: 'mock-revision-id',
      uploaded: true,
      location: `${request.storageBase}/${String(request.oid).slice(0, 2)}/${String(request.oid).slice(2, 4)}/${String(request.oid)}`
    }
  });
}

if (command === 'download') {
  // Retrieve stored content for E2E roundtrip
  const objectPath = storagePath(request.oid);
  if (fs.existsSync(objectPath) && request.outputPath) {
    const outputDir = path.dirname(request.outputPath);
    fs.mkdirSync(outputDir, { recursive: true });
    fs.copyFileSync(objectPath, request.outputPath);
    const fileSize = fs.statSync(request.outputPath).size;
    write({
      ok: true,
      payload: {
        oid: request.oid,
        size: fileSize,
        outputPath: request.outputPath,
        downloaded: true,
        path: request.outputPath
      }
    });
  }

  write({
    ok: true,
    payload: {
      oid: request.oid,
      size: 321,
      outputPath: request.outputPath,
      downloaded: true,
      path: request.outputPath
    }
  });
}

if (command === 'list') {
  write({
    ok: true,
    payload: {
      files: [
        {
          oid: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          name: 'fixture',
          size: 1,
          type: 'file',
          modifiedTime: Date.now()
        }
      ]
    }
  });
}

write({ ok: false, code: 400, error: `unsupported command: ${command}` }, 1);
