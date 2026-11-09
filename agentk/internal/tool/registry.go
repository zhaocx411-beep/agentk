package tool

import "fmt"

// Registry 是构造后不可变的工具集合。
type Registry struct {
	byName map[string]Spec
	// order 保持提供给模型的工具顺序稳定，也有利于 prompt cache 命中。
	order []string
}

func NewRegistry(specs ...Spec) (*Registry, error) {
	r := &Registry{byName: make(map[string]Spec, len(specs))}
	for _, s := range specs {
		if s.Name == "" {
			return nil, fmt.Errorf("tool: spec with empty name")
		}
		if s.Binding == nil {
			return nil, fmt.Errorf("tool: spec %q has no execution binding", s.Name)
		}
		if ab, ok := s.Binding.(ActivityBinding); ok && ab.ActivityName == "" {
			return nil, fmt.Errorf("tool: spec %q activity binding requires activityName", s.Name)
		}
		if wb, ok := s.Binding.(WorkflowBinding); ok {
			if wb.WorkflowType == "" {
				return nil, fmt.Errorf("tool: spec %q workflow binding requires workflowType", s.Name)
			}
		}
		if _, dup := r.byName[s.Name]; dup {
			return nil, fmt.Errorf("tool: duplicate tool name %q", s.Name)
		}
		r.byName[s.Name] = s
		r.order = append(r.order, s.Name)
	}
	return r, nil
}

func (r *Registry) Get(name string) (Spec, bool) {
	s, ok := r.byName[name]
	return s, ok
}

// Specs 按注册顺序返回工具契约。
func (r *Registry) Specs() []Spec {
	out := make([]Spec, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.byName[name])
	}
	return out
}
