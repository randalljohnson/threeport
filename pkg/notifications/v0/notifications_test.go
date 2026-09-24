package v0

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestConsumeMessage_DecodesCreatedNotification asserts that a well-formed json
// payload with a Created operation round-trips into a Notification with the
// same operation, creation time, and object payload.
func TestConsumeMessage_DecodesCreatedNotification(t *testing.T) {
	// build a payload representing a Created notification with an object body
	creationTime := int64(1720000000)
	payload := map[string]interface{}{
		"Operation":     NotificationOperationCreated,
		"CreationTime":  creationTime,
		"Object":        map[string]interface{}{"ID": 42, "Name": "example"},
		"ObjectVersion": "v0",
	}
	msgData, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal test payload: %v", err)
	}

	// decode the payload through the code under test
	notif, err := ConsumeMessage(msgData)
	if err != nil {
		t.Fatalf("ConsumeMessage returned unexpected error: %v", err)
	}

	// verify the operation field carried through
	if notif.Operation != NotificationOperationCreated {
		t.Errorf("expected Operation=%q, got %q", NotificationOperationCreated, notif.Operation)
	}
	// verify the creation time pointer is populated with the input value
	if notif.CreationTime == nil {
		t.Fatalf("expected CreationTime to be non-nil")
	}
	if *notif.CreationTime != creationTime {
		t.Errorf("expected CreationTime=%d, got %d", creationTime, *notif.CreationTime)
	}
	// verify the object version carried through
	if notif.ObjectVersion != "v0" {
		t.Errorf("expected ObjectVersion=%q, got %q", "v0", notif.ObjectVersion)
	}
	// verify the object body decoded into a map with the expected fields
	obj, ok := notif.Object.(map[string]interface{})
	if !ok {
		t.Fatalf("expected Object to decode as map[string]interface{}, got %T", notif.Object)
	}
	if obj["Name"] != "example" {
		t.Errorf("expected Object[Name]=example, got %v", obj["Name"])
	}
}

// TestConsumeMessage_UsesJSONNumberForNumericFields asserts that decoder.UseNumber
// is in effect: numeric fields inside the arbitrary Object payload arrive as
// json.Number rather than being coerced to float64, so callers can safely
// re-decode them as int64 without precision loss.
func TestConsumeMessage_UsesJSONNumberForNumericFields(t *testing.T) {
	// use a numeric id large enough to lose precision if decoded as float64
	msgData := []byte(`{"Operation":"Updated","Object":{"ID":9007199254740993}}`)

	// decode and inspect the numeric field's type
	notif, err := ConsumeMessage(msgData)
	if err != nil {
		t.Fatalf("ConsumeMessage returned unexpected error: %v", err)
	}
	obj, ok := notif.Object.(map[string]interface{})
	if !ok {
		t.Fatalf("expected Object to decode as map[string]interface{}, got %T", notif.Object)
	}
	// assert the numeric field is a json.Number rather than a float64
	num, ok := obj["ID"].(json.Number)
	if !ok {
		t.Fatalf("expected Object[ID] to be json.Number, got %T", obj["ID"])
	}
	// assert the raw string survives the round-trip without precision loss
	if num.String() != "9007199254740993" {
		t.Errorf("expected numeric round-trip to preserve %q, got %q", "9007199254740993", num.String())
	}
}

// TestConsumeMessage_RejectsInvalidJSON asserts that malformed json returns a
// wrapped decode error rather than a nil notification with no error.
func TestConsumeMessage_RejectsInvalidJSON(t *testing.T) {
	// feed the decoder a payload that is not valid json
	notif, err := ConsumeMessage([]byte("{not json"))

	// verify an error is returned and the notification pointer is nil
	if err == nil {
		t.Fatalf("expected error decoding malformed json, got nil")
	}
	if notif != nil {
		t.Errorf("expected nil notification on decode failure, got %+v", notif)
	}
	// verify the error text names the decode context so callers can trace it
	if !strings.Contains(err.Error(), "failed to decode notification json") {
		t.Errorf("expected error to describe decode context, got %q", err.Error())
	}
}

// TestConsumeMessage_LeavesCreationTimeNilWhenAbsent asserts that a payload
// without a CreationTime field decodes into a Notification whose CreationTime
// pointer is nil, so callers can distinguish an absent value from a zero one.
func TestConsumeMessage_LeavesCreationTimeNilWhenAbsent(t *testing.T) {
	// omit CreationTime from the payload entirely
	msgData := []byte(`{"Operation":"Deleted","ObjectVersion":"v0"}`)

	// decode and assert the pointer is nil rather than pointing at zero
	notif, err := ConsumeMessage(msgData)
	if err != nil {
		t.Fatalf("ConsumeMessage returned unexpected error: %v", err)
	}
	if notif.CreationTime != nil {
		t.Errorf("expected CreationTime to be nil when field is absent, got %d", *notif.CreationTime)
	}
	if notif.Operation != NotificationOperationDeleted {
		t.Errorf("expected Operation=%q, got %q", NotificationOperationDeleted, notif.Operation)
	}
}
