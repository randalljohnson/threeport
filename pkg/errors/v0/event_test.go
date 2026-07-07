package v0

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestErrWithEvent_ErrorReturnsMessage covers the Error() contract:
// the returned string carries the constructor's Message verbatim so
// caller-side log lines stay stable.
func TestErrWithEvent_ErrorReturnsMessage(t *testing.T) {
	// build a sentinel with a distinctive message so the assertion
	// catches accidental prefixing or reformatting.
	err := &ErrWithEvent{
		Message: "ssh dial failed",
		Event: v0.Event{
			Reason: util.Ptr("SSHConnectFailed"),
			Type:   util.Ptr("Warning"),
			Note:   util.Ptr("dial tcp: refused"),
		},
	}

	// Error() must round-trip the Message field with no additions.
	require.Equal(t, "ssh dial failed", err.Error())
}

// TestErrWithEvent_UnwrapsThroughErrorsAs covers the substitution
// contract HandleEventOverride relies on: errors.As reaches the
// wrapped ErrWithEvent through a fmt.Errorf %w chain so a caller
// that added context still lets the recorder swap in the specific
// event.
func TestErrWithEvent_UnwrapsThroughErrorsAs(t *testing.T) {
	// wrap the sentinel with fmt.Errorf %w twice so the unwrap has
	// real work to do.
	inner := &ErrWithEvent{
		Message: "boom",
		Event: v0.Event{
			Reason: util.Ptr("CreateResourceError"),
			Type:   util.Ptr("Warning"),
		},
	}
	outer := fmt.Errorf("wrap two: %w", fmt.Errorf("wrap one: %w", inner))

	// errors.As should find the sentinel through the two wraps and
	// bind it into the target variable.
	var got *ErrWithEvent
	require.True(t, errors.As(outer, &got), "errors.As must unwrap ErrWithEvent through fmt.Errorf %%w layers")
	require.NotNil(t, got)
	require.NotNil(t, got.Event.Reason)
	assert.Equal(t, "CreateResourceError", *got.Event.Reason, "unwrap surfaces the sentinel's carried event")
}
