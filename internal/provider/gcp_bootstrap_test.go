package provider

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	installer "github.com/threeport/threeport/pkg/threeport-installer/v0"
)

// TestFormatServiceAccountIDLowercase covers formatServiceAccountID lowercasing
// mixed-case names when formatting the GCP account ID.
func TestFormatServiceAccountIDLowercase(t *testing.T) {
	// format a mixed-case name through the standard threeport format
	got := formatServiceAccountID(serviceAccountNameFormat, "MyRuntime")

	// assert the result is lowercased and prefixed
	want := "threeport-svc-myruntime"
	if got != want {
		t.Fatalf("formatServiceAccountID = %q, want %q", got, want)
	}
}

// TestFormatServiceAccountIDReplacesInvalidChars covers formatServiceAccountID
// replacing characters that are not lowercase alphanumeric or hyphen with a
// hyphen so the result satisfies GCP's account-id rules.
func TestFormatServiceAccountIDReplacesInvalidChars(t *testing.T) {
	// feed a name that contains an underscore and a dot
	got := formatServiceAccountID(serviceAccountNameFormat, "foo_bar.baz")

	// assert underscores and dots became hyphens
	want := "threeport-svc-foo-bar-baz"
	if got != want {
		t.Fatalf("formatServiceAccountID = %q, want %q", got, want)
	}
}

// TestFormatServiceAccountIDTruncatesAt30 covers formatServiceAccountID
// truncating results longer than the 30-character GCP maximum and stripping
// trailing hyphens that the truncation may expose.
func TestFormatServiceAccountIDTruncatesAt30(t *testing.T) {
	// build a name long enough to exceed 30 chars once prefixed
	longName := strings.Repeat("a", 40)
	got := formatServiceAccountID(serviceAccountNameFormat, longName)

	// assert the result respects the 30-char ceiling
	if len(got) > 30 {
		t.Fatalf("expected length <= 30, got %d (%q)", len(got), got)
	}
	// assert no trailing hyphen leaks through truncation
	if strings.HasSuffix(got, "-") {
		t.Fatalf("expected no trailing hyphen, got %q", got)
	}
}

// TestFormatServiceAccountIDPadsShort covers formatServiceAccountID padding a
// too-short result with the "-svc" suffix so it clears the 6-character floor.
func TestFormatServiceAccountIDPadsShort(t *testing.T) {
	// feed an empty name through a bare format so the result is too short
	got := formatServiceAccountID("%s", "ab")

	// assert the pad kicks in and the final length is at least 6
	if len(got) < 6 {
		t.Fatalf("expected length >= 6, got %d (%q)", len(got), got)
	}
	if !strings.HasSuffix(got, "-svc") {
		t.Fatalf("expected -svc suffix on short id, got %q", got)
	}
}

// TestGenerateServiceAccountIDUsesThreeportFormat covers generateServiceAccountID
// applying the threeport-svc- prefix that both create and delete paths depend on.
func TestGenerateServiceAccountIDUsesThreeportFormat(t *testing.T) {
	// generate an id for a runtime name
	got := generateServiceAccountID("prod")

	// assert the threeport-svc- prefix is present
	want := "threeport-svc-prod"
	if got != want {
		t.Fatalf("generateServiceAccountID = %q, want %q", got, want)
	}
}

// TestIsNotFoundErrorNil covers isNotFoundError returning false when the input
// error is nil so callers do not accidentally treat a success as a miss.
func TestIsNotFoundErrorNil(t *testing.T) {
	// pass a nil error
	if isNotFoundError(nil) {
		t.Fatal("expected isNotFoundError(nil) = false")
	}
}

// TestIsNotFoundErrorGRPC covers isNotFoundError recognizing the gRPC
// codes.NotFound status returned by GCP client libraries.
func TestIsNotFoundErrorGRPC(t *testing.T) {
	// build a gRPC NotFound error
	err := status.Error(codes.NotFound, "missing")

	// assert the helper recognizes it
	if !isNotFoundError(err) {
		t.Fatal("expected isNotFoundError(gRPC NotFound) = true")
	}
}

// TestIsNotFoundErrorGRPCOtherCode covers isNotFoundError returning false for
// non-NotFound gRPC status codes so unrelated failures are not swallowed.
func TestIsNotFoundErrorGRPCOtherCode(t *testing.T) {
	// build a gRPC PermissionDenied error
	err := status.Error(codes.PermissionDenied, "nope")

	// assert the helper does not misclassify it
	if isNotFoundError(err) {
		t.Fatal("expected isNotFoundError(PermissionDenied) = false")
	}
}

