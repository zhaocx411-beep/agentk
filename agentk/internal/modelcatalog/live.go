package modelcatalog

import (
	"sync/atomic"
)

type Active struct {
	ptr atomic.Pointer[Catalog]
}

func NewActive(catalog *Catalog) *Active {
	a := &Active{}
	a.ptr.Store(catalog)
	return a
}

func (a *Active) Current() *Catalog {
	if a == nil {
		return nil
	}
	return a.ptr.Load()
}

func (a *Active) Swap(catalog *Catalog) {
	a.ptr.Store(catalog)
}
