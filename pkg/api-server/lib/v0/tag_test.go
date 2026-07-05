package v0

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTranslateAcceptsUnsettableValue asserts that Translate is a no-op when
// the reflected value is not settable and never panics.
func TestTranslateAcceptsUnsettableValue(t *testing.T) {
	// pass a non-addressable value so CanSet() reports false
	var s = "hello"
	v := reflect.ValueOf(s)
	require.False(t, v.CanSet(), "precondition: value must not be settable")

	// verify Translate returns without panic on an unsettable value
	assert.NotPanics(t, func() {
		Translate("validate", v, reflect.StructTag(``))
	})
}

// TestTranslateAcceptsMissingValidateTag asserts that Translate is a no-op when
// the struct tag has no validate key, even on a settable value.
func TestTranslateAcceptsMissingValidateTag(t *testing.T) {
	// build a settable reflect.Value via addressable struct field
	type holder struct{ Name string }
	h := &holder{Name: "x"}
	v := reflect.ValueOf(h).Elem().Field(0)
	require.True(t, v.CanSet(), "precondition: value must be settable")

	// verify Translate exits early on the empty validate tag
	assert.NotPanics(t, func() {
		Translate("validate", v, reflect.StructTag(``))
	})
}

// TestTranslateAcceptsValidateTag asserts that Translate handles a present
// validate tag without panicking; the function is currently a skeleton with
// no observable side effect beyond a successful return.
func TestTranslateAcceptsValidateTag(t *testing.T) {
	type holder struct {
		Name string `validate:"required"`
	}
	h := &holder{Name: "x"}
	v := reflect.ValueOf(h).Elem().Field(0)
	tag := reflect.TypeOf(*h).Field(0).Tag

	// verify the validate branch is exercised without panic
	assert.NotPanics(t, func() {
		Translate("validate", v, tag)
	})
}

// sampleStruct exercises every validate-tag branch ParseStruct handles.
type sampleStruct struct {
	RequiredField string `validate:"required"`
	OptionalField string `validate:"optional"`
	AssocField    string `validate:"optional,association"`
	Untagged      string
}

// TestParseStructClassifiesFieldsByValidateTag asserts that ParseStruct
// partitions struct fields into Required, Optional, and OptionalAssociations
// per their validate tag values.
func TestParseStructClassifiesFieldsByValidateTag(t *testing.T) {
	// arrange a fresh FieldsByTag entry keyed by "validate"
	tf := map[string]*FieldsByTag{
		"validate": {TagName: "validate"},
	}
	s := sampleStruct{}
	v := reflect.ValueOf(&s)
	callCount := 0
	fn := func(_ string, _ reflect.Value, _ reflect.StructTag) { callCount++ }

	// invoke ParseStruct on the sample
	ParseStruct("validate", v, reflect.StructTag(``), fn, tf)

	// assert each tag class collected the matching field name
	assert.Equal(t, []string{"RequiredField"}, tf["validate"].Required)
	assert.Equal(t, []string{"OptionalField"}, tf["validate"].Optional)
	assert.Equal(t, []string{"AssocField"}, tf["validate"].OptionalAssociations)
	// assert the string-leaf callback fired once per string field, including untagged
	assert.Equal(t, 4, callCount)
}

// TestParseStructIndirectsPointers asserts that ParseStruct dereferences a
// pointer input and classifies the pointee's fields.
func TestParseStructIndirectsPointers(t *testing.T) {
	tf := map[string]*FieldsByTag{
		"validate": {TagName: "validate"},
	}
	s := &sampleStruct{}
	// pass pointer-to-pointer to force reflect.Indirect to unwrap
	v := reflect.ValueOf(&s).Elem()
	fn := func(_ string, _ reflect.Value, _ reflect.StructTag) {}

	ParseStruct("validate", v, reflect.StructTag(``), fn, tf)

	// verify field partitioning matches the direct-struct case
	assert.Equal(t, []string{"RequiredField"}, tf["validate"].Required)
	assert.Equal(t, []string{"OptionalField"}, tf["validate"].Optional)
	assert.Equal(t, []string{"AssocField"}, tf["validate"].OptionalAssociations)
}

// TestParseStructRecursesIntoNestedStruct asserts that ParseStruct descends
// into embedded and nested struct fields and classifies their fields too.
func TestParseStructRecursesIntoNestedStruct(t *testing.T) {
	type inner struct {
		InnerReq string `validate:"required"`
	}
	type outer struct {
		Outer string `validate:"required"`
		Nest  inner
	}

	tf := map[string]*FieldsByTag{
		"validate": {TagName: "validate"},
	}
	v := reflect.ValueOf(&outer{})
	fn := func(_ string, _ reflect.Value, _ reflect.StructTag) {}

	ParseStruct("validate", v, reflect.StructTag(``), fn, tf)

	// verify both the top-level and nested required fields were collected
	assert.ElementsMatch(t, []string{"Outer", "InnerReq"}, tf["validate"].Required)
}