// TestIsNotFoundErrorHTTP404 covers isNotFoundError matching the "404" token in
// the error string used by the googleapi REST client.
func TestIsNotFoundErrorHTTP404(t *testing.T) {
	// build a plain error containing 404
	err := errors.New("googleapi: Error 404: not found, notFound")

	// assert the helper matches on the substring
	if !isNotFoundError(err) {
		t.Fatal("expected isNotFoundError(404 string) = true")
	}
}

// TestIsNotFoundErrorHTTPOther covers isNotFoundError returning false for other
// HTTP error strings so the helper is not overly greedy.
func TestIsNotFoundErrorHTTPOther(t *testing.T) {
	// build an unrelated error string
	err := errors.New("googleapi: Error 500: internal server error")

	// assert the helper does not match unrelated codes
	if isNotFoundError(err) {
		t.Fatal("expected isNotFoundError(500 string) = false")
	}
}

// TestIsIAMServiceAccountCreatePermissionDenied covers
// isIAMServiceAccountCreatePermissionDenied detecting a 403 whose message names
// the iam.serviceAccounts.create permission.
func TestIsIAMServiceAccountCreatePermissionDenied(t *testing.T) {
	// build a googleapi 403 that names the missing permission
	err := &googleapi.Error{
		Code:    403,
		Message: "Permission 'iam.serviceAccounts.create' denied on resource",
	}

	// assert the helper flags the permission denial
	if !isIAMServiceAccountCreatePermissionDenied(err) {
		t.Fatal("expected true for 403 naming iam.serviceAccounts.create")
	}
}

// TestIsIAMServiceAccountCreatePermissionDeniedNestedError covers
// isIAMServiceAccountCreatePermissionDenied inspecting the nested Errors slice
// where GCP sometimes places the permission string.
func TestIsIAMServiceAccountCreatePermissionDeniedNestedError(t *testing.T) {
	// build a 403 whose top-level message is generic but nested errors carry the token
	err := &googleapi.Error{
		Code:    403,
		Message: "forbidden",
		Errors: []googleapi.ErrorItem{
			{Message: "IAM.SERVICEACCOUNTS.CREATE permission required", Reason: "forbidden"},
		},
	}

	// assert the helper case-folds and finds the token in the nested errors
	if !isIAMServiceAccountCreatePermissionDenied(err) {
		t.Fatal("expected true for nested Errors carrying the permission token")
	}
}

// TestIsIAMServiceAccountCreatePermissionDeniedWrongCode covers
// isIAMServiceAccountCreatePermissionDenied ignoring non-403 googleapi errors.
func TestIsIAMServiceAccountCreatePermissionDeniedWrongCode(t *testing.T) {
	// build a googleapi 404 (not a 403)
	err := &googleapi.Error{
		Code:    404,
		Message: "iam.serviceAccounts.create",
	}

	// assert non-403 codes are rejected even when the token is present
	if isIAMServiceAccountCreatePermissionDenied(err) {
		t.Fatal("expected false for non-403 googleapi error")
	}
}

// TestIsIAMServiceAccountCreatePermissionDeniedNonGoogleapi covers
// isIAMServiceAccountCreatePermissionDenied rejecting errors that are not
// googleapi.Error at all.
func TestIsIAMServiceAccountCreatePermissionDeniedNonGoogleapi(t *testing.T) {
	// build a plain error
	err := errors.New("iam.serviceAccounts.create denied")

	// assert plain errors do not satisfy the 403 predicate
	if isIAMServiceAccountCreatePermissionDenied(err) {
		t.Fatal("expected false for non-googleapi error")
	}
}

