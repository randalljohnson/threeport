package status

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/threeport/threeport/internal/machinetest"
	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestGetGatewayDefinitionStatus_ReturnsInstancesFromAPI asserts the helper
// queries the gateway-instances endpoint by definition id and returns the
// decoded slice in the status detail.
func TestGetGatewayDefinitionStatus_ReturnsInstancesFromAPI(t *testing.T) {
	// stand up an API stub with a handler on the gateway-instances path
	stub := machinetest.NewAPIStub(t)
	inst := v0.GatewayInstance{
		Common:              v0.Common{ID: util.Ptr(uint(42))},
		Instance:            v0.Instance{Name: util.Ptr("gw-inst")},
		GatewayDefinitionID: util.Ptr(uint(7)),
	}
	stub.Mux.HandleFunc(
		v0.PathGatewayInstances,
		func(w http.ResponseWriter, r *http.Request) {
			// confirm the client passed the expected query string
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "gatewaydefinitionid=7", r.URL.RawQuery)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{inst})
		},
	)

	// invoke the helper under test
	detail, err := GetGatewayDefinitionStatus(stub.Client, stub.Addr, 7)

	// happy path returns the decoded instance list on the detail struct
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.NotNil(t, detail.GatewayInstances)
	require.Len(t, *detail.GatewayInstances, 1)
	assert.Equal(t, uint(42), *(*detail.GatewayInstances)[0].ID)
}

// TestGetGatewayDefinitionStatus_WrapsAPIError asserts a non-2xx API response
// is wrapped with the helper's error prefix and the detail struct is still
// returned (non-nil) with a nil instance slice.
func TestGetGatewayDefinitionStatus_WrapsAPIError(t *testing.T) {
	// stub returns a 500 so the client helper reports an error
	stub := machinetest.NewAPIStub(t)
	stub.Mux.HandleFunc(
		v0.PathGatewayInstances,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)

	// invoke the helper against the failing endpoint
	detail, err := GetGatewayDefinitionStatus(stub.Client, stub.Addr, 1)

	// error is wrapped with the helper's prefix and detail is non-nil
	require.Error(t, err)
	assert.True(
		t,
		strings.Contains(err.Error(), "failed to retrieve gateway instances related to gateway definition"),
		"expected wrapped error prefix, got %q", err.Error(),
	)
	require.NotNil(t, detail)
	assert.Nil(t, detail.GatewayInstances)
}

// TestGetGatewayInstanceStatus_ReturnsDefinitionFromAPI asserts the helper
// resolves the definition id from the passed instance and returns the fetched
// definition in the status detail.
func TestGetGatewayInstanceStatus_ReturnsDefinitionFromAPI(t *testing.T) {
	// stub the gateway-definitions/:id endpoint
	stub := machinetest.NewAPIStub(t)
	def := v0.GatewayDefinition{
		Common:     v0.Common{ID: util.Ptr(uint(9))},
		Definition: v0.Definition{Name: util.Ptr("gw-def")},
	}
	stub.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGatewayDefinitions, 9),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{def})
		},
	)

	// invoke with an instance carrying the target definition id
	inst := &v0.GatewayInstance{
		Common:              v0.Common{ID: util.Ptr(uint(1))},
		GatewayDefinitionID: util.Ptr(uint(9)),
	}
	detail, err := GetGatewayInstanceStatus(stub.Client, stub.Addr, inst)

	// happy path returns the fetched definition on the detail struct
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.NotNil(t, detail.GatewayDefinition)
	assert.Equal(t, uint(9), *detail.GatewayDefinition.ID)
}

// TestGetGatewayInstanceStatus_WrapsAPIError asserts a failing definition
// fetch is wrapped with the helper's error prefix.
func TestGetGatewayInstanceStatus_WrapsAPIError(t *testing.T) {
	// stub returns a 500 for the definition fetch
	stub := machinetest.NewAPIStub(t)
	stub.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathGatewayDefinitions, 3),
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)

	// invoke with the matching instance id
	inst := &v0.GatewayInstance{GatewayDefinitionID: util.Ptr(uint(3))}
	detail, err := GetGatewayInstanceStatus(stub.Client, stub.Addr, inst)

	// error is wrapped and detail is a non-nil zero struct
	require.Error(t, err)
	assert.True(
		t,
		strings.Contains(err.Error(), "failed to retrieve gateway definition related to gateway instance"),
		"expected wrapped error prefix, got %q", err.Error(),
	)
	require.NotNil(t, detail)
}

