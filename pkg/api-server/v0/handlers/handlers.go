package handlers

import (
	"fmt"
	"reflect"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	api_v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// Handler is the main handler for the API server.  It contains the database connection,
// NATS connection, JetStream context, and logger.
type Handler struct {
	DB             *gorm.DB
	NC             *nats.Conn
	JS             nats.JetStreamContext
	Logger         *zap.Logger
	PaginationMode apiserver_lib.PaginationMode
}

// New() returns a new Handler configured with the given pagination mode.
func New(db *gorm.DB, nc *nats.Conn, rc nats.JetStreamContext, logger *zap.Logger, paginationMode apiserver_lib.PaginationMode) Handler {
	return Handler{DB: db, NC: nc, JS: rc, Logger: logger, PaginationMode: paginationMode}
}

// RequestDB returns the handler DB scoped to the HTTP request, with query
// scopes applied and the request context attached so GORM honors client
// cancellation and hooks can read per-request state.
func (h Handler) RequestDB(c echo.Context) *gorm.DB {
	return h.DB.
		WithContext(c.Request().Context()).
		Scopes(apiserver_lib.QueryScopes(c)...)
}

// RespondBlockedDelete writes a 409 with blockers rendered as
// <api-namespace>/<kebab-kind>/<name>, falling back to id when no name resolves.
func RespondBlockedDelete(c echo.Context, db *gorm.DB, blocked *api_v0.BlockedDeleteError) error {
	baseType := *blocked.AttachedRefs[0].ObjectType
	baseID := *blocked.AttachedRefs[0].ObjectID
	idsByType := map[string]map[uint]struct{}{
		baseType: {baseID: struct{}{}},
	}
	for _, ref := range blocked.AttachedRefs {
		if idsByType[*ref.AttachedObjectType] == nil {
			idsByType[*ref.AttachedObjectType] = map[uint]struct{}{}
		}
		idsByType[*ref.AttachedObjectType][*ref.AttachedObjectID] = struct{}{}
	}

	// resolve names per object type; on lookup failure, fall back to an
	// empty map so one bad type doesn't drop every blocker from the response.
	namesByType := make(map[string]map[uint]string, len(idsByType))
	for objectType, idSet := range idsByType {
		ids := make([]uint, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		names, err := GetObjectNames(c.Request().Context(), db, objectType, ids, false)
		if err != nil {
			names = map[uint]string{}
		}
		namesByType[objectType] = names
	}

	msg := api_v0.FormatBlockedDelete(blocked, namesByType)
	return apiserver_lib.ResponseStatus409(c, nil, fmt.Errorf("%s", msg), baseType)
}

// CreateMaterializedView creates a materialized view for a given object type and returns the
// view name and query ID. The views are used for result pagination in handlers that support pagination.
func (h Handler) CreateMaterializedView(queryTable string) (string, string, error) {
	// create the materialized view name
	viewName, queryId := GenerateMaterializedViewName()

	// create the materialized view
	createView := fmt.Sprintf("CREATE MATERIALIZED VIEW %s AS SELECT * FROM %s ORDER BY ID ASC", viewName, queryTable)
	if result := h.DB.Exec(createView); result.Error != nil {
		return "", "", fmt.Errorf("handler error: error creating materialized view with name %s: %w", viewName, result.Error)
	}

	// create an ID index on the materialized view
	createIdIndex := fmt.Sprintf("CREATE INDEX ON %s (ID)", viewName)
	if result := h.DB.Exec(createIdIndex); result.Error != nil {
		return "", "", fmt.Errorf("handler error: error creating ID index: %w", result.Error)
	}

	return viewName, queryId, nil
}

// GetMaterializedViewName finds the name of the materialized view created for a given pagination query ID.
func (h Handler) GetMaterializedViewName(queryId string) (string, error) {
	// find the materialized view name by query ID
	viewQuery := fmt.Sprintf("SELECT table_name FROM information_schema.tables WHERE table_type = 'VIEW' AND table_name LIKE 'paginated_%%_%s'", queryId)
	var viewName string
	if result := h.DB.Raw(viewQuery).Scan(&viewName); result.Error != nil {
		return "", fmt.Errorf("handler error: error finding materialized view: %w", result.Error)
	}

	return viewName, nil
}

// GetMaterializedViewRecords fetches records from a materialized view based on a cursor
// for pagination.  This function uses reflection to work with any slice type.  It will
// use a cursor to fetch the next page of results if a cursor is included in the page
// request parameters.
func (h Handler) GetMaterializedViewRecords(
	records interface{},
	viewName string,
	pageParams *apiserver_lib.PageRequestParams,
) (int64, error) {
	var recordsQuery string
	if pageParams.Cursor == 0 {
		recordsQuery = fmt.Sprintf("SELECT * FROM %s ORDER BY ID ASC LIMIT %d", viewName, pageParams.Limit)
	} else {
		recordsQuery = fmt.Sprintf("SELECT * FROM %s WHERE ID > %d ORDER BY ID ASC LIMIT %d", viewName, pageParams.Cursor, pageParams.Limit)
	}
	if result := h.DB.Raw(recordsQuery).Find(records); result.Error != nil {
		return 0, fmt.Errorf("handler error: error finding records: %w", result.Error)
	}

	// Use reflection to get the length of the slice
	recordsValue := reflect.ValueOf(records)
	if recordsValue.Kind() == reflect.Ptr {
		recordsValue = recordsValue.Elem()
	}
	if recordsValue.Kind() != reflect.Slice {
		return 0, fmt.Errorf("records must be a slice")
	}

	return int64(recordsValue.Len()), nil
}

// GenerateMaterializedViewName generates a standardized, unique materialized view name.
// The name contains the current timestamp for that is used to clean up materialized views
// that have passed a TTL. The query ID is used by the client to identify the view for
// subsequent pages of results.
func GenerateMaterializedViewName() (string, string) {
	queryId := util.RandomAlphaNumericString(16)
	viewName := fmt.Sprintf(
		"%s_%s_%s",
		apiserver_lib.PaginationViewPrefix,
		time.Now().Format("20060102150405"),
		queryId,
	)

	return viewName, queryId
}

// GetPaginatedRecordsAsOfSystemTime() fetches a page of records from queryTable
// against a stable HLC snapshot without creating a materialized view. When
// queryId is empty a fresh HLC is captured via cluster_logical_timestamp() and
// returned so the caller can echo it back as the pagination queryId; when
// queryId is non-empty it is used verbatim as the HLC. Reads use uses
// reflection to work with any slice type. Reads use AS OF SYSTEM TIME so
// subsequent pages see the same snapshot even under concurrent writes.
func (h Handler) GetPaginatedRecordsAsOfSystemTime(
	records interface{},
	queryTable string,
	queryId string,
	pageParams *apiserver_lib.PageRequestParams,
) (string, int64, error) {
	hlc := queryId
	if hlc == "" {
		// capture a fresh HLC to anchor the first page; every later page
		// echoes this token back so the whole result set sees the same
		// snapshot even under concurrent writes
		if result := h.DB.Raw("SELECT cluster_logical_timestamp()").Scan(&hlc); result.Error != nil {
			return "", 0, fmt.Errorf("handler error: error capturing HLC snapshot: %w", result.Error)
		}
	} else if !apiserver_lib.ValidHLCToken(hlc) {
		// reject caller-supplied tokens that aren't a bare decimal so a
		// stray queryid can't slip arbitrary SQL into the AS OF SYSTEM
		// TIME clause; only tokens produced above pass this check
		return "", 0, fmt.Errorf("handler error: invalid queryid: not a valid HLC token")
	}

	var recordsQuery string
	if pageParams.Cursor == 0 {
		recordsQuery = fmt.Sprintf(
			"SELECT * FROM %s AS OF SYSTEM TIME '%s' ORDER BY ID ASC LIMIT %d",
			queryTable, hlc, pageParams.Limit,
		)
	} else {
		recordsQuery = fmt.Sprintf(
			"SELECT * FROM %s AS OF SYSTEM TIME '%s' WHERE ID > %d ORDER BY ID ASC LIMIT %d",
			queryTable, hlc, pageParams.Cursor, pageParams.Limit,
		)
	}
	if result := h.DB.Raw(recordsQuery).Find(records); result.Error != nil {
		return hlc, 0, fmt.Errorf("handler error: error finding records: %w", result.Error)
	}

	// reflect through the slice pointer to get the returned row count
	recordsValue := reflect.ValueOf(records)
	if recordsValue.Kind() == reflect.Ptr {
		recordsValue = recordsValue.Elem()
	}
	if recordsValue.Kind() != reflect.Slice {
		return hlc, 0, fmt.Errorf("records must be a slice")
	}

	return hlc, int64(recordsValue.Len()), nil
}

// DispatchGetPaginatedRecords() fetches one page of results from queryTable
// using mode as the pagination strategy. Returns the queryId to echo back to
// the client (a view-name suffix in MaterializedView mode, an HLC token in
// AsOfSystemTime mode), the row count for this page, and any error.
//
// In MaterializedView mode a view is created on the initial call (empty
// pageParams.QueryId) and looked up by queryId on continuation; the view is
// dropped when this page is the last page (returned count < limit) so the
// TTL sweeper doesn't have to. In AsOfSystemTime mode a fresh HLC is captured
// on the initial call and the caller's queryId is used verbatim on
// continuation; page-SELECT errors are translated via
// TranslatePaginationSessionError so a GC-expired snapshot surfaces the
// restart-without-queryid hint.
func (h Handler) DispatchGetPaginatedRecords(
	mode apiserver_lib.PaginationMode,
	records interface{},
	queryTable string,
	pageParams *apiserver_lib.PageRequestParams,
) (string, int64, error) {
	switch mode {
	case apiserver_lib.PaginationModeAsOfSystemTime:
		hlc, count, err := h.GetPaginatedRecordsAsOfSystemTime(records, queryTable, pageParams.QueryId, pageParams)
		if err != nil {
			return hlc, count, apiserver_lib.TranslatePaginationSessionError(err)
		}
		return hlc, count, nil

	case apiserver_lib.PaginationModeMaterializedView:
		var viewName string
		var queryId string
		if pageParams.QueryId == "" {
			// first page: build the view and use its suffix as queryId
			var err error
			viewName, queryId, err = h.CreateMaterializedView(queryTable)
			if err != nil {
				return "", 0, err
			}
		} else {
			// continuation: resolve the view the previous call created
			queryId = pageParams.QueryId
			var err error
			viewName, err = h.GetMaterializedViewName(pageParams.QueryId)
			if err != nil {
				return queryId, 0, err
			}
		}

		count, err := h.GetMaterializedViewRecords(records, viewName, pageParams)
		if err != nil {
			return queryId, count, err
		}

		// a page smaller than the limit is the tail: drop the view now
		// so the TTL cleanup doesn't have to catch it later. Failures
		// are logged and swallowed since the TTL sweep is the safety net.
		if count < pageParams.Limit {
			dropQuery := fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s", viewName)
			if result := h.DB.Exec(dropQuery); result.Error != nil {
				h.Logger.Warn("handler error: error dropping materialized view", zap.String("viewName", viewName), zap.Error(result.Error))
			}
		}

		return queryId, count, nil

	default:
		return "", 0, fmt.Errorf("handler error: unknown pagination mode: %s", mode)
	}
}
