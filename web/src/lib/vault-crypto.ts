/**
 * Vault encryption in the browser, byte-for-byte compatible with internal/vault/crypto.go. Sensitive
 * documents (PAN, Aadhaar, passport) are encrypted here, on the device, before upload, and decrypted
 * here after download, so the cloud only ever holds ciphertext.
 *
 * Format: "GVLT1" | iterations (uint32 big endian) | salt (16) | nonce (12) | AES-256-GCM ciphertext+tag.
 * Key: PBKDF2-HMAC-SHA256 of the passphrase. The header (magic, iterations, salt) is authenticated.
 */

const MAGIC = new TextEncoder().encode("GVLT1");
const SALT_LEN = 16;
const NONCE_LEN = 12;
const HEADER_LEN = MAGIC.length + 4 + SALT_LEN + NONCE_LEN;
export const DEFAULT_ITERATIONS = 600_000;
const MIN_ITERATIONS = 1_000;
const MAX_ITERATIONS = 10_000_000;

export class VaultDecryptError extends Error {
  constructor() {
    // A wrong passphrase and a damaged file look the same on purpose.
    super("Could not decrypt: wrong passphrase, or the file is damaged.");
    this.name = "VaultDecryptError";
  }
}

// WebCrypto wants plain ArrayBuffer-backed views; this narrows Uint8Array's generic buffer type.
const bytes = (u: Uint8Array) => u as Uint8Array<ArrayBuffer>;

export function isEncrypted(data: Uint8Array): boolean {
  if (data.length < HEADER_LEN + 16) return false;
  return MAGIC.every((b, i) => data[i] === b);
}

async function deriveKey(passphrase: string, salt: Uint8Array, iterations: number): Promise<CryptoKey> {
  const base = await crypto.subtle.importKey("raw", new TextEncoder().encode(passphrase), "PBKDF2", false, ["deriveKey"]);
  return crypto.subtle.deriveKey(
    { name: "PBKDF2", hash: "SHA-256", salt: bytes(salt), iterations },
    base,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
}

export interface EncryptOptions {
  /** For test vectors only. Real files must use the random defaults: reusing a nonce breaks AES-GCM. */
  salt?: Uint8Array;
  nonce?: Uint8Array;
  iterations?: number;
}

export async function encryptVault(passphrase: string, plaintext: Uint8Array, opts: EncryptOptions = {}): Promise<Uint8Array> {
  if (!passphrase) throw new Error("A passphrase is required.");
  const salt = opts.salt ?? crypto.getRandomValues(new Uint8Array(SALT_LEN));
  const nonce = opts.nonce ?? crypto.getRandomValues(new Uint8Array(NONCE_LEN));
  const iterations = opts.iterations ?? DEFAULT_ITERATIONS;
  if (salt.length !== SALT_LEN || nonce.length !== NONCE_LEN) throw new Error("Bad salt or nonce length.");
  if (iterations < MIN_ITERATIONS || iterations > MAX_ITERATIONS) throw new Error("Iterations out of range.");

  const header = new Uint8Array(HEADER_LEN);
  header.set(MAGIC, 0);
  new DataView(header.buffer).setUint32(MAGIC.length, iterations, false);
  header.set(salt, MAGIC.length + 4);
  header.set(nonce, MAGIC.length + 4 + SALT_LEN);

  const key = await deriveKey(passphrase, salt, iterations);
  const aad = header.slice(0, HEADER_LEN - NONCE_LEN);
  const sealed = new Uint8Array(
    await crypto.subtle.encrypt({ name: "AES-GCM", iv: bytes(nonce), additionalData: bytes(aad) }, key, bytes(plaintext)),
  );
  const out = new Uint8Array(HEADER_LEN + sealed.length);
  out.set(header, 0);
  out.set(sealed, HEADER_LEN);
  return out;
}

export async function decryptVault(passphrase: string, data: Uint8Array): Promise<Uint8Array> {
  if (!isEncrypted(data)) throw new Error("This is not an encrypted vault file.");
  const iterations = new DataView(data.buffer, data.byteOffset, data.byteLength).getUint32(MAGIC.length, false);
  if (iterations < MIN_ITERATIONS || iterations > MAX_ITERATIONS) throw new VaultDecryptError();
  const salt = data.slice(MAGIC.length + 4, MAGIC.length + 4 + SALT_LEN);
  const nonce = data.slice(MAGIC.length + 4 + SALT_LEN, HEADER_LEN);
  const aad = data.slice(0, HEADER_LEN - NONCE_LEN);
  try {
    const key = await deriveKey(passphrase, salt, iterations);
    return new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv: bytes(nonce), additionalData: bytes(aad) }, key, bytes(data.slice(HEADER_LEN))));
  } catch {
    throw new VaultDecryptError();
  }
}

export const toHex = (u: Uint8Array) => Array.from(u, (b) => b.toString(16).padStart(2, "0")).join("");
export const fromHex = (h: string) => Uint8Array.from(h.match(/../g) ?? [], (x) => parseInt(x, 16));