// TestGetDomainNameDefinitionStatus_ReturnsInstancesFromAPI asserts the
// helper queries the domain-name-instances endpoint by definition id and
// returns the decoded slice in the status detail.
func TestGetDomainNameDefinitionStatus_ReturnsInstancesFromAPI(t *testing.T) {
	// stub the domain-name-instances collection endpoint
	stub := machinetest.NewAPIStub(t)
	inst := v0.DomainNameInstance{
		Common:                 v0.Common{ID: util.Ptr(uint(11))},
		Instance:               v0.Instance{Name: util.Ptr("dn-inst")},
		DomainNameDefinitionID: util.Ptr(uint(5)),
	}
	stub.Mux.HandleFunc(
		v0.PathDomainNameInstances,
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "domainnamedefinitionid=5", r.URL.RawQuery)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{inst})
		},
	)

	// invoke the helper under test
	detail, err := GetDomainNameDefinitionStatus(stub.Client, stub.Addr, 5)

	// happy path returns the decoded instance list
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.NotNil(t, detail.DomainNameInstances)
	require.Len(t, *detail.DomainNameInstances, 1)
	assert.Equal(t, uint(11), *(*detail.DomainNameInstances)[0].ID)
}

// TestGetDomainNameDefinitionStatus_WrapsAPIError asserts a failing instance
// fetch is wrapped with the helper's error prefix.
func TestGetDomainNameDefinitionStatus_WrapsAPIError(t *testing.T) {
	// stub returns a 500 for the collection endpoint
	stub := machinetest.NewAPIStub(t)
	stub.Mux.HandleFunc(
		v0.PathDomainNameInstances,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)

	// invoke the helper against the failing endpoint
	detail, err := GetDomainNameDefinitionStatus(stub.Client, stub.Addr, 2)

	// error is wrapped and detail is non-nil
	require.Error(t, err)
	assert.True(
		t,
		strings.Contains(err.Error(), "failed to retrieve domain name instances related to domain name definition"),
		"expected wrapped error prefix, got %q", err.Error(),
	)
	require.NotNil(t, detail)
	assert.Nil(t, detail.DomainNameInstances)
}

// TestGetDomainNameInstanceStatus_ReturnsDefinitionFromAPI asserts the helper
// resolves the definition id from the passed instance and returns the fetched
// definition in the status detail.
func TestGetDomainNameInstanceStatus_ReturnsDefinitionFromAPI(t *testing.T) {
	// stub the domain-name-definitions/:id endpoint
	stub := machinetest.NewAPIStub(t)
	def := v0.DomainNameDefinition{
		Common:     v0.Common{ID: util.Ptr(uint(21))},
		Definition: v0.Definition{Name: util.Ptr("dn-def")},
		Domain:     util.Ptr("example.test"),
	}
	stub.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathDomainNameDefinitions, 21),
		func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodGet, r.Method)
			machinetest.WriteResponse(t, w, http.StatusOK, []apiserver_lib.Object{def})
		},
	)

	// invoke with an instance carrying the target definition id
	inst := &v0.DomainNameInstance{
		Common:                 v0.Common{ID: util.Ptr(uint(1))},
		DomainNameDefinitionID: util.Ptr(uint(21)),
	}
	detail, err := GetDomainNameInstanceStatus(stub.Client, stub.Addr, inst)

	// happy path returns the fetched definition on the detail struct
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.NotNil(t, detail.DomainNameDefinition)
	assert.Equal(t, uint(21), *detail.DomainNameDefinition.ID)
}

// TestGetDomainNameInstanceStatus_WrapsAPIError asserts a failing definition
// fetch is wrapped with the helper's error prefix.
func TestGetDomainNameInstanceStatus_WrapsAPIError(t *testing.T) {
	// stub returns a 500 for the definition fetch
	stub := machinetest.NewAPIStub(t)
	stub.Mux.HandleFunc(
		fmt.Sprintf("%s/%d", v0.PathDomainNameDefinitions, 4),
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)

	// invoke with the matching instance id
	inst := &v0.DomainNameInstance{DomainNameDefinitionID: util.Ptr(uint(4))}
	detail, err := GetDomainNameInstanceStatus(stub.Client, stub.Addr, inst)

	// error is wrapped and detail is non-nil
	require.Error(t, err)
	assert.True(
		t,
		strings.Contains(err.Error(), "failed to retrieve domain name definition related to domain name instance"),
		"expected wrapped error prefix, got %q", err.Error(),
	)
	require.NotNil(t, detail)
}
