package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// Canonical identity: how Griffin decides that two records, possibly from different sources, are the same
// opportunity. Two records are one listing when they share an employer key and a role key, or a canonical URL, or
// (within one source) an external id.

// employerSuffixes are legal-form and generic company-type words that do not tell two employers apart.
var employerSuffixes = map[string]bool{
	"pvt": true, "private": true, "ltd": true, "limited": true, "inc": true, "incorporated": true,
	"llp": true, "llc": true, "corp": true, "corporation": true, "co": true, "company": true,
	"technologies": true, "technology": true, "gmbh": true, "plc": true, "opc": true,
}

// words splits text into lower-case letter and digit runs ("Acme, Inc." is [acme inc]).
func words(s string) []string {
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// NormalizeEmployer is the key two spellings of one employer share: lower case, no punctuation, no legal or
// generic suffix ("Acme Technologies Pvt. Ltd." and "ACME" are both "acme"). A name that is only suffix words
// keeps them, so it never normalizes to nothing.
func NormalizeEmployer(name string) string {
	w := words(name)
	if len(w) > 1 && w[0] == "the" {
		w = w[1:]
	}
	kept := w
	for len(kept) > 1 && employerSuffixes[kept[len(kept)-1]] {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, " ")
}

var roleWords = map[string]string{
	"internship": "intern", "internships": "intern", "interns": "intern", "intern": "intern",
	"engineering": "engineer", "engineers": "engineer",
	"developers": "developer", "development": "developer",
	"trainee": "trainee",
}

// NormalizeRole is the key two spellings of one role share: lower case, punctuation and spacing collapsed,
// internship/intern/interns one word that goes last, and engineering/engineer one word. The role's meaning is
// kept: "Software Engineer Intern" and "Software Engineering Internship" match; "Backend Intern" does not match
// "Frontend Intern", and "Software Developer" is not "Software Engineer".
func NormalizeRole(title string) string {
	var out []string
	intern := false
	for _, w := range words(title) {
		if n, ok := roleWords[w]; ok {
			w = n
		}
		if w == "intern" {
			intern = true
			continue
		}
		out = append(out, w)
	}
	if intern {
		out = append(out, "intern")
	}
	return strings.Join(out, " ")
}

// trackingParams are query parameters that say where a visitor came from, never which posting it is.
var trackingParams = map[string]bool{
	"ref": true, "source": true, "gh_src": true, "lever-source": true, "lever-origin": true,
	"fbclid": true, "gclid": true, "ref_src": true, "referrer": true, "mc_cid": true, "mc_eid": true,
}

// CanonicalURL is the form of a posting URL that does not change with where the visitor came from: https, lower-case
// host without "www." or a default port, no fragment, no tracking parameters (utm_*, ref, source, gh_src,
// lever-source, ...), remaining parameters sorted, no trailing slash. "" for anything that is not an http(s) URL.
func CanonicalURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host == "" {
		return ""
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		host += ":" + p
	}
	q := url.Values{}
	for k, vs := range u.Query() {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			continue
		}
		q[k] = vs
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	out := "https://" + host + path
	if len(q) > 0 {
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			vs := append([]string(nil), q[k]...)
			sort.Strings(vs)
			for _, v := range vs {
				parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
		}
		out += "?" + strings.Join(parts, "&")
	}
	return out
}

// Identity is everything that says which opportunity a record is.
type Identity struct {
	EmployerKey  string
	RoleKey      string
	CanonicalURL string
	Source       string
	ExternalID   string
}

// NewIdentity derives the identity of a record from its employer name, title, URL and the source's own id.
func NewIdentity(source, externalID, employer, title, link string) Identity {
	return Identity{EmployerKey: NormalizeEmployer(employer), RoleKey: NormalizeRole(title), CanonicalURL: CanonicalURL(link), Source: source, ExternalID: strings.TrimSpace(externalID)}
}

// Same reports whether two identities are one opportunity: the same canonical URL, or the same external id from the
// same source, or the same employer and role.
func (a Identity) Same(b Identity) bool {
	if a.CanonicalURL != "" && a.CanonicalURL == b.CanonicalURL {
		return true
	}
	if a.ExternalID != "" && a.ExternalID == b.ExternalID && a.Source == b.Source {
		return true
	}
	return a.EmployerKey != "" && a.RoleKey != "" && a.EmployerKey == b.EmployerKey && a.RoleKey == b.RoleKey
}

// DescriptionHash fingerprints a description with its spacing normalized; "" for no description, so an unknown
// description stays unknown instead of hashing as empty.
func DescriptionHash(description string) string {
	d := strings.Join(strings.Fields(description), " ")
	if d == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(d))
	return hex.EncodeToString(sum[:])
}
