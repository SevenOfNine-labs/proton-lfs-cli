#!/usr/bin/env node

/**
 * Stand-in for `proton-drive-cli bridge <command>`: one JSON request on stdin,
 * one response envelope on stdout (submodules/proton-drive-cli/schemas/bridge/v1).
 * Objects are stored under MOCK_BRIDGE_STORAGE_DIR at <oid[0:2]>/<oid[2:4]>/<oid>.
 */

const fs = require('fs');
const path = require('path');

const subcommand = process.argv[2];
const command = process.argv[3] || '';

const STORAGE_DIR = (process.env.MOCK_BRIDGE_STORAGE_DIR || '/tmp/mock-bridge-storage').trim();

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

if (process.env.MOCK_BRIDGE_CRASH === '1') {
  process.exit(137);
}

if (process.env.MOCK_BRIDGE_HANG === '1') {
  setTimeout(() => {}, 999999);
  return;
}

function storagePath(oid) {
  const prefix = String(oid).slice(0, 2);
  const second = String(oid).slice(2, 4);
  return path.join(STORAGE_DIR, prefix, second, String(oid));
}

if (command === 'auth-state') {
  const state = process.env.MOCK_BRIDGE_AUTH_STATE || 'ready';
  const hasSession = state !== 'needs_login';
  const credentialsAvailable =
    process.env.MOCK_BRIDGE_CREDENTIALS_AVAILABLE === 'true' || state === 'ready';
  write({
    ok: true,
    payload: {
      state,
      hasSession,
      sessionValid: state === 'ready' || state === 'credentials_unavailable',
      sessionExpired: state === 'session_expired',
      sessionUidPresent: hasSession,
      credentialsAvailable,
      credentialStore: process.env.MOCK_BRIDGE_CREDENTIAL_STORE || 'git-credential',
      credentialHost: process.env.MOCK_BRIDGE_CREDENTIAL_HOST || 'proton-drive-cli.invalid',
      willAttemptNetwork: false,
      errors: [],
      actions: []
    }
  });
}

if (command === 'init') {
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
