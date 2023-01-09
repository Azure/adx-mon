package query

type stack []Statement

// IsEmpty: check if stack is empty
func (s *stack) IsEmpty() bool {
	return len(*s) == 0
}

// Push a new value onto the stack
func (s *stack) Push(v Statement) {
	*s = append(*s, v) // Simply append the new value to the end of the stack
}

// Remove and return top element of stack. Return false if stack is empty.
func (s *stack) Pop() (Statement, bool) {
	if s.IsEmpty() {
		return nil, false
	} else {
		index := len(*s) - 1   // Get the index of the top most element.
		element := (*s)[index] // Index into the slice and obtain the element.
		*s = (*s)[:index]      // Remove it from the stack by slicing it off.
		return element, true
	}
}
