package hands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

const maxExtractBytes = 64 << 10

// ErrNoAction means no earlier step produced a messaging action.
var ErrNoAction = errors.New("no messaging action found in the earlier steps")

// ActionFromDependencies finds the messaging action an earlier agent step wrote. Such a step is asked to
// reply with a single JSON object; models still wrap it in prose or a code fence, so every top-level JSON
// object in the reply is considered and the first one that has an "op" must validate. Later steps win.
// The result is untrusted text turned into an action: the permission gateway and the owner's approval
// still stand between it and anything being sent.
func ActionFromDependencies(deps []repository.DependencyOutput) (*domain.MessagingAction, error) {
	for i := len(deps) - 1; i >= 0; i-- {
		text := outputText(deps[i].Output)
		if len(text) > maxExtractBytes {
			text = text[:maxExtractBytes]
		}
		if act, err := scanForAction(text); act != nil || err != nil {
			return act, err
		}
	}
	return nil, ErrNoAction
}

func outputText(out json.RawMessage) string {
	var env struct {
		Response *string `json:"response"`
	}
	if json.Unmarshal(out, &env) == nil && env.Response != nil {
		return *env.Response
	}
	return string(out)
}

func scanForAction(text string) (*domain.MessagingAction, error) {
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var obj map[string]json.RawMessage
		if err := dec.Decode(&obj); err != nil {
			continue
		}
		// Accept {"messaging": {...}} as well as the bare action.
		if inner, ok := obj[domain.MessagingKey]; ok {
			var nested map[string]json.RawMessage
			if json.Unmarshal(inner, &nested) == nil {
				obj = nested
			}
		}
		if _, ok := obj["op"]; !ok {
			i += int(dec.InputOffset()) - 1
			continue
		}
		raw, _ := json.Marshal(obj)
		var act domain.MessagingAction
		if err := json.Unmarshal(bytes.TrimSpace(raw), &act); err != nil {
			return nil, fmt.Errorf("the earlier step's messaging action is not valid: %w", err)
		}
		if err := act.Validate(); err != nil {
			return nil, fmt.Errorf("the earlier step's messaging action is not valid: %w", err)
		}
		return &act, nil
	}
	return nil, nil
}
