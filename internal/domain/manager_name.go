package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// DefaultManagerName is what the top manager is called until the owner picks another name.
const DefaultManagerName = "JJ"

// ManagerNameKey is where the name lives: in a user's profile settings, and in a task's input_payload
// so a worker can speak as that manager without looking anything up.
const ManagerNameKey = "manager_name"

const maxManagerNameRunes = 30

// NormalizeManagerName trims and checks a name the owner chose. The name is spoken, shown and put into
// prompts, so it is limited to letters, digits, spaces, hyphens and apostrophes: nothing that could
// carry an instruction into a prompt.
func NormalizeManagerName(raw string) (string, error) {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: the name may not contain line breaks or control characters", ErrValidation)
		}
	}
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" {
		return "", fmt.Errorf("%w: the manager needs a name", ErrValidation)
	}
	if len([]rune(name)) > maxManagerNameRunes {
		return "", fmt.Errorf("%w: the name is at most %d characters", ErrValidation, maxManagerNameRunes)
	}
	for _, r := range name {
		// Marks are part of many scripts (the vowel signs in Devanagari are marks, not letters).
		if !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsDigit(r) && r != ' ' && r != '-' && r != '\'' {
			return "", fmt.Errorf("%w: the name may use letters, digits, spaces, hyphens and apostrophes only", ErrValidation)
		}
	}
	return name, nil
}

// ManagerNameFrom reads the manager's name from a task payload and falls back to the default when there
// is none or it is not valid, so a bad stored value can never reach a prompt.
func ManagerNameFrom(payload json.RawMessage) string {
	var v struct {
		Name string `json:"manager_name"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &v) != nil {
		return DefaultManagerName
	}
	if name, err := NormalizeManagerName(v.Name); err == nil {
		return name
	}
	return DefaultManagerName
}

// WithManagerName returns payload with the manager's name set. A payload that is not a JSON object is
// returned unchanged.
func WithManagerName(payload json.RawMessage, name string) json.RawMessage {
	var m map[string]json.RawMessage
	if len(payload) == 0 {
		m = map[string]json.RawMessage{}
	} else if json.Unmarshal(payload, &m) != nil || m == nil {
		return payload
	}
	encoded, _ := json.Marshal(name)
	m[ManagerNameKey] = encoded
	out, err := json.Marshal(m)
	if err != nil {
		return payload
	}
	return out
}
