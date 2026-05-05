package memory

import "time"

// SupersedeMemory marks an old memory as superseded by a new one.
// Sets ValidUntil on the old memory and SupersededBy pointing to the new memory's name.
func SupersedeMemory(old *Memory, newName string) {
	now := time.Now()
	old.ValidUntil = &now
	old.SupersededBy = newName
}

// IsValid returns true if the memory is currently within its validity window.
// A memory with no validity constraints is always valid.
func IsValid(mem *Memory) bool {
	now := time.Now()
	if mem.ValidFrom != nil && now.Before(*mem.ValidFrom) {
		return false
	}
	if mem.ValidUntil != nil && now.After(*mem.ValidUntil) {
		return false
	}
	return true
}

// IsSuperseded returns true if this memory has been replaced by another.
func IsSuperseded(mem *Memory) bool {
	return mem.SupersededBy != ""
}
