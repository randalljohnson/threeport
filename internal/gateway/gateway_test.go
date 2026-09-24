package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// newTestReconciler builds a Reconciler pointing at the given httptest server.
// The scheme is stripped because client_lib.GetResponse re-prepends "http://"
// when there is no custom transport configured.
func newTestReconciler(server *httptest.Server) *controller.Reconciler {
	addr := strings.TrimPrefix(server.URL, "http://")
	return &controller.Reconciler{
		Name:      "test",
		APIServer: addr,
		APIClient: server.Client(),
	}
}

// newTestLogger returns a discard-backed logger that satisfies the *logr.Logger
// signature used by the reconciliation entry points.
func newTestLogger() *logr.Logger {
	l := logr.Discard()
	return &l
}

// writeAPIResponse marshals obj as the Data[0] element of an apiserver
// Response envelope and writes it back with the given status.
func writeAPIResponse(t *testing.T, w http.ResponseWriter, status int, obj interface{}) {
	t.Helper()
	resp := apiserver_lib.Response{Data: []apiserver_lib.Object{obj}}
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// TestGetSubDomain asserts subdomain assembly joins the gateway subdomain and
// the definition domain with a single dot separator.
func TestGetSubDomain(t *testing.T) {
	// build a gateway definition + domain name definition that together should
	// produce "api.example.com".
	sub := "api"
	domain := "example.com"
	gd := &v0.GatewayDefinition{SubDomain: &sub}
	dnd := &v0.DomainNameDefinition{Domain: &domain}

	// exercise the joiner and confirm dot-separated composition.
	assert.Equal(t, "api.example.com", getSubDomain(gd, dnd))
}

// TestEnsureGlooEdgePortExists_MatchIsNoop confirms a matching port entry is
// left untouched: the returned slice is the same as the input.
func TestEnsureGlooEdgePortExists_MatchIsNoop(t *testing.T) {
	// pre-populate the ports slice with an entry that matches the request.
	existing := []interface{}{
		map[string]interface{}{
			"name":     "80",
			"protocol": "http",
			"port":     int64(80),
			"ssl":      false,
		},
	}
	log := newTestLogger()

	// exercise the ensure helper with the same protocol/port/tls values.
	ports, err := ensureGlooEdgePortExists("http", 80, false, existing, log)
	require.NoError(t, err)

	// the ports slice should not have grown when the request already matches.
	assert.Len(t, ports, 1)
}

// TestEnsureGlooEdgePortExists_AppendsNewPort confirms a non-matching request
// grows the ports slice and the appended entry carries the requested fields.
func TestEnsureGlooEdgePortExists_AppendsNewPort(t *testing.T) {
	// start from an empty ports slice so a new entry has to be appended.
	existing := []interface{}{}
	log := newTestLogger()

	// request a fresh port so the append branch is exercised.
	ports, err := ensureGlooEdgePortExists("tcp", 5432, true, existing, log)
	require.NoError(t, err)

	// the ports slice should have gained exactly one entry with the requested
	// protocol, port, and tls flag.
	require.Len(t, ports, 1)
	spec := ports[0].(map[string]interface{})
	assert.Equal(t, "tcp", spec["protocol"])
	assert.Equal(t, int64(5432), spec["port"])
	assert.Equal(t, true, spec["ssl"])
	assert.Equal(t, "5432", spec["name"])
}

// TestEnsureGlooEdgePortExists_MismatchAppends confirms that a partial-match
// (same port but different tls flag) results in a new entry being appended,
// not a mutation of the existing one.
func TestEnsureGlooEdgePortExists_MismatchAppends(t *testing.T) {
	// existing entry is port 443 tls=false; request is port 443 tls=true.
	existing := []interface{}{
		map[string]interface{}{
			"name":     "443",
			"protocol": "http",
			"port":     int64(443),
			"ssl":      false,
		},
	}
	log := newTestLogger()

	// exercise the ensure helper with a mismatched tls flag.
	ports, err := ensureGlooEdgePortExists("http", 443, true, existing, log)
	require.NoError(t, err)

	// the slice should carry both entries: original plus the newly appended one.
	assert.Len(t, ports, 2)
}

// TestV0GatewayDefinitionDeleted_NoDeletionScheduled asserts the reconciler
// short-circuits when deletion has not been scheduled.
func TestV0GatewayDefinitionDeleted_NoDeletionScheduled(t *testing.T) {
	// build a definition with a nil DeletionScheduled field.
	gd := &v0.GatewayDefinition{}

	// exercise the deleter and confirm the short-circuit branch.
	requeue, err := v0GatewayDefinitionDeleted(&controller.Reconciler{}, gd, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
}

// TestV0GatewayDefinitionDeleted_AlreadyConfirmed asserts the reconciler
// short-circuits when deletion has already been confirmed.
func TestV0GatewayDefinitionDeleted_AlreadyConfirmed(t *testing.T) {
	// build a definition with both DeletionScheduled and DeletionConfirmed set.
	now := time.Now().UTC()
	gd := &v0.GatewayDefinition{
		Reconciliation: v0.Reconciliation{
			DeletionScheduled: &now,
			DeletionConfirmed: &now,
		},
	}

	// exercise the deleter and confirm the already-confirmed short-circuit.
	requeue, err := v0GatewayDefinitionDeleted(&controller.Reconciler{}, gd, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
}

// TestV0GatewayDefinitionDeleted_NilWorkloadDefinitionID asserts the deleter
// short-circuits when the linked workload definition ID is nil (nothing to
// clean up in the API).
func TestV0GatewayDefinitionDeleted_NilWorkloadDefinitionID(t *testing.T) {
	// build a definition marked for deletion with no workload-definition link.
	now := time.Now().UTC()
	gd := &v0.GatewayDefinition{
		Reconciliation: v0.Reconciliation{DeletionScheduled: &now},
	}

	// exercise the deleter and confirm the nil-id short-circuit.
	requeue, err := v0GatewayDefinitionDeleted(&controller.Reconciler{}, gd, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
}

// TestV0GatewayInstanceDeleted_NoDeletionScheduled asserts the reconciler
// returns an error when a delete notification arrives without a scheduled
// deletion timestamp.
func TestV0GatewayInstanceDeleted_NoDeletionScheduled(t *testing.T) {
	// build an instance with a nil DeletionScheduled field.
	gi := &v0.GatewayInstance{}

	// exercise the deleter and confirm the not-scheduled error.
	_, err := v0GatewayInstanceDeleted(&controller.Reconciler{}, gi, newTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not scheduled")
}

// TestV0GatewayInstanceDeleted_AlreadyConfirmed asserts the reconciler
// short-circuits when deletion has already been confirmed.
func TestV0GatewayInstanceDeleted_AlreadyConfirmed(t *testing.T) {
	// build an instance with both DeletionScheduled and DeletionConfirmed set.
	now := time.Now().UTC()
	gi := &v0.GatewayInstance{
		Reconciliation: v0.Reconciliation{
			DeletionScheduled: &now,
			DeletionConfirmed: &now,
		},
	}

	// exercise the deleter and confirm the already-confirmed short-circuit.
	requeue, err := v0GatewayInstanceDeleted(&controller.Reconciler{}, gi, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
}

// TestV0DomainNameInstanceDeleted asserts the domain-name-instance deleter is
// a no-op and returns cleanly.
func TestV0DomainNameInstanceDeleted(t *testing.T) {
	// exercise the deleter and confirm the no-op result.
	requeue, err := v0DomainNameInstanceDeleted(&controller.Reconciler{}, &v0.DomainNameInstance{}, newTestLogger())
	require.NoError(t, err)
	assert.Equal(t, int64(0), requeue)
}

// TestGetThreeportObjects_NilRuntimeInstanceID asserts the helper fails when
// the runtime-instance link on the gateway instance is nil.
func TestGetThreeportObjects_NilRuntimeInstanceID(t *testing.T) {
	// build an instance whose KubernetesRuntimeInstanceID is nil.
	gi := &v0.GatewayInstance{}

	// exercise the helper and confirm the nil-runtime error.
	_, _, _, err := getThreeportObjects(&controller.Reconciler{}, gi)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubernetes runtime instance ID is nil")
}

// TestGetDomainInfo_NilDomainNameDefinitionID asserts the helper returns the
// empty domain and admin email when the gateway definition has no domain link.
func TestGetDomainInfo_NilDomainNameDefinitionID(t *testing.T) {
	// build a definition whose DomainNameDefinitionID is nil.
	gd := &v0.GatewayDefinition{}

	// exercise the helper and confirm the empty-string fallback.
	domain, email, err := getDomainInfo(&controller.Reconciler{}, gd)
	require.NoError(t, err)
	assert.Equal(t, "", domain)
	assert.Equal(t, "", email)
}

// TestValidateThreeportStateExternalDns_NilWorkloadInstanceID asserts the
// dns validator fails when the domain-name instance has no workload-instance
// link.
func TestValidateThreeportStateExternalDns_NilWorkloadInstanceID(t *testing.T) {
	// build a domain-name instance whose KubernetesWorkloadInstanceID is nil.
	dni := &v0.DomainNameInstance{}

	// exercise the validator and confirm the nil-workload error.
	err := validateThreeportStateExternalDns(&controller.Reconciler{}, dni, newTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubernetes workload instance ID is nil")
}

// TestConfirmWorkloadInstanceReconciled_True drives the helper against a stub
// API returning Reconciled=true and confirms the boolean is true.
func TestConfirmWorkloadInstanceReconciled_True(t *testing.T) {
	// stand up a stub API that returns a workload instance with Reconciled=true.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wi := v0.KubernetesWorkloadInstance{
			Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(true)},
		}
		writeAPIResponse(t, w, http.StatusOK, wi)
	}))
	defer server.Close()

	// exercise the helper against the stub server.
	rec := newTestReconciler(server)
	got, err := confirmWorkloadInstanceReconciled(rec, 1)
	require.NoError(t, err)
	assert.True(t, got)
}

// TestConfirmWorkloadInstanceReconciled_False drives the helper against a stub
// API returning Reconciled=false and confirms the boolean is false.
func TestConfirmWorkloadInstanceReconciled_False(t *testing.T) {
	// stub API returns a workload instance with Reconciled=false.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wi := v0.KubernetesWorkloadInstance{
			Reconciliation: v0.Reconciliation{Reconciled: util.Ptr(false)},
		}
		writeAPIResponse(t, w, http.StatusOK, wi)
	}))
	defer server.Close()

	// exercise the helper and confirm it surfaces the unreconciled state.
	rec := newTestReconciler(server)
	got, err := confirmWorkloadInstanceReconciled(rec, 1)
	require.NoError(t, err)
	assert.False(t, got)
}

