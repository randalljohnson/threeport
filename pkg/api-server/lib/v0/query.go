package v0

import (
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// QueryParamIncludeDeleted is the URL query parameter that bypasses the
// soft-delete filter on Get handlers when set to "true". Used for historical
// lookups such as resolving the name of an object that has since been deleted.
const QueryParamIncludeDeleted = "includedeleted"

// QueryScopes returns the gorm scopes derived from URL query parameters on c.
// Each entry transforms a *gorm.DB to apply one request-driven behavior. New
// behavior modifiers are added here; generated handlers don't change.
func QueryScopes(c echo.Context) []func(*gorm.DB) *gorm.DB {
	var scopes []func(*gorm.DB) *gorm.DB
	if c.QueryParam(QueryParamIncludeDeleted) == "true" {
		scopes = append(scopes, func(db *gorm.DB) *gorm.DB {
			return db.Unscoped()
		})
	}
	return scopes
}
