package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHabitProfile_NormalizeLingo(t *testing.T) {
	p := DefaultHabitProfile()

	cases := []struct {
		input string
		want  string
	}{
		{"sned a watsapp message to bhai", "send a whatsapp message to bhai"},
		{"snd a msg to mom saying wassup", "send a message to mom saying wassup"},
		{"txt dad saying hellooo", "text dad saying hello"},
		{"wapp message to bhai wassup", "whatsapp message to bhai wassup"},
	}

	for _, tc := range cases {
		got := p.NormalizeLingo(tc.input)
		assert.Equal(t, tc.want, got)
	}
}

func TestHabitProfile_ResolveContact(t *testing.T) {
	p := DefaultHabitProfile()
	assert.Equal(t, "bhai", p.ResolveContact("bhai"))
	assert.Equal(t, "mom", p.ResolveContact("maa"))
	assert.Equal(t, "dad", p.ResolveContact("papa"))
	assert.Equal(t, "Sam", p.ResolveContact("Sam"))
}
