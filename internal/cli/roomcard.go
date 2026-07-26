package cli

import (
	"os"
	"strings"

	"github.com/qiangli/coreutils/pkg/room"

	"github.com/qiangli/ycode/internal/api"
)

// joinRoom publishes a membership card so the rest of the fleet can SEE this
// session, and returns the function that withdraws it.
//
// The dividend is the card ID. ycode's session id is already its --fork handle
// and is exported as YCODE_SESSION_ID, so a row in `bashy chat sessions`
// becomes directly actionable: `ycode --fork <id>` resumes exactly that
// conversation. No other tool in the fleet offers that.
//
// Everything here is best-effort. Discovery is a convenience, and a session
// that cannot advertise itself must still start — so no error from this path
// is ever fatal.
func (a *App) joinRoom() (leave func()) {
	if launchedByBashy() {
		// bashy already published a card for this process when it launched us
		// (chat/interact.go). A second one would show the same session twice
		// under two ids, and the duplicate would outlive nothing.
		return func() {}
	}

	card := room.Card{
		ID:     "ycode-" + a.SessionID(),
		Tool:   "ycode",
		Model:  a.resolvedModel(),
		Mode:   "interactive",
		PID:    os.Getpid(),
		Cwd:    a.workDir,
		Native: true,
		// Exactly what ycode.yaml declares, and nothing aspirational — a
		// capability advertised here is one another agent may try to use.
		Caps: []string{"events", "fork", "steer", "shell", "tools"},

		// DELIBERATELY EMPTY. ycode has no control socket, so nothing can
		// reach in and steer it. Advertising one would put a card in the room
		// that `bashy chat steer` would dial and fail on; empty makes the
		// failure honest and immediate instead of mysterious.
		CtlSock: "",
	}
	if agent, ok := api.YcodeAgentForModel(card.Model); ok {
		card.Nick = agent.Nick
		card.Band = agent.Band
		card.Binding = agent.Binding
	} else if card.Model != "" {
		card.Binding = "ycode:" + card.Model
	}

	if err := room.Join(card); err != nil {
		return func() {}
	}
	return func() { room.Leave(card.ID) }
}

// launchedByBashy reports whether this ycode was started by `bashy chat`,
// which publishes its own card for the session.
//
// The tell is the PID: agentpty execs the child from the bashy process, so
// bashy's card carries our PARENT's pid. BASHY_PRINCIPAL cannot be used for
// this — native harnesses are handed os.Environ() verbatim and never see it.
func launchedByBashy() bool {
	members, err := room.Members()
	if err != nil {
		return false
	}
	ppid := os.Getppid()
	for _, m := range members {
		if strings.EqualFold(m.Tool, "ycode") && m.PID == ppid {
			return true
		}
	}
	return false
}
