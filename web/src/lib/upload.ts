import { api, APIError, xhrUpload } from './api';
import { encryptedSize, encryptFileStream } from './e2ee';
import type { UploadInit } from './types';

export interface UploadProgress {
  loaded: number;
  total: number;
  phase: 'encrypting' | 'uploading';
}

export interface UploadResult {
  id: string;
  filename: string;
  mime: string;
  size: number;
  plainSize: number;
  encrypted: boolean;
  scanStatus: string;
}

interface UploadStatusResponse {
  upload: UploadInit & { status: string; scanStatus: string };
  received: number[];
  receivedBytes: number;
  totalChunks: number;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function isAbort(err: unknown): boolean {
  return (err as Error)?.name === 'AbortError';
}

async function initUpload(file: File, encrypted: boolean, size: number, mime: string): Promise<UploadInit> {
  return api<UploadInit>('/uploads', {
    method: 'POST',
    body: {
      filename: file.name || 'file',
      size,
      mime,
      encrypted
    }
  });
}

async function completeUpload(id: string): Promise<string> {
  const res = await api<{ ok: boolean; scanStatus?: string }>(`/uploads/${encodeURIComponent(id)}/complete`, {
    method: 'POST',
    body: {}
  });
  return res?.scanStatus || 'unknown';
}

async function putChunk(id: string, index: number, data: Uint8Array, signal?: AbortSignal): Promise<void> {
  const blob = new Blob([data as unknown as BlobPart], { type: 'application/octet-stream' });
  await xhrUpload(`PUT`, `/uploads/${encodeURIComponent(id)}/chunks/${index}`, blob, undefined, signal);
}

async function putChunkRetry(
  id: string,
  index: number,
  data: Uint8Array,
  signal?: AbortSignal
): Promise<void> {
  let lastError: unknown = null;
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      await putChunk(id, index, data, signal);
      return;
    } catch (err) {
      lastError = err;
      if (isAbort(err) || signal?.aborted) throw err;
      if (err instanceof APIError && err.status >= 400 && err.status < 500) throw err;
      await sleep(400 * (attempt + 1));
    }
  }
  throw lastError;
}

async function uploadEncrypted(
  file: File,
  masterKey: Uint8Array,
  onProgress?: (progress: UploadProgress) => void,
  signal?: AbortSignal
): Promise<UploadResult> {
  const total = encryptedSize(file.size);
  const init = await initUpload(file, true, total, 'application/octet-stream');
  if (init.mode !== 'chunked' || init.chunkSize <= 0) {
    throw new APIError(400, 'upload_failed', 'Encrypted uploads require chunked mode.');
  }
  const chunkSize = init.chunkSize;
  let pending = new Uint8Array(chunkSize);
  let pendingLength = 0;
  let index = 0;
  let uploaded = 0;

  const flush = async () => {
    if (pendingLength === 0) return;
    const data = pending.slice(0, pendingLength);
    await putChunkRetry(init.id, index, data, signal);
    uploaded += pendingLength;
    index += 1;
    pendingLength = 0;
    onProgress?.({ loaded: uploaded, total, phase: 'uploading' });
  };

  try {
    let produced = 0;
    for await (const piece of encryptFileStream(file, masterKey, (done) => {
      produced = done;
      onProgress?.({ loaded: Math.min(done, total), total, phase: 'encrypting' });
    })) {
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
      let offset = 0;
      while (offset < piece.length) {
        const room = chunkSize - pendingLength;
        const take = Math.min(room, piece.length - offset);
        pending.set(piece.subarray(offset, offset + take), pendingLength);
        pendingLength += take;
        offset += take;
        if (pendingLength === chunkSize) await flush();
      }
    }
    if (pendingLength > 0) await flush();
    if (index !== init.totalChunks) {
      throw new APIError(500, 'upload_failed', 'Encrypted container length mismatch.');
    }
    if (uploaded !== total) {
      throw new APIError(500, 'upload_failed', 'Encrypted upload length mismatch.');
    }
    const scanStatus = await completeUpload(init.id);
    onProgress?.({ loaded: total, total, phase: 'uploading' });
    return {
      id: init.id,
      filename: file.name,
      mime: file.type || 'application/octet-stream',
      size: total,
      plainSize: file.size,
      encrypted: true,
      scanStatus
    };
  } catch (err) {
    if (isAbort(err) || signal?.aborted) {
      await cancelUpload(init.id).catch(() => undefined);
    }
    throw err;
  }
}

async function uploadPlainChunked(
  file: File,
  init: UploadInit,
  onProgress?: (progress: UploadProgress) => void,
  signal?: AbortSignal
): Promise<UploadResult> {
  let received: number[] = [];
  try {
    const status = await api<UploadStatusResponse>(`/uploads/${encodeURIComponent(init.id)}`);
    received = status.received || [];
  } catch {
    received = [];
  }
  const done = new Set(received);
  let uploaded = 0;
  for (const idx of received) uploaded += Math.min(init.chunkSize, Math.max(0, init.size - idx * init.chunkSize));
  onProgress?.({ loaded: uploaded, total: init.size, phase: 'uploading' });

  for (let index = 0; index < init.totalChunks; index++) {
    if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
    if (done.has(index)) continue;
    const start = index * init.chunkSize;
    const end = Math.min(start + init.chunkSize, init.size);
    const data = new Uint8Array(await file.slice(start, end).arrayBuffer());
    await putChunkRetry(init.id, index, data, signal);
    uploaded += data.length;
    onProgress?.({ loaded: uploaded, total: init.size, phase: 'uploading' });
  }
  const scanStatus = await completeUpload(init.id);
  return {
    id: init.id,
    filename: file.name,
    mime: file.type || 'application/octet-stream',
    size: file.size,
    plainSize: file.size,
    encrypted: false,
    scanStatus
  };
}

async function uploadPlainDirect(
  file: File,
  init: UploadInit,
  onProgress?: (progress: UploadProgress) => void,
  signal?: AbortSignal
): Promise<UploadResult> {
  await xhrUpload(
    'PUT',
    `/uploads/${encodeURIComponent(init.id)}`,
    file,
    (loaded, total) => onProgress?.({ loaded, total, phase: 'uploading' }),
    signal
  );
  const scanStatus = await completeUpload(init.id);
  return {
    id: init.id,
    filename: file.name,
    mime: file.type || 'application/octet-stream',
    size: file.size,
    plainSize: file.size,
    encrypted: false,
    scanStatus
  };
}

export async function uploadFile(
  file: File,
  options: {
    encrypted: boolean;
    masterKey?: Uint8Array;
    onProgress?: (progress: UploadProgress) => void;
    signal?: AbortSignal;
  }
): Promise<UploadResult> {
  if (options.encrypted) {
    if (!options.masterKey) throw new APIError(500, 'upload_failed', 'Missing encryption key.');
    return uploadEncrypted(file, options.masterKey, options.onProgress, options.signal);
  }
  const init = await initUpload(file, false, file.size, file.type || 'application/octet-stream');
  if (init.mode === 'chunked') {
    return uploadPlainChunked(file, init, options.onProgress, options.signal);
  }
  return uploadPlainDirect(file, init, options.onProgress, options.signal);
}

export async function cancelUpload(id: string): Promise<void> {
  try {
    await api(`/uploads/${encodeURIComponent(id)}`, { method: 'DELETE' });
  } catch {
    // Already completed, attached or expired: nothing left to clean up.
  }
}