// TestWrapIAMServiceAccountCreateErrorNil covers wrapIAMServiceAccountCreateError
// returning nil when the invoke error is nil so callers do not manufacture
// spurious wrap errors.
func TestWrapIAMServiceAccountCreateErrorNil(t *testing.T) {
	// call with a nil invoke error
	if got := wrapIAMServiceAccountCreateError("proj", nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// TestWrapIAMServiceAccountCreateErrorPermissionDenied covers
// wrapIAMServiceAccountCreateError appending the 403 hint for permission
// denials and preserving the wrapped cause.
func TestWrapIAMServiceAccountCreateErrorPermissionDenied(t *testing.T) {
	// build a 403 with the create-permission token
	cause := &googleapi.Error{
		Code:    403,
		Message: "iam.serviceAccounts.create denied",
	}

	// wrap it via the helper
	wrapped := wrapIAMServiceAccountCreateError("proj-123", cause)

	// assert the wrap preserves the cause for errors.Is
	if !errors.Is(wrapped, cause) {
		t.Fatal("expected wrapped error to chain to the cause")
	}
	// assert the hint text is appended for the 403 branch
	if !strings.Contains(wrapped.Error(), "proj-123") {
		t.Fatalf("expected project id in hint, got %q", wrapped.Error())
	}
	if !strings.Contains(wrapped.Error(), "Hint:") {
		t.Fatalf("expected Hint: text on 403, got %q", wrapped.Error())
	}
}

// TestWrapIAMServiceAccountCreateErrorGeneric covers
// wrapIAMServiceAccountCreateError wrapping non-permission errors with the
// standard prefix and without the 403 hint.
func TestWrapIAMServiceAccountCreateErrorGeneric(t *testing.T) {
	// build a plain non-403 error
	cause := errors.New("network timeout")

	// wrap it via the helper
	wrapped := wrapIAMServiceAccountCreateError("proj", cause)

	// assert the wrap preserves the cause
	if !errors.Is(wrapped, cause) {
		t.Fatal("expected wrapped error to chain to the cause")
	}
	// assert the 403 hint is NOT attached for generic failures
	if strings.Contains(wrapped.Error(), "Hint:") {
		t.Fatalf("did not expect Hint: on generic error, got %q", wrapped.Error())
	}
	// assert the standard prefix is present
	if !strings.HasPrefix(wrapped.Error(), "failed to create service account:") {
		t.Fatalf("unexpected wrap prefix: %q", wrapped.Error())
	}
}

// TestFormatIAMServiceAccountCreate403Hint covers
// formatIAMServiceAccountCreate403Hint quoting the project id inside the hint
// string so operators can copy-paste it verbatim.
func TestFormatIAMServiceAccountCreate403Hint(t *testing.T) {
	// call with a sample project id
	hint := formatIAMServiceAccountCreate403Hint("my-proj")

	// assert the project id is quoted per fmt %q
	if !strings.Contains(hint, `"my-proj"`) {
		t.Fatalf("expected quoted project id in hint, got %q", hint)
	}
}

// TestGetServiceAccountID covers getServiceAccountID composing the id from the
// runtime instance name field on KubernetesRuntimeInfraGKE.
func TestGetServiceAccountID(t *testing.T) {
	// build a GKE infra struct with only the fields the helper reads
	i := &KubernetesRuntimeInfraGKE{
		PulumiWorkspace: PulumiWorkspace{RuntimeInstanceName: "dev"},
	}

	// call the accessor
	got := i.getServiceAccountID()

	// assert the id uses the threeport-svc- prefix and instance name
	want := "threeport-svc-dev"
	if got != want {
		t.Fatalf("getServiceAccountID = %q, want %q", got, want)
	}
}

// TestGetServiceAccountEmail covers getServiceAccountEmail composing the full
// email from the account id and the ProjectID field.
func TestGetServiceAccountEmail(t *testing.T) {
	// build a GKE infra struct with runtime name and project id
	i := &KubernetesRuntimeInfraGKE{
		PulumiWorkspace: PulumiWorkspace{RuntimeInstanceName: "dev"},
		ProjectID:       "my-proj",
	}

	// call the accessor
	got := i.getServiceAccountEmail()

	// assert the email is <account>@<project>.iam.gserviceaccount.com
	want := "threeport-svc-dev@my-proj.iam.gserviceaccount.com"
	if got != want {
		t.Fatalf("getServiceAccountEmail = %q, want %q", got, want)
	}
}

// TestGetWorkloadIdentityMembers covers getWorkloadIdentityMembers producing a
// serviceAccount:<pool>[<namespace>/<ksa>] member string for the gcp controller.
func TestGetWorkloadIdentityMembers(t *testing.T) {
	// build a GKE infra struct
	i := &KubernetesRuntimeInfraGKE{}

	// call the accessor with a sample workload-identity pool
	got := i.getWorkloadIdentityMembers("my-proj.svc.id.goog")

	// assert exactly one member is produced (only the gcp controller today)
	if len(got) != 1 {
		t.Fatalf("expected 1 member, got %d: %v", len(got), got)
	}
	// assert the member string encodes the pool, namespace, and controller name
	want := fmt.Sprintf(
		"serviceAccount:my-proj.svc.id.goog[%s/%s]",
		installer.ControlPlaneNamespace,
		installer.ThreeportGcpControllerName,
	)
	if got[0] != want {
		t.Fatalf("member = %q, want %q", got[0], want)
	}
}
