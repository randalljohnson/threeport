package v0

import "testing"

// TestPtrReturnsPointerToInputValue asserts Ptr wraps a value in a pointer that
// dereferences back to the same value across representative types.
func TestPtrReturnsPointerToInputValue(t *testing.T) {
	// exercise string
	sp := Ptr("hello")
	if sp == nil {
		t.Fatal("expected non-nil pointer for string input")
	}
	if *sp != "hello" {
		t.Errorf("expected %q, got %q", "hello", *sp)
	}

	// exercise int, including zero value
	ip := Ptr(0)
	if ip == nil || *ip != 0 {
		t.Errorf("expected pointer to 0, got %v", ip)
	}

	// exercise struct type
	type payload struct {
		Name string
		N    int
	}
	pp := Ptr(payload{Name: "a", N: 7})
	if pp == nil {
		t.Fatal("expected non-nil struct pointer")
	}
	if pp.Name != "a" || pp.N != 7 {
		t.Errorf("expected {a 7}, got %+v", *pp)
	}
}

// TestPtrEachCallReturnsDistinctPointer asserts consecutive Ptr calls with the
// same value return distinct pointers so callers get independent storage.
func TestPtrEachCallReturnsDistinctPointer(t *testing.T) {
	// two calls with identical input
	a := Ptr("x")
	b := Ptr("x")

	// pointers must differ but underlying values must match
	if a == b {
		t.Error("expected distinct pointers from separate Ptr calls")
	}
	if *a != *b {
		t.Errorf("expected equal values, got %q and %q", *a, *b)
	}
}

// TestDerefStringReturnsValueOrEmptyForNil covers the nil and non-nil branches
// of DerefString using a table.
func TestDerefStringReturnsValueOrEmptyForNil(t *testing.T) {
	empty := ""
	value := "threeport"

	cases := []struct {
		name string
		in   *string
		want string
	}{
		{name: "nil pointer yields empty string", in: nil, want: ""},
		{name: "pointer to empty string yields empty string", in: &empty, want: ""},
		{name: "pointer to populated string yields value", in: &value, want: "threeport"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// invoke DerefString on each input variant
			got := DerefString(tc.in)
			// assert the returned string matches the expected value
			if got != tc.want {
				t.Errorf("DerefString(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDerefReturnsValueOrZeroForNil asserts Deref returns the dereferenced
// value for a non-nil pointer and the type's zero value for nil, across
// several types.
func TestDerefReturnsValueOrZeroForNil(t *testing.T) {
	// nil string pointer returns ""
	if got := Deref[string](nil); got != "" {
		t.Errorf("Deref[string](nil) = %q, want empty", got)
	}

	// nil int pointer returns 0
	if got := Deref[int](nil); got != 0 {
		t.Errorf("Deref[int](nil) = %d, want 0", got)
	}

	// non-nil string pointer returns underlying value
	s := "hi"
	if got := Deref(&s); got != "hi" {
		t.Errorf("Deref(&%q) = %q, want %q", s, got, s)
	}

	// non-nil int pointer including zero value round-trips
	zero := 0
	if got := Deref(&zero); got != 0 {
		t.Errorf("Deref(&0) = %d, want 0", got)
	}
	seven := 7
	if got := Deref(&seven); got != 7 {
		t.Errorf("Deref(&7) = %d, want 7", got)
	}
}

// TestDerefStructZeroValueForNil asserts that for a struct type Deref returns
// the struct's zero literal when the pointer is nil.
func TestDerefStructZeroValueForNil(t *testing.T) {
	type payload struct {
		Name string
		N    int
	}

	// nil struct pointer returns the zero literal
	got := Deref[payload](nil)
	if got != (payload{}) {
		t.Errorf("Deref[payload](nil) = %+v, want zero struct", got)
	}

	// non-nil struct pointer returns underlying value
	p := payload{Name: "a", N: 3}
	if out := Deref(&p); out != p {
		t.Errorf("Deref(&p) = %+v, want %+v", out, p)
	}
}

// TestDerefPointerTypeZeroForNil asserts Deref of a *T where T itself is a
// pointer kind returns a nil inner pointer when the outer pointer is nil.
func TestDerefPointerTypeZeroForNil(t *testing.T) {
	// nil **string yields nil *string (T's zero value is nil)
	got := Deref[*string](nil)
	if got != nil {
		t.Errorf("Deref[*string](nil) = %v, want nil", got)
	}
}

// TestPtrDerefRoundTrip asserts that Ptr and Deref compose to the identity on
// representative types.
func TestPtrDerefRoundTrip(t *testing.T) {
	// string round-trips through Ptr then Deref
	if got := Deref(Ptr("z")); got != "z" {
		t.Errorf("round trip string: got %q, want %q", got, "z")
	}
	// int round-trips including zero value
	if got := Deref(Ptr(42)); got != 42 {
		t.Errorf("round trip int: got %d, want 42", got)
	}
}
