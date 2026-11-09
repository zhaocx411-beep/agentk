// Package target provides the implementation behind a public alias.
package target

// Subject is exported through another package as a type alias.
type Subject struct{}

// Value is a value-receiver method.
func (Subject) Value() string {
	return "value"
}

// Pointer is a pointer-receiver method.
func (*Subject) Pointer() string {
	return "pointer"
}

func (*Subject) hidden() string {
	return "hidden"
}
