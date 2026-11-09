import { api, type VaultCategory, type VaultEntry } from "@/lib/api";
import { decryptVault, encryptVault, isEncrypted } from "@/lib/vault-crypto";

/**
 * The Vault in the browser. Identity and finance documents are encrypted here, before upload, with a
 * passphrase that never leaves this device; ordinary documents may go up as they are.
 */

export const SENSITIVE_CATEGORIES: VaultCategory[] = ["id", "finance"];
export const isSensitive = (c: VaultCategory) => SENSITIVE_CATEGORIES.includes(c);

export const CATEGORY_LABELS: Record<VaultCategory, string> = {
  id: "ID (PAN, Aadhaar, passport)",
  finance: "Bank and tax",
  resume: "Resume and career",
  certificate: "Certificates and letters",
  other: "Other",
};

export const MAX_BYTES = 5 * 1024 * 1024;

export async function sha256Hex(data: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", data as Uint8Array<ArrayBuffer>);
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("");
}

/** What a document is, from its first bytes, so it can be shown in the app. Anything unknown is downloaded. */
export function sniffMime(b: Uint8Array): string {
  const at = (...sig: number[]) => sig.every((v, i) => b[i] === v);
  if (at(0x89, 0x50, 0x4e, 0x47)) return "image/png";
  if (at(0xff, 0xd8, 0xff)) return "image/jpeg";
  if (at(0x47, 0x49, 0x46, 0x38)) return "image/gif";
  if (at(0x25, 0x50, 0x44, 0x46)) return "application/pdf";
  if (at(0x52, 0x49, 0x46, 0x46) && b[8] === 0x57 && b[9] === 0x45 && b[10] === 0x42 && b[11] === 0x50) return "image/webp";
  return "application/octet-stream";
}

// The passphrase lives in memory for this page load only. It is never stored, sent, or logged.
let passphrase: string | null = null;
const listeners = new Set<() => void>();
export const getPassphrase = () => passphrase;
export function setPassphrase(p: string | null) {
  passphrase = p && p.length > 0 ? p : null;
  listeners.forEach((l) => l());
}
export function subscribePassphrase(cb: () => void) {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

export interface AddInput {
  file: File;
  label: string;
  category: VaultCategory;
}

/** Encrypts when the category is sensitive, then uploads. Throws a plain message when something is wrong. */
export async function addDocument({ file, label, category }: AddInput) {
  if (file.size === 0) throw new Error("That file is empty.");
  if (file.size > MAX_BYTES - 1024) throw new Error("Documents can be at most 5 MB. Try a smaller photo or a compressed PDF.");
  const plain = new Uint8Array(await file.arrayBuffer());
  const fingerprint = await sha256Hex(plain);
  let payload: Uint8Array = plain;
  if (isSensitive(category)) {
    if (!passphrase) throw new Error("Enter your Vault passphrase first. It encrypts this document on your device.");
    payload = await encryptVault(passphrase, plain);
  }
  const form = new FormData();
  form.append("label", label);
  form.append("category", category);
  form.append("fingerprint", fingerprint);
  form.append("file", new Blob([payload as Uint8Array<ArrayBuffer>], { type: isSensitive(category) ? "application/octet-stream" : file.type || "application/octet-stream" }), file.name);
  return api.addVault(form);
}

export interface OpenedDocument {
  bytes: Uint8Array;
  mime: string;
  filename: string;
}

/** Downloads a document and decrypts it if it is encrypted. */
export async function openDocument(entry: VaultEntry): Promise<OpenedDocument> {
  const { bytes } = await api.getArtifactBytes(entry.id);
  let plain: Uint8Array = bytes;
  if (entry.encrypted || isEncrypted(bytes)) {
    if (!passphrase) throw new Error("Enter your Vault passphrase to open this document.");
    plain = await decryptVault(passphrase, bytes);
  }
  const name = (entry.filename ?? entry.label).replace(/\.gvlt$/, "");
  return { bytes: plain, mime: sniffMime(plain), filename: name };
}
