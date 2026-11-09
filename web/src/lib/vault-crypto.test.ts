// @vitest-environment node
import { describe, expect, it } from "vitest";
import { VaultDecryptError, decryptVault, encryptVault, fromHex, isEncrypted, toHex } from "@/lib/vault-crypto";

// The same vector as internal/vault/crypto_test.go. It was produced by the Go code and independently
// decrypted with Python's PBKDF2 and AES-GCM, so all three agree on the format.
const PASS = "correct horse battery staple";
const SALT = Uint8Array.from({ length: 16 }, (_, i) => i);
const NONCE = Uint8Array.from({ length: 12 }, (_, i) => 0xa0 + i);
const GO_HEX =
  "47564c5431000003e8000102030405060708090a0b0c0d0e0fa0a1a2a3a4a5a6a7a8a9aaab869dfde86d5a41f654fa80d221965d77383468f99bb0f65ce87f04b1d3299c";
const text = (s: string) => new TextEncoder().encode(s);

describe("vault crypto (browser side)", () => {
  it("reads a file the Go side wrote", async () => {
    const plain = await decryptVault(PASS, fromHex(GO_HEX));
    expect(new TextDecoder().decode(plain)).toBe("PAN: ABCDE1234F");
  });

  it("writes exactly the bytes the Go side writes", async () => {
    const enc = await encryptVault(PASS, text("PAN: ABCDE1234F"), { salt: SALT, nonce: NONCE, iterations: 1000 });
    expect(toHex(enc)).toBe(GO_HEX);
  });

  it("round trips with fresh randomness and no plaintext in the output", async () => {
    const msg = text("front and back of the card ".repeat(200));
    const a = await encryptVault("pass phrase", msg, { iterations: 1000 });
    const b = await encryptVault("pass phrase", msg, { iterations: 1000 });
    expect(toHex(a)).not.toBe(toHex(b));
    expect(isEncrypted(a)).toBe(true);
    expect(new TextDecoder().decode(a)).not.toContain("front and back");
    expect(await decryptVault("pass phrase", a)).toEqual(msg);
  });

  it("rejects a wrong passphrase and any changed byte the same way", async () => {
    const enc = fromHex(GO_HEX);
    await expect(decryptVault("wrong", enc)).rejects.toBeInstanceOf(VaultDecryptError);
    for (let i = 0; i < enc.length; i++) {
      const flipped = enc.slice();
      flipped[i] ^= 1;
      await expect(decryptVault(PASS, flipped), `byte ${i}`).rejects.toThrow();
    }
    await expect(decryptVault(PASS, enc.slice(0, enc.length - 1))).rejects.toThrow();
  });

  it("refuses a header that asks for a trivial or absurd cost", async () => {
    for (const iters of [1, 0xffffffff]) {
      const enc = fromHex(GO_HEX);
      new DataView(enc.buffer).setUint32(5, iters, false);
      await expect(decryptVault(PASS, enc)).rejects.toBeInstanceOf(VaultDecryptError);
    }
  });

  it("says plainly when a file is not encrypted, and refuses bad parameters", async () => {
    await expect(decryptVault(PASS, text("just a normal file with no header at all, long enough"))).rejects.toThrow(/not an encrypted/);
    await expect(encryptVault("", text("x"))).rejects.toThrow();
    await expect(encryptVault("p", text("x"), { iterations: 10 })).rejects.toThrow();
    await expect(encryptVault("p", text("x"), { salt: new Uint8Array(4) })).rejects.toThrow();
  });
});
