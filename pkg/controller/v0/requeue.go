package controller

import "time"

const (
	DefaultInitialRequeueDelay = 1
	DefaultMaxRequeueDelay     = 30
)

// Done is the requeue delay signaling this reconcile pass is complete;
// the wrapper marks the subject reconciled without requeuing.
const Done int64 = 0

// Requeue30s is the standard requeue delay for waiting on a child's
// Reconciled=true state before marking the parent reconciled.
const Requeue30s int64 = 30

// SetRequeueDelay returns the delay before an object is reconciled again. The
// delay grows with how long ago the notification was published, starting at
// the initial delay and reaching twice the elapsed time, and it never exceeds
// the maximum delay.
func SetRequeueDelay(creationTime *int64) int64 {
	var requeueDelay int64

	currentTime := time.Now().Unix()
	elapsedTime := currentTime - *creationTime

	if elapsedTime < DefaultInitialRequeueDelay {
		requeueDelay = DefaultInitialRequeueDelay
	} else {
		requeueDelay = elapsedTime * 2
	}

	// cap the result rather than the elapsed time it was derived from: an
	// elapsed time just under the maximum doubles to well above it
	if requeueDelay > DefaultMaxRequeueDelay {
		requeueDelay = DefaultMaxRequeueDelay
	}

	return requeueDelay
}
