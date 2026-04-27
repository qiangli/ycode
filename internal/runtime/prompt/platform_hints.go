package prompt

// PlatformHints maps channel type identifiers to formatting guidance.
var PlatformHints = map[string]string{
	"telegram": "The user is on Telegram. Use Telegram MarkdownV2 for formatting. Limit messages to 4096 characters. Avoid complex nested formatting.",
	"discord":  "The user is on Discord. Use Discord markdown for formatting. Code blocks use triple backticks. Limit messages to 2000 characters.",
	"slack":    "The user is on Slack. Use Slack mrkdwn syntax (not standard Markdown). Bold uses *text*, italic uses _text_, code uses `text`. No nested formatting.",
	"matrix":   "The user is on Matrix. Use standard Markdown. Messages support rich formatting including tables.",
	"email":    "The user is communicating via email. Use plain text or simple HTML formatting. Be more formal in tone. Include a clear subject-line-appropriate summary at the top.",
	"whatsapp": "The user is on WhatsApp. Do NOT use Markdown — use plain text only. Bold uses *text*, italic uses _text_. Keep messages concise. Limit to 65536 characters.",
	"web":      "The user is on a web interface. Standard Markdown is supported including code blocks, tables, and links.",
	"cli":      "", // no extra hints needed for CLI
}

// PlatformHintsSection returns the prompt section for platform-specific formatting.
// Returns empty string if the channel has no special hints.
func PlatformHintsSection(channelType string) string {
	hint, ok := PlatformHints[channelType]
	if !ok || hint == "" {
		return ""
	}
	return "# Platform\n" + hint
}
