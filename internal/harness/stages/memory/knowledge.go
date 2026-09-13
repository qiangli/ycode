package memory

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
)

// The `bashy kb context --json` envelope is a frozen cross-repo contract:
// the golden copy lives in the umbrella as docs/kb-context-envelope.json and
// byte-identical in testdata/kb-context-envelope.json here. Fields may be
// added upstream but never repurposed or removed, so decoding tolerates
// unknown fields and requires exactly the non-negotiable ones.
const KnowledgeEnvelopeVersion = 1

type KnowledgeEnvelope struct {
	ContextVersion *int             `json:"context_version"`
	For            string           `json:"for"`
	Budget         *KnowledgeBudget `json:"budget"`
	Abstained      *bool            `json:"abstained"`
	Rings          []KnowledgeRing  `json:"rings"`
	Blocks         []KnowledgeBlock `json:"blocks"`
}

type KnowledgeBudget struct {
	Limit int `json:"limit"`
	Used  int `json:"used"`
}

type KnowledgeRing struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type KnowledgeBlock struct {
	Ring       string   `json:"ring"`
	Form       string   `json:"form"`
	Ref        string   `json:"ref"`
	Tokens     int      `json:"tokens"`
	Text       string   `json:"text"`
	Resolution string   `json:"resolution,omitempty"`
	Status     string   `json:"status,omitempty"`
	Why        []string `json:"why,omitempty"`
}

// KnowledgeMessages projects a kb context envelope, as produced by a
// bashy.run node's typed out port, into knowledge-port prompt messages that
// keep per-block ring/form/ref provenance. An abstained envelope yields no
// messages; a malformed one fails closed.
func KnowledgeMessages(value any) ([]ioctx.PromptMessage, error) {
	if messages, ok := value.([]ioctx.PromptMessage); ok {
		return append([]ioctx.PromptMessage(nil), messages...), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("knowledge envelope: %w", err)
	}
	var envelope KnowledgeEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("knowledge envelope: %w", err)
	}
	if envelope.ContextVersion == nil || *envelope.ContextVersion != KnowledgeEnvelopeVersion {
		return nil, fmt.Errorf("knowledge envelope: unsupported context_version %v", envelope.ContextVersion)
	}
	if envelope.Budget == nil || envelope.Abstained == nil {
		return nil, errors.New("knowledge envelope: budget and abstained are required")
	}
	if *envelope.Abstained && len(envelope.Blocks) != 0 {
		return nil, errors.New("knowledge envelope: an abstained assembly must carry no blocks")
	}
	messages := make([]ioctx.PromptMessage, 0, len(envelope.Blocks))
	for position, block := range envelope.Blocks {
		if block.Ring == "" || block.Form == "" || block.Ref == "" || block.Text == "" {
			return nil, fmt.Errorf("knowledge envelope: block %d is missing ring, form, ref or text", position)
		}
		if block.Tokens < 0 {
			return nil, fmt.Errorf("knowledge envelope: block %d has negative tokens", position)
		}
		messages = append(messages, ioctx.PromptMessage{Port: ioctx.PortKnowledge, Role: "system", Content: block.Text, Ring: block.Ring, Form: block.Form, Ref: block.Ref})
	}
	return messages, nil
}
