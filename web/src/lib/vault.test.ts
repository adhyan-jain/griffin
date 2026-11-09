// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/api", () => ({ api: { addVault: vi.fn(), getArtifactBytes: vi.fn() } }));

import { api, type VaultEntry } from "@/lib/api";
import { addDocument, isSensitive, openDocument, setPassphrase, sha256Hex, sniffMime } from "@/lib/vault";
import { decryptVault, encryptVault, isEncrypted } from "@/lib/vault-crypto";

const addMock = vi.mocked(api.addVault);
const bytesMock = vi.mocked(api.getArtifactBytes);
const png = Uint8Array.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4, 5, 6, 7, 8]);
const file = (data: Uint8Array, name = "pan.png", type = "image/png") => new File([data as Uint8Array<ArrayBuffer>], name, { type });
const entry = (over: Partial<VaultEntry> = {}): VaultEntry => ({
  id: "a1", label: "PAN card", category: "id", sensitive: true, encrypted: true, filename: "pan.png.gvlt", size_bytes: 100, created_at: "2026-11-09T00:00:00Z", ...over,
});

beforeEach(() => {
  addMock.mockReset();
  bytesMock.mockReset();
  setPassphrase(null);
  addMock.mockResolvedValue({ duplicate: false, entry: entry() });
});

async function uploaded(): Promise<{ form: FormData; body: Uint8Array }> {
  const form = addMock.mock.calls[0][0] as FormData;
  const f = form.get("file") as File;
  return { form, body: new Uint8Array(await f.arrayBuffer()) };
}

describe("vault in the browser", () => {
  it("sensitive documents are encrypted before they leave the device", async () => {
    setPassphrase("my vault passphrase");
    await addDocument({ file: file(png), label: "PAN card", category: "id" });
    const { form, body } = await uploaded();
    expect(isEncrypted(body)).toBe(true);
    expect(Array.from(body).join(",")).not.toContain(Array.from(png).join(","));
    expect(await decryptVault("my vault passphrase", body)).toEqual(png);
    expect(form.get("category")).toBe("id");
    expect(form.get("fingerprint")).toBe(await sha256Hex(png)); // of the original, so mirroring twice is recognised
  });

  it("refuses to upload a sensitive document without a passphrase, and sends nothing", async () => {
    await expect(addDocument({ file: file(png), label: "PAN card", category: "id" })).rejects.toThrow(/passphrase/);
    await expect(addDocument({ file: file(png), label: "Statement", category: "finance" })).rejects.toThrow(/passphrase/);
    expect(addMock).not.toHaveBeenCalled();
  });

  it("ordinary documents go up as they are, with no passphrase needed", async () => {
    const md = new TextEncoder().encode("# Career context");
    await addDocument({ file: file(md, "AJ_resume_context.md", "text/markdown"), label: "Resume context", category: "resume" });
    const { body } = await uploaded();
    expect(isEncrypted(body)).toBe(false);
    expect(new TextDecoder().decode(body)).toBe("# Career context");
  });

  it("rejects empty and oversized files before any work", async () => {
    await expect(addDocument({ file: file(new Uint8Array(0)), label: "x", category: "other" })).rejects.toThrow(/empty/);
    await expect(addDocument({ file: file(new Uint8Array(5 * 1024 * 1024)), label: "x", category: "other" })).rejects.toThrow(/5 MB/);
    expect(addMock).not.toHaveBeenCalled();
  });

  it("opens an encrypted document: downloads, decrypts, and says what it is", async () => {
    const cipher = await encryptVault("pw", png, { iterations: 1000 });
    bytesMock.mockResolvedValue({ bytes: cipher, contentType: "application/octet-stream" });
    setPassphrase("pw");
    const doc = await openDocument(entry());
    expect(doc.bytes).toEqual(png);
    expect(doc.mime).toBe("image/png");
    expect(doc.filename).toBe("pan.png");
  });

  it("opening needs the passphrase, and a wrong one fails clearly without showing anything", async () => {
    const cipher = await encryptVault("pw", png, { iterations: 1000 });
    bytesMock.mockResolvedValue({ bytes: cipher, contentType: "application/octet-stream" });
    await expect(openDocument(entry())).rejects.toThrow(/passphrase/);
    setPassphrase("not the passphrase");
    await expect(openDocument(entry())).rejects.toThrow(/wrong passphrase/i);
  });

  it("opens a plain document without a passphrase", async () => {
    bytesMock.mockResolvedValue({ bytes: png, contentType: "image/png" });
    const doc = await openDocument(entry({ encrypted: false, sensitive: false, category: "other", filename: "photo.png" }));
    expect(doc.mime).toBe("image/png");
  });

  it("knows which categories are sensitive", () => {
    expect(["id", "finance"].every((c) => isSensitive(c as never))).toBe(true);
    expect(["resume", "certificate", "other"].some((c) => isSensitive(c as never))).toBe(false);
  });

  it("recognises common document types from their first bytes", () => {
    expect(sniffMime(png)).toBe("image/png");
    expect(sniffMime(Uint8Array.from([0xff, 0xd8, 0xff, 0xe0]))).toBe("image/jpeg");
    expect(sniffMime(new TextEncoder().encode("%PDF-1.7"))).toBe("application/pdf");
    expect(sniffMime(new TextEncoder().encode("RIFF\0\0\0\0WEBPVP8 "))).toBe("image/webp");
    expect(sniffMime(new TextEncoder().encode("plain text"))).toBe("application/octet-stream");
  });
});