// TestParseStructIteratesStringSlice asserts that ParseStruct fans out over
// slices of strings, invoking the callback once per element.
func TestParseStructIteratesStringSlice(t *testing.T) {
	tf := map[string]*FieldsByTag{
		"validate": {TagName: "validate"},
	}
	strs := []string{"a", "b", "c"}
	v := reflect.ValueOf(strs)
	callCount := 0
	fn := func(_ string, val reflect.Value, _ reflect.StructTag) {
		callCount++
		// each visit lands on a string leaf
		assert.Equal(t, reflect.String, val.Kind())
	}

	ParseStruct("validate", v, reflect.StructTag(``), fn, tf)

	// verify the callback fired once per slice element
	assert.Equal(t, 3, callCount)
}

// TestParseStructSkipsNonStringSlice asserts that ParseStruct ignores slices
// whose element type is not string; no callback fires and no field is added.
func TestParseStructSkipsNonStringSlice(t *testing.T) {
	tf := map[string]*FieldsByTag{
		"validate": {TagName: "validate"},
	}
	ints := []int{1, 2, 3}
	v := reflect.ValueOf(ints)
	callCount := 0
	fn := func(_ string, _ reflect.Value, _ reflect.StructTag) { callCount++ }

	ParseStruct("validate", v, reflect.StructTag(``), fn, tf)

	// verify the non-string-slice branch produces no visits and no classified fields
	assert.Equal(t, 0, callCount)
	assert.Nil(t, tf["validate"].Required)
	assert.Nil(t, tf["validate"].Optional)
	assert.Nil(t, tf["validate"].OptionalAssociations)
}

// TestParseStructCallsFnOnStringLeaf asserts that a bare string value routes
// straight to the callback with the propagated tag.
func TestParseStructCallsFnOnStringLeaf(t *testing.T) {
	tf := map[string]*FieldsByTag{
		"validate": {TagName: "validate"},
	}
	s := "leaf"
	v := reflect.ValueOf(s)
	incomingTag := reflect.StructTag(`validate:"required"`)
	var seenTag reflect.StructTag
	callCount := 0
	fn := func(_ string, val reflect.Value, tag reflect.StructTag) {
		callCount++
		// capture the value and tag for post-hoc assertions
		assert.Equal(t, reflect.String, val.Kind())
		seenTag = tag
	}

	ParseStruct("validate", v, incomingTag, fn, tf)

	// verify the callback fired exactly once with the caller-supplied tag
	assert.Equal(t, 1, callCount)
	assert.Equal(t, incomingTag, seenTag)
}

// TestParseStructIgnoresUnsupportedKinds asserts that ParseStruct is a no-op
// on kinds it does not handle (int, map), classifying nothing.
func TestParseStructIgnoresUnsupportedKinds(t *testing.T) {
	tf := map[string]*FieldsByTag{
		"validate": {TagName: "validate"},
	}
	callCount := 0
	fn := func(_ string, _ reflect.Value, _ reflect.StructTag) { callCount++ }

	// exercise int and map inputs; both should fall through the switch
	ParseStruct("validate", reflect.ValueOf(42), reflect.StructTag(``), fn, tf)
	ParseStruct("validate", reflect.ValueOf(map[string]string{"k": "v"}), reflect.StructTag(``), fn, tf)

	// verify neither input caused a callback or a field classification
	assert.Equal(t, 0, callCount)
	assert.Nil(t, tf["validate"].Required)
}

// TestParseStructEmptyStruct asserts that a struct with no fields produces
// no classification work but does not panic.
func TestParseStructEmptyStruct(t *testing.T) {
	type empty struct{}
	tf := map[string]*FieldsByTag{
		"validate": {TagName: "validate"},
	}
	fn := func(_ string, _ reflect.Value, _ reflect.StructTag) {}

	// verify the empty-struct case exits cleanly
	assert.NotPanics(t, func() {
		ParseStruct("validate", reflect.ValueOf(empty{}), reflect.StructTag(``), fn, tf)
	})
	assert.Nil(t, tf["validate"].Required)
}

// TestParseStructRespectsTagName asserts that ParseStruct switches on the
// caller-supplied tag key so a non-validate key routes into the same slot.
func TestParseStructRespectsTagName(t *testing.T) {
	type customTagged struct {
		A string `custom:"required"`
		B string `custom:"optional"`
	}
	tf := map[string]*FieldsByTag{
		"custom": {TagName: "custom"},
	}
	fn := func(_ string, _ reflect.Value, _ reflect.StructTag) {}

	// invoke ParseStruct with a non-default tag key
	ParseStruct("custom", reflect.ValueOf(&customTagged{}), reflect.StructTag(``), fn, tf)

	// verify the custom-tag classification tracks the same required/optional split
	assert.Equal(t, []string{"A"}, tf["custom"].Required)
	assert.Equal(t, []string{"B"}, tf["custom"].Optional)
}
