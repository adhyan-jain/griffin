package vault

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

// KnownAnswer is shared with web/src/lib/vault-crypto.test.ts: both sides must produce and read exactly
// these bytes, which is what makes a file encrypted on the phone readable on the laptop and back.
var (
	kaSalt  = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	kaNonce = []byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab}
)

const (
	kaPassphrase = "correct horse battery staple"
	kaPlaintext  = "PAN: ABCDE1234F"
	kaIterations = 1000
	// kaHex was produced by this package's EncryptWith and is checked in the web test as well.
	kaHex = "47564c5431000003e8000102030405060708090a0b0c0d0e0fa0a1a2a3a4a5a6a7a8a9aaab869dfde86d5a41f654fa80d221965d77383468f99bb0f65ce87f04b1d3299c"
)

func TestKnownAnswerVector(t *testing.T) {
	got, err := EncryptWith(kaPassphrase, []byte(kaPlaintext), kaSalt, kaNonce, kaIterations)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("KNOWN_ANSWER_HEX=%s", hex.EncodeToString(got))
	want := kaHex
	if hex.EncodeToString(got) != want {
		t.Fatalf("the file format changed:\n got %s\nwant %s", hex.EncodeToString(got), want)
	}
	raw, _ := hex.DecodeString(want)
	plain, err := Decrypt(kaPassphrase, raw)
	if err != nil || string(plain) != kaPlaintext {
		t.Fatalf("known vector must decrypt: %q %v", plain, err)
	}
}

func TestRoundTripAndFreshRandomness(t *testing.T) {
	msg := bytes.Repeat([]byte("front and back of the card "), 1000)
	a, err := Encrypt("pass phrase", msg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Encrypt("pass phrase", msg)
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same data must differ (fresh salt and nonce)")
	}
	if !IsEncrypted(a) || bytes.Contains(a, []byte("front and back")) {
		t.Fatal("ciphertext must carry the header and no plaintext")
	}
	got, err := Decrypt("pass phrase", a)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("round trip failed: %v", err)
	}
	empty, _ := Encrypt("p", nil)
	if got, err := Decrypt("p", empty); err != nil || len(got) != 0 {
		t.Fatalf("empty files must round trip: %v", err)
	}
}

func TestWrongPassphraseAndTamperingAreRejectedIdentically(t *testing.T) {
	enc, _ := EncryptWith("right", []byte("secret"), kaSalt, kaNonce, kaIterations)
	if _, err := Decrypt("wrong", enc); !errors.Is(err, ErrDecrypt) {
		t.Errorf("wrong passphrase: %v", err)
	}
	for i := range enc {
		flipped := append([]byte(nil), enc...)
		flipped[i] ^= 0x01
		if got, err := Decrypt("right", flipped); err == nil {
			t.Fatalf("flipping byte %d must not decrypt (got %q); every byte, header included, is authenticated", i, got)
		}
	}
	if _, err := Decrypt("right", enc[:len(enc)-1]); err == nil {
		t.Error("a truncated file must not decrypt")
	}
	if _, err := Decrypt("right", []byte("just a normal file")); !errors.Is(err, ErrNotEncrypted) {
		t.Errorf("plain data: %v", err)
	}
}

func TestHeaderCannotWeakenTheKey(t *testing.T) {
	enc, _ := EncryptWith("right", []byte("secret"), kaSalt, kaNonce, kaIterations)
	weak := append([]byte(nil), enc...)
	weak[5], weak[6], weak[7], weak[8] = 0, 0, 0, 1 // iterations = 1
	if _, err := Decrypt("right", weak); !errors.Is(err, ErrDecrypt) {
		t.Errorf("a header asking for one iteration must be refused, got %v", err)
	}
	huge := append([]byte(nil), enc...)
	huge[5], huge[6], huge[7], huge[8] = 0xff, 0xff, 0xff, 0xff
	if _, err := Decrypt("right", huge); !errors.Is(err, ErrDecrypt) {
		t.Errorf("a header asking for billions of iterations must be refused, got %v", err)
	}
}

func TestEncryptRejectsBadParameters(t *testing.T) {
	if _, err := Encrypt("", []byte("x")); err == nil {
		t.Error("an empty passphrase must be refused")
	}
	if _, err := EncryptWith("p", nil, kaSalt[:8], kaNonce, kaIterations); err == nil {
		t.Error("short salt")
	}
	if _, err := EncryptWith("p", nil, kaSalt, kaNonce[:4], kaIterations); err == nil {
		t.Error("short nonce")
	}
	if _, err := EncryptWith("p", nil, kaSalt, kaNonce, 10); err == nil {
		t.Error("too few iterations")
	}
}
