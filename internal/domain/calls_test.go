package domain

import (
	"strings"
	"testing"
)

// The worked example from Twilio's own documentation for request validation.
func TestTwilioSignatureMatchesTheDocumentedExample(t *testing.T) {
	params := map[string][]string{
		"CallSid": {"CA1234567890ABCDE"}, "Caller": {"+14158675310"}, "Digits": {"1234"},
		"From": {"+14158675310"}, "To": {"+18005551212"},
	}
	got := TwilioSignature("12345", "https://mycompany.com/myapp.php?foo=1&bar=2", params)
	if got != "GvWf1cFY/Q7PnoempGyD5oXAezc=" {
		t.Fatalf("signature %q does not match Twilio's documented example", got)
	}
	if !ValidTwilioSignature("12345", "https://mycompany.com/myapp.php?foo=1&bar=2", params, got) {
		t.Fatal("must accept the right signature")
	}
	if ValidTwilioSignature("12345", "https://mycompany.com/other", params, got) {
		t.Fatal("a different URL must fail")
	}
	params["Digits"] = []string{"9999"}
	if ValidTwilioSignature("12345", "https://mycompany.com/myapp.php?foo=1&bar=2", params, got) {
		t.Fatal("a changed parameter must fail")
	}
	if ValidTwilioSignature("", "u", nil, "x") || ValidTwilioSignature("t", "u", nil, "") {
		t.Fatal("empty token or signature is never valid")
	}
}

func TestTwiMLEscapesAndShapes(t *testing.T) {
	s := TwiMLScreen("A<b>&Co", "https://x/y?a=1&b=2")
	if strings.Contains(s, "<b>") || !strings.Contains(s, "a=1&amp;b=2") || !strings.Contains(s, `input="speech"`) {
		t.Fatal(s)
	}
	if !strings.Contains(TwiMLDial("+911234567890"), "<Dial") {
		t.Fatal("dial")
	}
}

func TestSameNumber(t *testing.T) {
	if !SameNumber("+91 98765-43210", "+919876543210") || SameNumber("+919876543210", "+919876543211") || SameNumber("", "") {
		t.Fatal("number matching")
	}
}
