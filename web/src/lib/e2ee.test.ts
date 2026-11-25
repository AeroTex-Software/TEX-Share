import { describe, expect, it } from 'vitest';
import {
  base32Decode,
  base32Encode,
  fromBase64Url,
  fromHex,
  generateRecoveryKey,
  normalizeRecoveryKey,
  randomKey,
  toBase64Url,
  toHex
} from './crypto';
import {
  decryptContainerToBlob,
  decryptManifest,
  encryptFileStream,
  encryptManifest,
  encryptedSize,
  E2EE_CHUNK_SIZE,
  keyFragment,
  masterKeyFromInput,
  parseContainerHeader,
  parseKeyFragment,
  unwrapMasterKey,
  unwrapWithPassword,
  unwrapWithRecovery,
  wrapMasterKey,
  type E2EEManifestData
} from './e2ee';

async function collect(blob: Blob, key: Uint8Array): Promise<Uint8Array> {
  const parts: Uint8Array[] = [];
  let total = 0;
  for await (const chunk of encryptFileStream(blob, key)) {
    parts.push(chunk);
    total += chunk.length;
  }
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

describe('crypto helpers', () => {
  it('round-trips base32', () => {
    const raw = randomKey();
    expect(Array.from(base32Decode(base32Encode(raw)))).toEqual(Array.from(raw));
  });

  it('round-trips base64url and hex', () => {
    const raw = randomKey();
    expect(Array.from(fromBase64Url(toBase64Url(raw)))).toEqual(Array.from(raw));
    expect(Array.from(fromHex(toHex(raw)))).toEqual(Array.from(raw));
  });

  it('formats recovery keys as eight dash-separated groups', () => {
    const { raw, formatted } = generateRecoveryKey();
    expect(raw.length).toBe(20);
    expect(formatted).toMatch(/^[A-Z2-7]{4}(-[A-Z2-7]{4}){7}$/);
    // Normalisation ignores dashes and case.
    expect(normalizeRecoveryKey(formatted.toLowerCase().replace(/-/g, ' '))).toEqual(raw);
    expect(normalizeRecoveryKey('not-a-key')).toBeNull();
  });
});

describe('encrypted container', () => {
  it('encrypts and decrypts a payload and matches encryptedSize', async () => {
    const master = randomKey();
    const plaintext = new TextEncoder().encode('TEX Share end-to-end encryption test payload.');
    const blob = new Blob([plaintext]);
    const ciphertext = await collect(blob, master);

    expect(ciphertext.length).toBe(encryptedSize(plaintext.length));

    const header = parseContainerHeader(ciphertext.slice(0, 37));
    expect(header.plainSize).toBe(plaintext.length);
    expect(header.chunkSize).toBe(E2EE_CHUNK_SIZE);

    const decrypted = await decryptContainerToBlob(new Blob([ciphertext]), master);
    expect(new Uint8Array(await decrypted.arrayBuffer())).toEqual(plaintext);
  });

  it('handles multi-chunk payloads', async () => {
    const master = randomKey();
    const plaintext = new Uint8Array(E2EE_CHUNK_SIZE + 4096);
    for (let i = 0; i < plaintext.length; i++) plaintext[i] = i % 251;
    const ciphertext = await collect(new Blob([plaintext]), master);
    expect(ciphertext.length).toBe(encryptedSize(plaintext.length));
    const decrypted = await decryptContainerToBlob(new Blob([ciphertext]), master);
    expect(new Uint8Array(await decrypted.arrayBuffer())).toEqual(plaintext);
  });

  it('rejects a tampered container', async () => {
    const master = randomKey();
    const ciphertext = await collect(new Blob([new Uint8Array([1, 2, 3, 4, 5])]), master);
    const tampered = ciphertext.slice();
    tampered[tampered.length - 20] ^= 0xff;
    await expect(decryptContainerToBlob(new Blob([tampered]), master)).rejects.toBeTruthy();
  });

  it('rejects decryption with the wrong key', async () => {
    const ciphertext = await collect(new Blob([new Uint8Array([9, 9, 9])]), randomKey());
    await expect(decryptContainerToBlob(new Blob([ciphertext]), randomKey())).rejects.toBeTruthy();
  });
});

describe('manifest', () => {
  const manifest: E2EEManifestData = {
    v: 1,
    title: 'Secret',
    description: 'Encrypted metadata',
    type: 'file',
    files: [{ id: 'f1', name: 'a.txt', mime: 'text/plain', size: 3 }]
  };

  it('round-trips a manifest', async () => {
    const master = randomKey();
    const encoded = await encryptManifest(manifest, master);
    const decoded = await decryptManifest(encoded, master);
    expect(decoded).toEqual(manifest);
  });

  it('fails with the wrong key', async () => {
    const encoded = await encryptManifest(manifest, randomKey());
    await expect(decryptManifest(encoded, randomKey())).rejects.toBeTruthy();
  });
});

describe('key wrapping', () => {
  it('wraps and unwraps with a password', async () => {
    const master = randomKey();
    const wrap = await wrapMasterKey(master, 'correct horse battery staple');
    expect(Array.from(await unwrapWithPassword(wrap, 'correct horse battery staple'))).toEqual(Array.from(master));
    await expect(unwrapWithPassword(wrap, 'wrong password')).rejects.toBeTruthy();
  });

  it('wraps and unwraps with a recovery key', async () => {
    const master = randomKey();
    const { raw, formatted } = generateRecoveryKey();
    const wrap = await wrapMasterKey(master, `recovery:${toHex(raw)}`);
    expect(Array.from(await unwrapWithRecovery(wrap, formatted))).toEqual(Array.from(master));
    await expect(unwrapWithRecovery(wrap, 'AAAA-BBBB-CCCC-DDDD-EEEE-FFFF-GGGG-HHHH')).rejects.toBeTruthy();
  });

  it('rejects a malformed wrap', async () => {
    await expect(unwrapMasterKey('not-base64-json', 'x')).rejects.toBeTruthy();
  });
});

describe('key fragments', () => {
  it('round-trips through the URL fragment', () => {
    const master = randomKey();
    const fragment = keyFragment(master);
    expect(Array.from(parseKeyFragment(fragment) ?? [])).toEqual(Array.from(master));
    expect(Array.from(masterKeyFromInput(toBase64Url(master)) ?? [])).toEqual(Array.from(master));
    expect(Array.from(masterKeyFromInput(toHex(master)) ?? [])).toEqual(Array.from(master));
    expect(masterKeyFromInput('garbage')).toBeNull();
  });
});
