package domain

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio's signature scheme is HMAC-SHA1; it is not our choice
	"encoding/base64"
	"encoding/xml"
	"sort"
	"strings"
)

// TwilioSignature computes the X-Twilio-Signature for a request: the full URL followed by every POST
// parameter as name+value in name order, signed with HMAC-SHA1 under the account's auth token.
func TwilioSignature(authToken, fullURL string, params map[string][]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(fullURL)
	for _, k := range keys {
		vals := append([]string(nil), params[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			sb.WriteString(k)
			sb.WriteString(v)
		}
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(sb.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ValidTwilioSignature reports whether got is the right signature, comparing in constant time. An empty token
// or signature is never valid, so an unconfigured server accepts nothing.
func ValidTwilioSignature(authToken, fullURL string, params map[string][]string, got string) bool {
	if authToken == "" || got == "" {
		return false
	}
	return hmac.Equal([]byte(TwilioSignature(authToken, fullURL, params)), []byte(got))
}

func xmlEscape(s string) string {
	var sb strings.Builder
	_ = xml.EscapeText(&sb, []byte(s))
	return sb.String()
}

const twimlHead = `<?xml version="1.0" encoding="UTF-8"?><Response>`

// TwiMLDial forwards the call to the owner's number.
func TwiMLDial(number string) string {
	return twimlHead + `<Dial timeout="25">` + xmlEscape(number) + `</Dial></Response>`
}

// TwiMLScreen greets the caller, asks who they are and why they call, and posts the spoken answer to action.
func TwiMLScreen(ownerName, action string) string {
	return twimlHead + `<Say>Hello, you have reached ` + xmlEscape(ownerName) + `'s assistant. Please say your name and why you are calling.</Say>` +
		`<Gather input="speech" speechTimeout="auto" timeout="6" action="` + xmlEscape(action) + `" method="POST"></Gather>` +
		`<Say>I did not hear anything. Goodbye.</Say><Hangup/></Response>`
}

// TwiMLThanks ends a screened call politely.
func TwiMLThanks(ownerName string) string {
	return twimlHead + `<Say>Thank you. I will pass your message to ` + xmlEscape(ownerName) + `. Goodbye.</Say><Hangup/></Response>`
}

// SameNumber compares phone numbers by their digits, so "+91 98765-43210" equals "+919876543210".
func SameNumber(a, b string) bool {
	da, db := digits(a), digits(b)
	return len(da) >= 7 && da == db
}

func digits(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
