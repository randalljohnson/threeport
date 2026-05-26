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

// QueryBinder binds request query params to struct fields using the
// lowercased Go field name as the param key. The convention drops
// the need for `query:"..."` struct tags on api types - the param
// name is derived from the field's name, period.
//
// Body and path-parameter binding fall through to echo's default
// binder. Query binding only fires for GET / DELETE / HEAD, matching
// the default binder's behavior (other methods take their input from
// the request body).
type QueryBinder struct {
	fallback echo.DefaultBinder
}

// NewQueryBinder returns a QueryBinder ready to register on an Echo
// instance via e.Binder = NewQueryBinder().
func NewQueryBinder() *QueryBinder { return &QueryBinder{} }

// Bind delegates path-param and body binding to the default binder,
// then applies the lowercased-field-name convention for query params
// on methods that read input from the URL.
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

// bindQueryParams walks the target struct, matching each settable field
// against url params by strings.ToLower(field.Name). Skips fields with
// no matching param so missing params don't zero out existing values.
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
		// pointers to slices, maps, primitives etc. don't carry query
		// params; nothing to do here.
		return nil
	}
	return bindStructFields(qp, v)
}

// bindStructFields recurses through embedded anonymous structs (Common,
// Definition, Instance, Reconciliation) so their fields participate too.
func bindStructFields(qp url.Values, v reflect.Value) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)

		if field.Anonymous && fv.Kind() == reflect.Struct {
			if err := bindStructFields(qp, fv); err != nil {
				return err
			}
			continue
		}

		if !fv.CanSet() {
			continue
		}

		paramName := strings.ToLower(field.Name)
		raw, ok := qp[paramName]
		if !ok || len(raw) == 0 {
			continue
		}
		if err := setFieldFromString(fv, raw[0]); err != nil {
			return fmt.Errorf("QueryBinder: failed to bind %s=%q: %w", paramName, raw[0], err)
		}
	}
	return nil
}

// setFieldFromString writes val into fv, allocating a backing value
// for pointer fields so callers don't have to pre-initialize. Supports
// the primitive kinds threeport api types use (string, bool, int*,
// uint*, float*); anything else returns an error rather than silently
// skip.
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
