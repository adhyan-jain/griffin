package notify

import (
	"context"
	"testing"
	"time"
)

type mockSender struct {
	called bool
	msg    Message
}

func (m *mockSender) Send(ctx context.Context, msg Message) error {
	m.called = true
	m.msg = msg
	return nil
}

func TestMultiSender(t *testing.T) {
	s1 := &mockSender{}
	s2 := &mockSender{}
	ms := MultiSender{s1, s2}

	msg := Message{Title: "Approval Required", Body: "Please review", Priority: 4}
	err := ms.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s1.called || !s2.called {
		t.Errorf("expected both senders to be called, got s1=%v, s2=%v", s1.called, s2.called)
	}
	if s1.msg.Title != "Approval Required" {
		t.Errorf("expected title 'Approval Required', got %q", s1.msg.Title)
	}
}

func TestDesktopSender_Smoke(t *testing.T) {
	ds := &DesktopSender{}
	// Should not panic or crash even in test environment
	_ = ds.Send(context.Background(), Message{Title: "Test", Body: "Body", Priority: 2})
}

func TestMultiSenderPassesTheDeliveryTimeThrough(t *testing.T) {
	s := &mockSender{}
	at := time.Now().Add(time.Hour)
	if err := (MultiSender{s}).Send(context.Background(), Message{Title: "Reminder", At: at}); err != nil {
		t.Fatal(err)
	}
	if !s.msg.At.Equal(at) || !s.msg.delayed() {
		t.Errorf("At must reach the sender: %v", s.msg.At)
	}
}
