package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// jwksFailureBackoff is how long after a failed refresh we refuse to hit the
// JWKS endpoint again, so unknown-kid tokens cannot amplify into outbound fetches.
const jwksFailureBackoff = 30 * time.Second

// JWKSCache fetches and caches Supabase asymmetric (ES256/RS256) public keys.
type JWKSCache struct {
	url    string
	client *http.Client
	ttl    time.Duration

	mu        sync.Mutex
	keys      map[string]any
	fetchedAt time.Time
	failedAt  time.Time
	now       func() time.Time
}

// NewJWKSCache creates a cache for the given JWKS URL.
func NewJWKSCache(url string, client *http.Client) *JWKSCache {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &JWKSCache{url: url, client: client, ttl: 10 * time.Minute, now: time.Now}
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// Key returns the public key for kid, refreshing at most once per minute on a miss.
func (c *JWKSCache) Key(kid string) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if k, ok := c.keys[kid]; ok && time.Since(c.fetchedAt) < c.ttl {
		return k, nil
	}
	if c.keys != nil && time.Since(c.fetchedAt) < time.Minute {
		if k, ok := c.keys[kid]; ok {
			return k, nil
		}
		return nil, fmt.Errorf("unknown signing key id %q", kid)
	}
	if !c.failedAt.IsZero() && c.now().Sub(c.failedAt) < jwksFailureBackoff {
		if k, ok := c.keys[kid]; ok { // stale key still usable during backoff
			return k, nil
		}
		return nil, fmt.Errorf("jwks refresh suppressed after recent failure; unknown signing key id %q", kid)
	}
	if err := c.refreshLocked(); err != nil {
		c.failedAt = c.now()
		if k, ok := c.keys[kid]; ok { // serve stale key if refresh fails
			return k, nil
		}
		return nil, err
	}
	k, ok := c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("unknown signing key id %q", kid)
	}
	return k, nil
}

func (c *JWKSCache) refreshLocked() error {
	resp, err := c.client.Get(c.url)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch jwks: status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}
	keys := make(map[string]any, len(doc.Keys))
	for _, k := range doc.Keys {
		pub, err := k.public()
		if err != nil || k.Kid == "" {
			continue
		}
		keys[k.Kid] = pub
	}
	c.keys = keys
	c.fetchedAt = time.Now()
	c.failedAt = time.Time{}
	return nil
}

func (k jwk) public() (any, error) {
	switch k.Kty {
	case "EC":
		if k.Crv != "P-256" {
			return nil, errors.New("unsupported curve")
		}
		x, err1 := b64Int(k.X)
		y, err2 := b64Int(k.Y)
		if err1 != nil || err2 != nil {
			return nil, errors.New("bad EC coordinates")
		}
		if !elliptic.P256().IsOnCurve(x, y) {
			return nil, errors.New("point not on curve")
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
	case "RSA":
		n, err1 := b64Int(k.N)
		e, err2 := b64Int(k.E)
		if err1 != nil || err2 != nil || !e.IsInt64() {
			return nil, errors.New("bad RSA key")
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
	}
	return nil, errors.New("unsupported key type")
}

func b64Int(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(b), nil
}
