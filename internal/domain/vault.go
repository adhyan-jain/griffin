package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// VaultKind marks an artifact as a Vault document (artifacts already carry a free-form metadata kind).
const VaultKind = "vault"

// VaultCategory groups documents by how carefully they are handled.
type VaultCategory string

const (
	VaultID          VaultCategory = "id"          // PAN, Aadhaar, passport, licence
	VaultFinance     VaultCategory = "finance"     // bank and tax papers
	VaultResume      VaultCategory = "resume"      // resumes and the career context
	VaultCertificate VaultCategory = "certificate" // marksheets, certificates, offer letters
	VaultOther       VaultCategory = "other"
)

// Valid reports whether c is a known category.
func (c VaultCategory) Valid() bool {
	switch c {
	case VaultID, VaultFinance, VaultResume, VaultCertificate, VaultOther:
		return true
	}
	return false
}

// Sensitive categories must reach the cloud encrypted on the device. The server refuses them otherwise.
func (c VaultCategory) Sensitive() bool { return c == VaultID || c == VaultFinance }

// VaultMeta is stored in the artifact's metadata.
type VaultMeta struct {
	Kind      string        `json:"kind"`
	Label     string        `json:"label"`
	Category  VaultCategory `json:"category"`
	Sensitive bool          `json:"sensitive"`
	Encrypted bool          `json:"encrypted"`
	// Source is where it was added from: "web", "phone" or "laptop".
	Source string `json:"source,omitempty"`
	// Fingerprint is the SHA-256 of the original (plaintext) file, so a host that mirrors a folder again does
	// not upload a document twice. Encrypted copies differ every time, so the ciphertext cannot serve.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// ParseVaultMeta reads Vault metadata from an artifact; ok is false for any other artifact.
func ParseVaultMeta(raw json.RawMessage) (VaultMeta, bool) {
	var m VaultMeta
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil || m.Kind != VaultKind {
		return VaultMeta{}, false
	}
	return m, true
}

// NormalizeVaultLabel trims and checks a label such as "PAN card".
func NormalizeVaultLabel(raw string) (string, error) {
	label := strings.Join(strings.Fields(raw), " ")
	if label == "" {
		return "", fmt.Errorf("%w: a document needs a label, for example \"PAN card\"", ErrValidation)
	}
	if len([]rune(label)) > 80 {
		return "", fmt.Errorf("%w: the label is at most 80 characters", ErrValidation)
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: the label may not contain control characters", ErrValidation)
		}
	}
	return label, nil
}

// VaultMiss is a document that was needed and was not there. The next time a device that has it is on, it
// can offer to add it.
type VaultMiss struct {
	ID     string `json:"id"`
	Query  string `json:"query"`
	TaskID string `json:"task_id,omitempty"`
	Count  int    `json:"count"`
	At     string `json:"at"`
}
