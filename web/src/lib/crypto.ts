import { argon2id } from 'hash-wasm';

const encoder = new TextEncoder();
const decoder = new TextDecoder();

/** Cryptographically secure random bytes. */
export function randomBytes(length: number): Uint8Array {
  const out = new Uint8Array(length);
  crypto.getRandomValues(out);
  return out;
}

export function randomKey(): Uint8Array {
  return randomBytes(32);
}

export function utf8(value: string): Uint8Array {
  return encoder.encode(value);
}

export function fromUtf8(value: Uint8Array): string {
  return decoder.decode(value);
}

/** Base64 helpers that avoid call-stack overflows for large buffers. */
export function toBase64(data: Uint8Array): string {
  let binary = '';
  const chunk = 0x8000;
  for (let i = 0; i < data.length; i += chunk) {
    binary += String.fromCharCode(...data.subarray(i, i + chunk));
  }
  return btoa(binary);
}

export function fromBase64(value: string): Uint8Array {
  const binary = atob(value);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

export function toBase64Url(data: Uint8Array): string {
  return toBase64(data).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
}

export function fromBase64Url(value: string): Uint8Array {
  const normalized = value.replaceAll('-', '+').replaceAll('_', '/');
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
  return fromBase64(padded);
}

const BASE32_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

export function base32Encode(data: Uint8Array): string {
  let bits = 0;
  let value = 0;
  let out = '';
  for (const byte of data) {
    value = (value << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      out += BASE32_ALPHABET[(value >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) out += BASE32_ALPHABET[(value << (5 - bits)) & 31];
  return out;
}

export function base32Decode(value: string): Uint8Array {
  const clean = value.toUpperCase().replace(/[^A-Z2-7]/g, '');
  let bits = 0;
  let acc = 0;
  const out: number[] = [];
  for (const ch of clean) {
    const idx = BASE32_ALPHABET.indexOf(ch);
    if (idx < 0) continue;
    acc = (acc << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      out.push((acc >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Uint8Array.from(out);
}

export function toHex(data: Uint8Array): string {
  return Array.from(data, (b) => b.toString(16).padStart(2, '0')).join('');
}

export function fromHex(value: string): Uint8Array {
  const clean = value.replace(/[^0-9a-f]/gi, '');
  const out = new Uint8Array(Math.floor(clean.length / 2));
  for (let i = 0; i < out.length; i++) out[i] = parseInt(clean.slice(i * 2, i * 2 + 2), 16);
  return out;
}

export async function sha256Hex(data: Uint8Array | ArrayBuffer): Promise<string> {
  const buffer = data instanceof Uint8Array ? data : new Uint8Array(data);
  const digest = await crypto.subtle.digest('SHA-256', buffer as unknown as BufferSource);
  return toHex(new Uint8Array(digest));
}

/**
 * Argon2id is intentionally expensive: 64 MiB, 3 passes, one lane. This is
 * used for E2EE key wrapping only and never runs on the server.
 */
export async function deriveArgon2Key(secret: string, salt: Uint8Array): Promise<CryptoKey> {
  const raw = (await argon2id({
    password: secret,
    salt,
    parallelism: 1,
    iterations: 3,
    memorySize: 65536,
    hashLength: 32,
    outputType: 'binary'
  })) as Uint8Array;
  return crypto.subtle.importKey('raw', raw as unknown as BufferSource, { name: 'AES-GCM' }, false, [
    'encrypt',
    'decrypt'
  ]);
}

export async function deriveHkdfKey(
  master: Uint8Array,
  salt: Uint8Array,
  info: string,
  length = 32
): Promise<CryptoKey> {
  const base = await crypto.subtle.importKey('raw', master as unknown as BufferSource, 'HKDF', false, [
    'deriveKey'
  ]);
  return crypto.subtle.deriveKey(
    { name: 'HKDF', hash: 'SHA-256', salt: salt as unknown as BufferSource, info: utf8(info) },
    base,
    { name: 'AES-GCM', length: length * 8 },
    false,
    ['encrypt', 'decrypt']
  );
}

export interface SealedBox {
  nonce: Uint8Array;
  data: Uint8Array;
}

export async function aesGcmEncrypt(
  key: CryptoKey,
  plaintext: Uint8Array,
  aad?: Uint8Array
): Promise<SealedBox> {
  const nonce = randomBytes(12);
  const params: AesGcmParams = { name: 'AES-GCM', iv: nonce as unknown as BufferSource };
  if (aad) params.additionalData = aad as unknown as BufferSource;
  const ciphertext = await crypto.subtle.encrypt(params, key, plaintext as unknown as BufferSource);
  return { nonce, data: new Uint8Array(ciphertext) };
}

export async function aesGcmDecrypt(
  key: CryptoKey,
  nonce: Uint8Array,
  data: Uint8Array,
  aad?: Uint8Array
): Promise<Uint8Array> {
  const params: AesGcmParams = { name: 'AES-GCM', iv: nonce as unknown as BufferSource };
  if (aad) params.additionalData = aad as unknown as BufferSource;
  const plaintext = await crypto.subtle.decrypt(params, key, data as unknown as BufferSource);
  return new Uint8Array(plaintext);
}

/** Recovery keys are 20 random bytes encoded as eight groups of four base32 characters. */
export function generateRecoveryKey(): { raw: Uint8Array; formatted: string } {
  const raw = randomBytes(20);
  const encoded = base32Encode(raw);
  const formatted = encoded.match(/.{1,4}/g)?.join('-') || encoded;
  return { raw, formatted };
}

export function normalizeRecoveryKey(value: string): Uint8Array | null {
  const raw = base32Decode(value);
  return raw.length === 20 ? raw : null;
}
