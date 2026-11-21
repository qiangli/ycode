package turn

import (
	"encoding/xml"
	"io"
	"strings"

	"github.com/qiangli/ycode/internal/harness/provider"
)

// recoverClaudeDoorInvokes accepts only whole, valid invocation frames for
// the declared script tool. It refuses mixed prose and partial frames and
// leaves execution, authorization and deduplication to the normal tool stage.
func recoverClaudeDoorInvokes(body string) ([]any, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "<invoke ") || len(body) > 256<<10 {
		return nil, false
	}
	decoder := xml.NewDecoder(strings.NewReader(body))
	var calls []any
	for {
		token, err := nextInvokeToken(decoder)
		if err == io.EOF {
			return calls, len(calls) != 0
		}
		start, ok := token.(xml.StartElement)
		if err != nil || !ok || !namedInvokeElement(start, "invoke", provider.ToolName) {
			return nil, false
		}

		token, err = nextInvokeToken(decoder)
		parameter, ok := token.(xml.StartElement)
		if err != nil || !ok || !namedInvokeElement(parameter, "parameter", "script") {
			return nil, false
		}

		script, ok := readInvokeScript(decoder, parameter.Name)
		if !ok {
			return nil, false
		}
		token, err = nextInvokeToken(decoder)
		end, ok := token.(xml.EndElement)
		if err != nil || !ok || end.Name != start.Name {
			return nil, false
		}
		call, ok := textToolCallFrom(map[string]any{
			"name": provider.ToolName, "arguments": map[string]any{"script": script},
		})
		if !ok {
			return nil, false
		}
		calls = append(calls, call)
	}
}

func namedInvokeElement(element xml.StartElement, tag, name string) bool {
	return element.Name == (xml.Name{Local: tag}) && len(element.Attr) == 1 &&
		element.Attr[0] == (xml.Attr{Name: xml.Name{Local: "name"}, Value: name})
}

func nextInvokeToken(decoder *xml.Decoder) (xml.Token, error) {
	for {
		token, err := decoder.Token()
		if chars, ok := token.(xml.CharData); ok && strings.TrimSpace(string(chars)) == "" {
			continue
		}
		return token, err
	}
}

func readInvokeScript(decoder *xml.Decoder, name xml.Name) (string, bool) {
	var script strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", false
		}
		switch token := token.(type) {
		case xml.CharData:
			script.Write(token)
		case xml.EndElement:
			return script.String(), token.Name == name
		default:
			return "", false
		}
	}
}
