package swarm

import (
	"fmt"
	"os"
	"path/filepath"
)

// MailboxBridge connects the in-process event bus to file-based mailboxes
// for cross-process agent communication.
type MailboxBridge struct {
	baseDir string // e.g., ~/.agents/ycode/teams/
}

// NewMailboxBridge creates a bridge rooted at the given directory.
func NewMailboxBridge(baseDir string) *MailboxBridge {
	return &MailboxBridge{baseDir: baseDir}
}

// MailboxFor returns the mailbox for a specific agent in a team.
func (b *MailboxBridge) MailboxFor(teamID, agentID string) (*Mailbox, error) {
	dir := filepath.Join(b.baseDir, teamID, "agents", agentID, "inbox")
	return NewMailbox(dir)
}

// SendToMailbox delivers a message to an agent's file-based mailbox.
func (b *MailboxBridge) SendToMailbox(teamID, agentID string, msg MailboxMessage) error {
	mb, err := b.MailboxFor(teamID, agentID)
	if err != nil {
		return fmt.Errorf("get mailbox: %w", err)
	}
	return mb.Send(msg)
}

// ReceiveFromMailbox reads the next message from an agent's mailbox.
func (b *MailboxBridge) ReceiveFromMailbox(teamID, agentID string) (*MailboxMessage, error) {
	mb, err := b.MailboxFor(teamID, agentID)
	if err != nil {
		return nil, fmt.Errorf("get mailbox: %w", err)
	}
	return mb.Receive()
}

// DefaultBaseDir returns the default mailbox base directory.
func DefaultBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/ycode/teams"
	}
	return filepath.Join(home, ".agents", "ycode", "teams")
}
