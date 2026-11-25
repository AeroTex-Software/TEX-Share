import { Zip, ZipPassThrough } from 'fflate';

function cleanName(name: string): string {
  const parts = (name || 'file')
    .replaceAll('\\', '/')
    .split('/')
    .map((part) => part.trim())
    .filter((part) => part && part !== '.' && part !== '..');
  return parts.length ? parts.join('/') : 'file';
}

function uniqueName(used: Set<string>, name: string): string {
  const cleaned = cleanName(name);
  if (!used.has(cleaned)) {
    used.add(cleaned);
    return cleaned;
  }
  const slash = cleaned.lastIndexOf('/');
  const dir = slash >= 0 ? cleaned.slice(0, slash + 1) : '';
  const base = slash >= 0 ? cleaned.slice(slash + 1) : cleaned;
  const dot = base.lastIndexOf('.');
  const stem = dot > 0 ? base.slice(0, dot) : base;
  const ext = dot > 0 ? base.slice(dot) : '';
  for (let i = 2; ; i++) {
    const candidate = `${dir}${stem} (${i})${ext}`;
    if (!used.has(candidate)) {
      used.add(candidate);
      return candidate;
    }
  }
}

/**
 * Builds a ZIP incrementally so encrypted downloads can be decrypted and
 * appended file by file without holding every plaintext file at once.
 */
export class StreamingZip {
  private readonly parts: Uint8Array[] = [];
  private readonly used = new Set<string>();
  private readonly zip: Zip;
  private error: Error | null = null;
  private finished = false;
  private readonly done: Promise<void>;
  private resolveDone!: () => void;
  private rejectDone!: (err: Error) => void;

  constructor() {
    this.done = new Promise<void>((resolve, reject) => {
      this.resolveDone = resolve;
      this.rejectDone = reject;
    });
    this.zip = new Zip((err, data, final) => {
      if (err) {
        this.error = err instanceof Error ? err : new Error(String(err));
        this.rejectDone(this.error);
        return;
      }
      if (data && data.length) this.parts.push(data);
      if (final) {
        this.finished = true;
        this.resolveDone();
      }
    });
  }

  async add(name: string, chunks: AsyncIterable<Uint8Array>): Promise<void> {
    if (this.error) throw this.error;
    const entry = new ZipPassThrough(uniqueName(this.used, name));
    this.zip.add(entry);
    for await (const chunk of chunks) {
      if (chunk.length) entry.push(chunk, false);
    }
    entry.push(new Uint8Array(0), true);
  }

  async addBlob(name: string, blob: Blob): Promise<void> {
    await this.add(name, blobChunks(blob));
  }

  async end(): Promise<Blob> {
    this.zip.end();
    if (!this.finished) await this.done;
    if (this.error) throw this.error;
    return new Blob(this.parts as unknown as BlobPart[], { type: 'application/zip' });
  }
}

async function* blobChunks(blob: Blob, size = 1024 * 1024): AsyncGenerator<Uint8Array> {
  for (let offset = 0; offset < blob.size; offset += size) {
    yield new Uint8Array(await blob.slice(offset, Math.min(offset + size, blob.size)).arrayBuffer());
  }
}
