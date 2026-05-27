package v0

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// QueryBinder overrides echo's default request binder so threeport api
// types don't need `query:"..."` struct tags. Each settable struct
// field is bound from the query param keyed by strings.ToLower of the
// field name: a field named `WorkloadInstanceID` binds the
// `workloadinstanceid` param.
//
// Path-parameter binding, body binding, and the GET/DELETE/HEAD method
// branch are delegated to echo.DefaultBinder unchanged - we only
// replace the lookup rule for query params. This file is the source of
// truth for the override.
//
// Echo default binder behavior we mirror:
//   - Bind dispatches: path params, then query (on GET/DELETE/HEAD) or
//     body (on other methods).
//   - Validate is left to the registered echo.Validator; we don't
//     touch it.
//
// Echo default binder features we DON'T support (intentional cuts to
// match what threeport actually uses today - if you add a new api
// type that needs one of these, extend the binder rather than work
// around it):
//   - `query:"-"` opt-out: every exported field is bindable, no escape
//     hatch. Mitigated by the lowercased-name convention being so
//     specific it's hard to collide.
//   - Repeated params (?foo=1&foo=2): we take raw[0] and drop the
//     rest. Echo's default binds to a slice field via the `query` tag.
//   - time.Time, time.Duration, custom UnmarshalParam: not handled.
//     Only primitive kinds (string, bool, int*, uint*, float*).
//     Unsupported kinds error rather than silently skip so type drift
//     surfaces loudly.
//   - `form:"..."` tags on GET-like methods: not handled. Threeport
//     doesn't use form encoding for read endpoints.
//   - Non-anonymous embedded fields: only Anonymous embeds are
//     recursed into. Threeport's embedded types (Common, Definition,
//     Instance, Reconciliation) are all anonymous, so the limitation
//     is invisible in practice.
//
// References:
//   - echo.DefaultBinder source: https://github.com/labstack/echo/blob/master/bind.go
//   - echo binding docs: https://echo.labstack.com/docs/binding
//
// Gotchas:
//   - Renaming an exported field on a bound type silently renames the
//     query-param wire name; no `query:` tag pins it. Treat field
//     renames on api types as API-breaking.
//   - All exported field names must produce distinct lowercased
//     strings. Two fields named `ID` and `Id` would both want `id` -
//     don't do that. (Go style already prevents this; documented as
//     belt-and-suspenders.)
//   - Tests in binder_test.go pin the primitive-kind contract; extend
//     them when you add a kind to setFieldFromString.
type QueryBinder struct {
	fallback echo.DefaultBinder
}

// NewQueryBinder returns a QueryBinder ready to register on an Echo
// instance via e.Binder = NewQueryBinder().
func NewQueryBinder() *QueryBinder { return &QueryBinder{} }

// Bind dispatches to the same three stages as echo.DefaultBinder.Bind:
// path params, then query (read methods) or body (write methods).
// Only the query stage is overridden; the rest delegates to the
// default binder.
func (b *QueryBinder) Bind(i interface{}, c echo.Context) error {
	if err := b.fallback.BindPathParams(c, i); err != nil {
		return err
	}
	method := c.Request().Method
	if method == http.MethodGet || method == http.MethodDelete || method == http.MethodHead {
		return b.bindQueryParams(c.QueryParams(), i)
	}
	return b.fallback.BindBody(c, i)
}

// bindQueryParams resolves the target struct from i, then walks its
// fields setting each from the query param keyed by its lowercased
// name. Non-struct targets (a pointer to a slice, map, primitive) are
// no-ops because they don't carry per-field query semantics.
func (b *QueryBinder) bindQueryParams(qp url.Values, i interface{}) error {
	if len(qp) == 0 {
		return nil
	}
	v := reflect.ValueOf(i)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("QueryBinder: target must be a non-nil pointer, got %T", i)
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return nil
	}
	return bindStructFields(qp, v)
}

// bindStructFields walks v's fields and assigns each from the query
// param keyed by strings.ToLower(field.Name). Anonymous embedded
// structs are recursed into so their fields participate as if
// declared on the outer type - this is how Common, Definition,
// Instance, and Reconciliation fields become bindable on the wrapping
// api types.
func bindStructFields(qp url.Values, v reflect.Value) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)

		// flatten anonymous embeds so their fields look like the outer's
		if field.Anonymous && fv.Kind() == reflect.Struct {
			if err := bindStructFields(qp, fv); err != nil {
				return err
			}
			continue
		}

		// skip unexported or otherwise un-settable fields
		if !fv.CanSet() {
			continue
		}

		// skip fields with no matching param so we don't zero existing values
		raw, ok := qp[strings.ToLower(field.Name)]
		if !ok || len(raw) == 0 {
			continue
		}

		if err := setFieldFromString(fv, raw[0]); err != nil {
			return fmt.Errorf("QueryBinder: failed to bind %s=%q: %w", strings.ToLower(field.Name), raw[0], err)
		}
	}
	return nil
}

// setFieldFromString writes val into fv, allocating a backing value
// for pointer fields so callers don't have to pre-initialize. The
// recursion through reflect.Ptr is one level deep (pointer to
// scalar); the scalar path returns directly. Unsupported kinds error
// rather than silently no-op - that's load-bearing for catching the
// next "we added a time.Time query param" surprise loudly.
func setFieldFromString(fv reflect.Value, val string) error {
	if fv.Kind() == reflect.Ptr {
		ev := reflect.New(fv.Type().Elem())
		if err := setFieldFromString(ev.Elem(), val); err != nil {
			return err
		}
		fv.Set(ev)
		return nil
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(val)
	case reflect.Bool:
		n, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		fv.SetBool(n)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(val, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(val, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(val, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	default:
		return fmt.Errorf("unsupported field kind: %s", fv.Kind())
	}
	return nil
}
