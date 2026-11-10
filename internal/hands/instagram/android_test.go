package instagram

import (
	"context"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockADB struct {
	state string
	pkgs  string
	cmds  []string
}

func (m *mockADB) Run(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	m.cmds = append(m.cmds, args[0])
	if args[0] == "get-state" {
		return m.state, nil
	}
	if len(args) > 2 && args[0] == "shell" && args[1] == "pm" {
		return m.pkgs, nil
	}
	return "ok", nil
}

func TestInstagramAndroid_DoSend(t *testing.T) {
	adb := &mockADB{
		state: "device",
		pkgs:  "package:com.instagram.android",
	}
	d := NewAndroid(AndroidConfig{
		ADB: adb,
	})

	assert.Equal(t, "instagram-android", d.Name())
	assert.Equal(t, "phone", d.Device())

	act := &domain.MessagingAction{
		Channel: "instagram",
		Op:      domain.MessagingSend,
		Contact: "sam_lee",
		Text:    "hello from griffin",
	}

	res, err := d.Do(context.Background(), act)
	require.NoError(t, err)
	assert.True(t, res.Sent)
	assert.Equal(t, "instagram-android", res.Via)
}

func TestInstagramAndroid_RejectsOtherChannel(t *testing.T) {
	adb := &mockADB{state: "device", pkgs: "package:com.instagram.android"}
	d := NewAndroid(AndroidConfig{ADB: adb})

	act := &domain.MessagingAction{
		Channel: "whatsapp",
		Op:      domain.MessagingSend,
		Contact: "sam",
		Text:    "hi",
	}

	_, err := d.Do(context.Background(), act)
	require.Error(t, err)
}
