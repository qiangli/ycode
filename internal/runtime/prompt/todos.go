package prompt

// TodoBoardRenderer is the minimal contract the prompt builder needs to
// inject the agent-facing todo board. internal/runtime/todo.Board already
// satisfies it; keeping the dependency one-way (todo → prompt only via
// interface satisfaction) lets stable-tier builds ship without the
// experimental write_todos tool wired up — a nil board renders empty.
type TodoBoardRenderer interface {
	RenderMarkdown() string
}

// TodosSection returns the rendered todo board for injection into the
// dynamic (below-cache-boundary) region of the system prompt. The board's
// own RenderMarkdown returns "" when empty, so this is a zero-cost
// section on turns where the agent has not (yet) populated it.
func TodosSection(board TodoBoardRenderer) string {
	if board == nil {
		return ""
	}
	return board.RenderMarkdown()
}
