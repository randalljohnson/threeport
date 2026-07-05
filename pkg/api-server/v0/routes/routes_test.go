package routes

import (
	"testing"

	echo "github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	handlers "github.com/threeport/threeport/pkg/api-server/v0/handlers"
	v0 "github.com/threeport/threeport/pkg/api/v0"
)

// TestCustomRoutes_ReturnsExpectedRoutes covers CustomRoutes()'s enumeration of
// custom routes, asserting the path, method, handler presence, id-param
// handler presence, and the ApiObjects (name and version) attached to each
// entry match the intended registration set.
func TestCustomRoutes_ReturnsExpectedRoutes(t *testing.T) {
	// build a zero-value handler; CustomRoutes only takes function addresses
	h := &handlers.Handler{}

	// invoke the function under test
	routes := CustomRoutes(h)
	require.NotNil(t, routes)
	require.Len(t, *routes, 4, "expected four custom routes")

	// index each route by path so the assertions read by domain, not order
	byPath := map[string]CustomRoute{}
	for _, r := range *routes {
		byPath[r.Path] = r
	}

	// bulk kubernetes workload resource definition route: POST with a
	// primary handler, no id-param handler, and a single api object
	kwr, ok := byPath[v0.PathKubernetesWorkloadResourceDefinitionSets]
	require.True(t, ok)
	assert.Equal(t, "POST", kwr.Method)
	require.NotNil(t, kwr.Handler)
	assert.Nil(t, kwr.IdParamHandler)
	require.NotNil(t, kwr.ApiObjects)
	require.Len(t, *kwr.ApiObjects, 1)
	assert.Equal(t, v0.ObjectTypeKubernetesWorkloadResourceDefinition, (*kwr.ApiObjects)[0].Name)
	assert.Equal(t, "v0", (*kwr.ApiObjects)[0].Version)

	// events join attached object references route: GET with two api
	// objects, no id-param handler
	ev, ok := byPath[v0.PathEventsJoinAttachedObjectReferences]
	require.True(t, ok)
	assert.Equal(t, "GET", ev.Method)
	require.NotNil(t, ev.Handler)
	assert.Nil(t, ev.IdParamHandler)
	require.NotNil(t, ev.ApiObjects)
	require.Len(t, *ev.ApiObjects, 2)
	assert.Equal(t, v0.ObjectTypeEvent, (*ev.ApiObjects)[0].Name)
	assert.Equal(t, v0.ObjectTypeAttachedObjectReference, (*ev.ApiObjects)[1].Name)

	// module api route with module object references: POST route with
	// two api objects and no id-param handler
	mar, ok := byPath[v0.PathModuleApiRouteWithModuleObjectReferences]
	require.True(t, ok)
	assert.Equal(t, "POST", mar.Method)
	require.NotNil(t, mar.Handler)
	assert.Nil(t, mar.IdParamHandler)
	require.NotNil(t, mar.ApiObjects)
	require.Len(t, *mar.ApiObjects, 2)
	assert.Equal(t, v0.ObjectTypeModuleApiRoute, (*mar.ApiObjects)[0].Name)
	assert.Equal(t, v0.ObjectTypeModuleObject, (*mar.ApiObjects)[1].Name)

	// module objects with module api routes: GET route that carries both
	// a collection handler and an id-param handler
	mow, ok := byPath[v0.PathModuleObjectsWithModuleApiRoutes]
	require.True(t, ok)
	assert.Equal(t, "GET", mow.Method)
	require.NotNil(t, mow.Handler)
	require.NotNil(t, mow.IdParamHandler, "id-param handler must be set to register /:id route")
	require.NotNil(t, mow.ApiObjects)
	require.Len(t, *mow.ApiObjects, 2)
	assert.Equal(t, v0.ObjectTypeModuleObject, (*mow.ApiObjects)[0].Name)
	assert.Equal(t, v0.ObjectTypeModuleApiRoute, (*mow.ApiObjects)[1].Name)
}

// TestCustomRoutes_NilHandlerSubstituted covers the nil-handler guard in
// CustomRoutes(): a nil handler must not panic and must still yield the
// full route set with non-nil handler pointers.
func TestCustomRoutes_NilHandlerSubstituted(t *testing.T) {
	// pass nil to exercise the guard
	require.NotPanics(t, func() {
		routes := CustomRoutes(nil)
		require.NotNil(t, routes)
		// all four routes should still exist
		assert.Len(t, *routes, 4)
		// every handler should be a real (non-nil) function pointer
		for _, r := range *routes {
			assert.NotNil(t, r.Handler, "handler should be set for %s", r.Path)
		}
	})
}

// TestAddCustomRoutes_RegistersHandlerAndIdParam covers AddCustomRoutes()'s
// registration loop: every custom route's primary path lands on the echo
// router, and any route with an IdParamHandler also lands its ":id" variant.
func TestAddCustomRoutes_RegistersHandlerAndIdParam(t *testing.T) {
	// wire up a fresh echo instance and a zero-value handler
	e := echo.New()
	h := &handlers.Handler{}

	// invoke the function under test
	AddCustomRoutes(e, h)

	// index registered routes by "METHOD PATH" for lookup by expectation
	registered := map[string]bool{}
	for _, r := range e.Routes() {
		registered[r.Method+" "+r.Path] = true
	}

	// each primary path from CustomRoutes must be registered
	assert.True(t, registered["POST "+v0.PathKubernetesWorkloadResourceDefinitionSets])
	assert.True(t, registered["GET "+v0.PathEventsJoinAttachedObjectReferences])
	assert.True(t, registered["POST "+v0.PathModuleApiRouteWithModuleObjectReferences])
	assert.True(t, registered["GET "+v0.PathModuleObjectsWithModuleApiRoutes])

	// the module-objects route carries an id-param handler, so the
	// ":id" variant must also be registered
	assert.True(t, registered["GET "+v0.PathModuleObjectsWithModuleApiRoutes+"/:id"])

	// routes without IdParamHandler must NOT register a ":id" variant
	assert.False(t, registered["POST "+v0.PathKubernetesWorkloadResourceDefinitionSets+"/:id"])
	assert.False(t, registered["GET "+v0.PathEventsJoinAttachedObjectReferences+"/:id"])
	assert.False(t, registered["POST "+v0.PathModuleApiRouteWithModuleObjectReferences+"/:id"])
}

// TestAddCustomRoutes_NilHandlerDoesNotPanic covers the interaction between
// AddCustomRoutes() and CustomRoutes()'s nil guard: even when the caller
// passes nil, registration must succeed for every primary path.
func TestAddCustomRoutes_NilHandlerDoesNotPanic(t *testing.T) {
	// fresh echo, nil handler
	e := echo.New()

	// registration must not panic
	require.NotPanics(t, func() {
		AddCustomRoutes(e, nil)
	})

	// every primary path should still be registered by method+path
	registered := map[string]bool{}
	for _, r := range e.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	assert.True(t, registered["POST "+v0.PathKubernetesWorkloadResourceDefinitionSets])
	assert.True(t, registered["GET "+v0.PathEventsJoinAttachedObjectReferences])
	assert.True(t, registered["POST "+v0.PathModuleApiRouteWithModuleObjectReferences])
	assert.True(t, registered["GET "+v0.PathModuleObjectsWithModuleApiRoutes])
	assert.True(t, registered["GET "+v0.PathModuleObjectsWithModuleApiRoutes+"/:id"])
}
