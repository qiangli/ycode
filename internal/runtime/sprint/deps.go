package sprint

import "fmt"

// TaskDep manages dependency relationships between sprint tasks.
// Inspired by ClawTeam's blocked_by chains with auto-unblocking and
// ruflo's dependency DAG with forward/backward tracking.
//
// When a task completes, all tasks that depend on it are automatically
// unblocked if they have no remaining dependencies.

// DepGraph tracks blocked-by and blocks relationships between tasks.
type DepGraph struct {
	// blockedBy maps taskID → set of task IDs it depends on.
	blockedBy map[string]map[string]bool
	// blocks maps taskID → set of task IDs that depend on it.
	blocks map[string]map[string]bool
}

// NewDepGraph creates an empty dependency graph.
func NewDepGraph() *DepGraph {
	return &DepGraph{
		blockedBy: make(map[string]map[string]bool),
		blocks:    make(map[string]map[string]bool),
	}
}

// AddDep declares that taskID is blocked by depID.
// Returns error if the dependency would create a cycle.
func (g *DepGraph) AddDep(taskID, depID string) error {
	if taskID == depID {
		return fmt.Errorf("task %q cannot depend on itself", taskID)
	}
	// Check for cycles: if depID transitively depends on taskID, adding
	// taskID→depID would create a cycle.
	if g.transitivelyDependsOn(depID, taskID) {
		return fmt.Errorf("adding dependency %s→%s would create a cycle", taskID, depID)
	}

	if g.blockedBy[taskID] == nil {
		g.blockedBy[taskID] = make(map[string]bool)
	}
	g.blockedBy[taskID][depID] = true

	if g.blocks[depID] == nil {
		g.blocks[depID] = make(map[string]bool)
	}
	g.blocks[depID][taskID] = true

	return nil
}

// transitivelyDependsOn checks if fromID transitively depends on targetID.
func (g *DepGraph) transitivelyDependsOn(fromID, targetID string) bool {
	visited := make(map[string]bool)
	queue := []string{fromID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == targetID {
			return true
		}
		if visited[current] {
			continue
		}
		visited[current] = true
		for dep := range g.blockedBy[current] {
			queue = append(queue, dep)
		}
	}
	return false
}

// MarkComplete marks a task as completed and returns the IDs of tasks
// that were unblocked as a result (transitioned from blocked to ready).
func (g *DepGraph) MarkComplete(taskID string) []string {
	dependents := g.blocks[taskID]
	var unblocked []string

	for depID := range dependents {
		if deps, ok := g.blockedBy[depID]; ok {
			delete(deps, taskID)
			if len(deps) == 0 {
				delete(g.blockedBy, depID)
				unblocked = append(unblocked, depID)
			}
		}
	}

	delete(g.blocks, taskID)
	delete(g.blockedBy, taskID)

	return unblocked
}

// IsBlocked returns true if the task has unresolved dependencies.
func (g *DepGraph) IsBlocked(taskID string) bool {
	deps, ok := g.blockedBy[taskID]
	return ok && len(deps) > 0
}

// BlockedBy returns the set of task IDs that block the given task.
func (g *DepGraph) BlockedBy(taskID string) []string {
	deps := g.blockedBy[taskID]
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}
	return result
}

// Dependents returns the set of task IDs that depend on the given task.
func (g *DepGraph) Dependents(taskID string) []string {
	deps := g.blocks[taskID]
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}
	return result
}

// ReadyTasks returns tasks that have no unresolved dependencies from the
// given candidate set. Useful for finding parallelizable work.
func (g *DepGraph) ReadyTasks(candidates []string) []string {
	var ready []string
	for _, id := range candidates {
		if !g.IsBlocked(id) {
			ready = append(ready, id)
		}
	}
	return ready
}
