package hostagent

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/url"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/vault"
)

// VaultAdd uploads one document to the owner's Vault. The host needs the files capability.
func (c *Client) VaultAdd(ctx context.Context, u vault.Upload) (duplicate bool, err error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("label", u.Label)
	_ = mw.WriteField("category", string(u.Category))
	_ = mw.WriteField("fingerprint", u.Fingerprint)
	part, err := mw.CreateFormFile("file", u.Filename)
	if err != nil {
		return false, err
	}
	if _, err := part.Write(u.Data); err != nil {
		return false, err
	}
	if err := mw.Close(); err != nil {
		return false, err
	}
	var out struct {
		Duplicate bool `json:"duplicate"`
	}
	if _, err := c.doBody(ctx, "POST", "/api/v1/host/vault", &body, mw.FormDataContentType(), &out); err != nil {
		return false, err
	}
	return out.Duplicate, nil
}

// VaultEntries lists what the Vault holds (names and fingerprints, never files).
func (c *Client) VaultEntries(ctx context.Context) ([]VaultEntryInfo, error) {
	var out []VaultEntryInfo
	_, err := c.do(ctx, "GET", "/api/v1/host/vault", nil, &out)
	return out, err
}

// VaultEntryInfo is the part of a Vault entry a host needs.
type VaultEntryInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Fingerprint string `json:"fingerprint"`
}

// VaultMisses lists documents that were needed and were not in the Vault.
func (c *Client) VaultMisses(ctx context.Context) ([]domain.VaultMiss, error) {
	var out []domain.VaultMiss
	_, err := c.do(ctx, "GET", "/api/v1/host/vault/misses", nil, &out)
	return out, err
}

// ResolveVaultMiss clears a miss once its document has been added.
func (c *Client) ResolveVaultMiss(ctx context.Context, id string) error {
	_, err := c.do(ctx, "DELETE", "/api/v1/host/vault/misses/"+url.PathEscape(id), nil, nil)
	return err
}
