// Package fixture exercises the public Go documentation model.
package fixture

import "context"

// These constants are kept in one declaration group.
const (
	First  = 1
	Second = 2
	hidden = 3
)

// These variables are kept in one declaration group.
var (
	Exported = 1
	hiddenV  = 2
)

// Pair stores two values of the same type.
type Pair[T any] struct {
	// Left is the first value.
	Left T

	// Right is the second value.
	Right T

	hidden T
}

// NewPair constructs a Pair.
func NewPair[T any](left, right T) *Pair[T] {
	return &Pair[T]{Left: left, Right: right}
}

// Value returns the first value.
func (p *Pair[T]) Value() T {
	return p.Left
}

func (p *Pair[T]) hiddenValue() T {
	return p.hidden
}

// Contract describes an exported operation.
type Contract interface {
	// Apply performs the operation.
	Apply(string) error

	hiddenApply()
}

type (
	// Grouped is declared inside a grouped type declaration.
	Grouped struct{}
)

// Standalone is not associated with a declared type.
func Standalone(ctx context.Context, input string, pairs ...*Pair[int]) (string, error) {
	_ = ctx
	_ = pairs
	return input, nil
}
