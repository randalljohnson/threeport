package v0

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newValidatingContext returns an echo context wired with a CustomValidator
// that registers the tags used across the validator helpers so the round-trip
// through echo.Context.Validate exercises the real code path.
func newValidatingContext(t *testing.T) echo.Context {
	t.Helper()
	e := echo.New()
	v := validator.New()
	require.NoError(t, v.RegisterValidation("optional", IsOptional))
	require.NoError(t, v.RegisterValidation("association", IsAssociation))
	require.NoError(t, v.RegisterValidation("ISO8601date", IsISO8601Date))
	e.Validator = &CustomValidator{Validator: v}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec)
}

// TestCustomValidatorValidate covers that CustomValidator.Validate delegates
// to the embedded validator.Validate and reports required-tag violations.
func TestCustomValidatorValidate(t *testing.T) {
	// set up a struct with a required field and a validator instance
	type payload struct {
		Name string `validate:"required"`
	}
	cv := &CustomValidator{Validator: validator.New()}

	// happy path: populated required field returns no error
	assert.NoError(t, cv.Validate(payload{Name: "ok"}))

	// error path: empty required field returns a ValidationErrors error
	err := cv.Validate(payload{})
	require.Error(t, err)
	_, ok := err.(validator.ValidationErrors)
	assert.True(t, ok, "expected validator.ValidationErrors, got %T", err)
}

// TestIsISO8601Date covers accepted and rejected date-time forms of the
// ISO8601 regex used to gate date fields on incoming payloads.
func TestIsISO8601Date(t *testing.T) {
	v := validator.New()
	require.NoError(t, v.RegisterValidation("ISO8601date", IsISO8601Date))

	// each case names the input shape and whether it should validate
	cases := []struct {
		name  string
		input string
		valid bool
	}{
		{"space separator no zone", "2024-01-15 12:34:56", true},
		{"T separator with Z", "2024-12-31T23:59:59Z", true},
		{"leading dash year", "-2024-01-15T00:00:00Z", true},
		{"invalid month", "2024-13-01T00:00:00Z", false},
		{"invalid day", "2024-02-32T00:00:00Z", false},
		{"missing seconds", "2024-01-15T12:34", false},
		{"not a date at all", "hello", false},
		{"empty string", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// use Var so the registered function is exercised as a real tag
			err := v.Var(tc.input, "ISO8601date")
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// TestIsOptional covers that the optional tag always passes; it is a no-op
// marker used by codegen to keep required-collection logic uniform.
func TestIsOptional(t *testing.T) {
	v := validator.New()
	require.NoError(t, v.RegisterValidation("optional", IsOptional))

	// empty value passes: optional never rejects
	assert.NoError(t, v.Var("", "optional"))
	// populated value also passes
	assert.NoError(t, v.Var("anything", "optional"))
}

// TestIsAssociation covers that the association tag always passes; it is a
// marker used by codegen for related-object fields.
func TestIsAssociation(t *testing.T) {
	v := validator.New()
	require.NoError(t, v.RegisterValidation("association", IsAssociation))

	// association never rejects, regardless of value
	assert.NoError(t, v.Var("", "association"))
	assert.NoError(t, v.Var("anything", "association"))
}

// TestIsSliceOrArray covers positive and negative reflect-kind detection.
func TestIsSliceOrArray(t *testing.T) {
	// slice: kind reflect.Slice returns true
	assert.True(t, IsSliceOrArray([]int{1, 2, 3}))
	// array: kind reflect.Array returns true
	assert.True(t, IsSliceOrArray([3]int{1, 2, 3}))
	// empty slice still reports true
	assert.True(t, IsSliceOrArray([]string{}))

	// non-collection kinds return false
	assert.False(t, IsSliceOrArray("string"))
	assert.False(t, IsSliceOrArray(42))
	assert.False(t, IsSliceOrArray(struct{ X int }{X: 1}))
	assert.False(t, IsSliceOrArray(map[string]int{}))
}

// TestValidateObj covers that missing required fields are collected on the
// out-slice and that a valid object leaves the slice untouched.
func TestValidateObj(t *testing.T) {
	type payload struct {
		Name string `validate:"required"`
		Age  int    `validate:"required"`
	}
	c := newValidatingContext(t)

	// error path: both required fields empty are recorded by name
	var missing []string
	code, err := ValidateObj(c, payload{}, &missing)
	// ValidateObj always returns (500, nil) even when it accumulates fields
	assert.Equal(t, 500, code)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"Name", "Age"}, missing)

	// happy path: fully populated struct records nothing
	var missing2 []string
	code2, err2 := ValidateObj(c, payload{Name: "x", Age: 1}, &missing2)
	assert.Equal(t, 500, code2)
	assert.NoError(t, err2)
	assert.Empty(t, missing2)

	// non-required violations do not populate the missing slice
	type ranged struct {
		Age int `validate:"gt=10"`
	}
	var missing3 []string
	code3, err3 := ValidateObj(c, ranged{Age: 1}, &missing3)
	assert.Equal(t, 500, code3)
	assert.NoError(t, err3)
	assert.Empty(t, missing3)
}

// TestValidateBoundData covers the single-object path, the slice path, and
// the happy path when nothing is missing.
func TestValidateBoundData(t *testing.T) {
	type payload struct {
		Name string `validate:"required"`
	}
	c := newValidatingContext(t)

	// single-object missing required field returns 400 with joined names
	code, err := ValidateBoundData(c, payload{}, "payload")
	assert.Equal(t, 400, code)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ErrMsgMissingRequiredFields)
	assert.Contains(t, err.Error(), "Name")

	// single-object happy path returns the default 500, nil
	code, err = ValidateBoundData(c, payload{Name: "ok"}, "payload")
	assert.Equal(t, 500, code)
	assert.NoError(t, err)

	// slice with one bad element accumulates and returns 400
	code, err = ValidateBoundData(c, []payload{{Name: "ok"}, {}}, "payload")
	assert.Equal(t, 400, code)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Name")

	// slice with multiple bad elements joins field names with commas
	code, err = ValidateBoundData(c, []payload{{}, {}}, "payload")
	assert.Equal(t, 400, code)
	require.Error(t, err)
	// comma-joined list contains two Name entries, one per bad element
	parts := strings.Split(strings.TrimPrefix(err.Error(), ErrMsgMissingRequiredFields+" : "), ",")
	assert.Equal(t, []string{"Name", "Name"}, parts)

	// empty slice contributes no missing fields and returns the default 500
	code, err = ValidateBoundData(c, []payload{}, "payload")
	assert.Equal(t, 500, code)
	assert.NoError(t, err)

	// slice of all-valid elements also returns the default 500
	code, err = ValidateBoundData(c, []payload{{Name: "a"}, {Name: "b"}}, "payload")
	assert.Equal(t, 500, code)
	assert.NoError(t, err)
}
