package v0

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestDeleteResourceTreatsMissingCRDAsSuccess covers the case where the
// target cluster has no CRD for the resource's kind: DeleteResource should
// return nil (treat as already-deleted) so the workload-instance reconciler
// does not hang on a resource that cannot possibly exist. The case arises
// when a gateway controller materializes a VirtualService (gateway.solo.io)
// and records it in the threeport database, but the CRD was never installed
// in the target cluster.
func TestDeleteResourceTreatsMissingCRDAsSuccess(t *testing.T) {
	// an empty mapper knows about zero kinds, so any RESTMapping lookup
	// returns *meta.NoKindMatchError, the same error a cluster with no CRD
	// for the kind produces.
	emptyMapper := meta.NewDefaultRESTMapper(nil)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.solo.io",
		Version: "v1",
		Kind:    "VirtualService",
	})
	obj.SetName("does-not-matter")
	obj.SetNamespace("default")

	// dynamic client stays nil on purpose: DeleteResource short-circuits on
	// the mapper miss and never dereferences the client. Should it stop
	// short-circuiting, the call reaches the client, panics on nil, and this
	// test fails loudly instead of appearing to pass.
	err := DeleteResource(obj, nil, emptyMapper)
	if err != nil {
		t.Fatalf("DeleteResource with missing-CRD kind: got err %v, want nil", err)
	}
}

// TestDeleteResourceForwardsOtherMapperErrors covers the negative case: a
// mapper error that is NOT a "no match for kind" (e.g. a wrapped error
// with a different underlying cause) should still surface as an error so
// callers aren't silently masked from unrelated failures.
func TestDeleteResourceForwardsOtherMapperErrors(t *testing.T) {
	// wrap a non-NoMatch error in a mapper so getResourceMapping returns it
	// via the normal path
	mapper := &errMapper{err: errOtherMapperFailure}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	})

	err := DeleteResource(obj, nil, mapper)
	if err == nil {
		t.Fatalf("DeleteResource with non-NoMatch mapper error: got nil, want error")
	}
}

// errOtherMapperFailure is a distinct error value we can check for by identity.
var errOtherMapperFailure = &distinctErr{msg: "some other mapper failure"}

type distinctErr struct{ msg string }

func (e *distinctErr) Error() string { return e.msg }

// errMapper is a minimal RESTMapper that always returns a fixed error on
// RESTMapping. All other methods are unimplemented (they return zero
// values / nils) because DeleteResource only calls RESTMapping.
type errMapper struct {
	meta.RESTMapper
	err error
}

func (m *errMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	return nil, m.err
}
