package mailer

import (
	"context"
	"os"
	"testing"
)

func TestMailer_SendGeneratesReceipt(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mailer_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	m := NewMailer(MailConfig{}, tempDir)
	rec, err := m.Send(context.Background(), "test@example.com", "Test Subject", "Test Body")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if rec.Recipient != "test@example.com" {
		t.Errorf("expected test@example.com, got %s", rec.Recipient)
	}
	if _, err := os.Stat(rec.Receipt); os.IsNotExist(err) {
		t.Errorf("expected receipt file to exist at %s", rec.Receipt)
	}
}
