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

// QueryBinder overrides echo's default binder so api types don't need
// `query:"..."` struct tags. Each settable struct field is bound from
// the query param keyed by strings.ToLower of the field name. A field
// named KubernetesWorkloadInstanceID binds the workloadinstanceid param.
//
// Path params and body binding fall through to echo.DefaultBinder.
// Query binding fires on GET, DELETE, and HEAD, matching the default
// binder's method branching.
//
// Behavior we don't carry over from the default binder, because no
// current api type needs it:
//   - `query:"-"` opt-out. Every exported field is bindable.
//   - Repeated params like ?foo=1&foo=2. We take raw[0] and drop the
//     rest. The default binder reads a slice via the query tag.
//   - time.Time, time.Duration, custom UnmarshalParam. Only string,
//     bool, int*, uint*, and float* are supported. Other kinds return
//     an error rather than skip silently.
//   - `form:"..."` tags on GET-like methods. Threeport doesn't use
//     form encoding for read endpoints.
//   - Non-anonymous embedded fields. Only anonymous embeds (Common,
//     Definition, Instance, Reconciliation) are recursed into.
//
// References:
//
//	echo source:  https://github.com/labstack/echo/blob/master/bind.go
//	echo binding: https://echo.labstack.com/docs/binding
//
// Gotchas:
//   - Renaming an exported field renames the wire-level query key
//     too. Treat field renames on api types as breaking changes.
//   - Two fields whose lowercased names would collide is not
//     supported. Go style prevents this in practice.
type QueryBinder struct {
	fallback echo.DefaultBinder
}

// NewQueryBinder returns a binder ready to register via
// e.Binder = NewQueryBinder().
func NewQueryBinder() *QueryBinder { return &QueryBinder{} }

// Bind runs the three-stage dispatch: path params, then query or body
// depending on HTTP method.
func (b *QueryBinder) Bind(i interface{}, c echo.Context) error {
	// stage 1: path params via the default binder
	if err := b.fallback.BindPathParams(c, i); err != nil {
		return err
	}

	// stage 2: read methods take input from the URL query, write
	// methods from the body. Matches echo.DefaultBinder.Bind.
	method := c.Request().Method
	if method == http.MethodGet || method == http.MethodDelete || method == http.MethodHead {
		return b.bindQueryParams(c.QueryParams(), i)
	}
	return b.fallback.BindBody(c, i)
}

// bindQueryParams unwraps the target pointer and dispatches to the
// per-field walk when it points at a struct.
func (b *QueryBinder) bindQueryParams(qp url.Values, i interface{}) error {
	// no params in the URL means no bind work to do
	if len(qp) == 0 {
		return nil
	}

	// echo always passes a non-nil pointer; surface a misuse loudly
	// rather than silently no-op
	v := reflect.ValueOf(i)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("QueryBinder: target must be a non-nil pointer, got %T", i)
	}
	v = v.Elem()

	// only structs carry per-field query semantics; a pointer to a
	// slice/map/primitive has nothing to walk
	if v.Kind() != reflect.Struct {
		return nil
	}
	return bindStructFields(qp, v)
}

// bindStructFields assigns each settable field of v from the query
// param matching its lowercased name. Recurses into anonymous embeds
// (Common, Definition, Instance, Reconciliation) so their fields
// participate as if declared on the outer type.
func bindStructFields(qp url.Values, v reflect.Value) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)

		// anonymous embed: descend so its fields look like the outer's
		if field.Anonymous && fv.Kind() == reflect.Struct {
			if err := bindStructFields(qp, fv); err != nil {
				return err
			}
			continue
		}

		// skip unexported / un-settable fields (reflect won't let us
		// write to them anyway, and they aren't part of the API surface)
		if !fv.CanSet() {
			continue
		}

		// look up the URL param by lowercased field name; missing
		// param means leave the field at its incoming value (do not
		// zero a pre-populated default)
		paramName := strings.ToLower(field.Name)
		raw, ok := qp[paramName]
		if !ok || len(raw) == 0 {
			continue
		}

		// matched: convert the raw string into the field's actual type
		if err := setFieldFromString(fv, raw[0]); err != nil {
			return fmt.Errorf("QueryBinder: failed to bind %s=%q: %w", paramName, raw[0], err)
		}
	}
	return nil
}

// setFieldFromString parses val into fv. Pointer fields are allocated
// first so callers don't have to pre-initialize. Unsupported kinds
// return an error rather than skip silently, so type drift surfaces
// at the first request.
func setFieldFromString(fv reflect.Value, val string) error {
	// pointer field: allocate a new T, recurse to set its element,
	// then point fv at it. Recursion is exactly one level - the inner
	// type is always a scalar by the time we land in the switch below.
	if fv.Kind() == reflect.Ptr {
		ev := reflect.New(fv.Type().Elem())
		if err := setFieldFromString(ev.Elem(), val); err != nil {
			return err
		}
		fv.Set(ev)
		return nil
	}

	// scalar field: parse + set based on the field's kind
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