// TestConfirmWorkloadInstanceReconciled_APIError confirms an API failure is
// wrapped and surfaced to the caller.
func TestConfirmWorkloadInstanceReconciled_APIError(t *testing.T) {
	// stub API returns 500 for every request.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	// exercise the helper and confirm a wrapped error surfaces.
	rec := newTestReconciler(server)
	_, err := confirmWorkloadInstanceReconciled(rec, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get kubernetes workload instance")
}

// TestGetVirtualServicesYaml_EmptyPorts confirms the helper returns an empty
// manifest list (with no error) when the gateway definition has no HTTP ports.
func TestGetVirtualServicesYaml_EmptyPorts(t *testing.T) {
	// stub API returns an empty list of HTTP ports.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiserver_lib.Response{Data: []apiserver_lib.Object{}}
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	// build a gateway definition with an ID so the query URL is well-formed.
	id := uint(1)
	name := "gw"
	gd := &v0.GatewayDefinition{
		Common:     v0.Common{ID: &id},
		Definition: v0.Definition{Name: &name},
	}

	// exercise the helper and confirm the empty-manifest result.
	rec := newTestReconciler(server)
	manifests, err := getVirtualServicesYaml(rec, gd, "example.com")
	require.NoError(t, err)
	assert.Empty(t, manifests)
}

// TestGetTcpGatewaysYaml_EmptyPorts confirms the helper returns an empty
// manifest list (with no error) when the gateway definition has no TCP ports.
func TestGetTcpGatewaysYaml_EmptyPorts(t *testing.T) {
	// stub API returns an empty list of TCP ports.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := apiserver_lib.Response{Data: []apiserver_lib.Object{}}
		body, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	// build a gateway definition with an ID so the query URL is well-formed.
	id := uint(1)
	name := "gw"
	gd := &v0.GatewayDefinition{
		Common:     v0.Common{ID: &id},
		Definition: v0.Definition{Name: &name},
	}

	// exercise the helper and confirm the empty-manifest result.
	rec := newTestReconciler(server)
	manifests, err := getTcpGatewaysYaml(rec, gd)
	require.NoError(t, err)
	assert.Empty(t, manifests)
}
