package v0

import "fmt"

// typesByName maps an API type's reflect.Type.String() form (e.g.
// "v0.KubernetesRuntimeInstance") to a factory that returns a fresh
// zero-valued instance. Populated by codegen-emitted init() blocks.
var typesByName = map[string]func() interface{}{}

// RegisterTypeFactory registers a factory for an API type by its
// reflect.Type.String() name. Called from generated init() blocks in
// core threeport and in modules.
func RegisterTypeFactory(name string, factory func() interface{}) {
	typesByName[name] = factory
}

// newByObjectTypeName returns a fresh instance of the type named by
// typeKey, or an error if no factory is registered.
func newByObjectTypeName(typeKey string) (interface{}, error) {
	factory, ok := typesByName[typeKey]
	if !ok {
		return nil, fmt.Errorf("no factory registered for type %q", typeKey)
	}
	return factory(), nil
}
