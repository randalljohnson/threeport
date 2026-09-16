package controller

import "time"

const (
	DefaultInitialRequeueDelay = 1
	DefaultMaxRequeueDelay     = 30
)

// Done is a 0s delay meaning this pass is complete and should not requeue.
const Done int64 = 0

// Requeue30s is a 30s wait before the next reconcile of the same object.
const Requeue30s int64 = 30

// SetRequeueDelay returns the delay before the object is reconciled again.
// The delay is the initial delay while the notification is younger than that,
// then twice the age in seconds, never more than the maximum delay.
func SetRequeueDelay(creationTime *int64) int64 {
	var requeueDelay int64

	currentTime := time.Now().Unix()
	elapsedTime := currentTime - *creationTime

	if elapsedTime < DefaultInitialRequeueDelay {
		requeueDelay = DefaultInitialRequeueDelay
	} else {
		requeueDelay = elapsedTime * 2
	}

	// cap the delay after doubling, not the elapsed time: an elapsed time
	// one second below the maximum doubles past it
	if requeueDelay > DefaultMaxRequeueDelay {
		requeueDelay = DefaultMaxRequeueDelay
	}

	return requeueDelay
}
