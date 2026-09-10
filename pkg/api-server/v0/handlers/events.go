package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	echo "github.com/labstack/echo/v4"
	zap "go.uber.org/zap"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// objectNamespacePattern matches a DNS-like api namespace such as
// threeport.io. Anchored so caller text is safe to interpolate into a
// LIKE predicate on the attached object's qualified type.
var objectNamespacePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)

// objectVersionPattern matches an api version token such as v0 or
// v1alpha1. Anchored so caller text is safe to interpolate into a
// LIKE predicate on the attached object's qualified type.
var objectVersionPattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// reasonPattern matches an event Reason as [a-zA-Z0-9_]+, such as
// SuccessfulCreate. Anchored so caller text is safe to interpolate into
// equality and LIKE predicates on event reason.
var reasonPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// materializedViewThresholdFloor is the minimum row count that triggers
// cursor pagination over a stable snapshot. Below max(limit*10, this
// floor), the listing returns the whole result set in one query and
// skips creating and dropping a materialized view.
const materializedViewThresholdFloor = 5000

// eventJoinAttachedObjectReferenceClause is the inner join from
// v0_events to v0_attached_object_references on the polymorphic
// columns the reference table uses to point at events. The `?`
// placeholder stands in for the event's fully qualified type. Used
// directly by gorm .Joins() chains; raw-SQL paths substitute the
// literal in via strings.Replace.
const eventJoinAttachedObjectReferenceClause = `INNER JOIN v0_attached_object_references
	ON v0_attached_object_references.attached_object_type = ?
	AND v0_attached_object_references.attached_object_id = v0_events.id`

// JoinEventsToAttachedObjectReferences chains the join above plus the
// soft-delete predicate on the reference rows.
func JoinEventsToAttachedObjectReferences(query *gorm.DB, fullyQualifiedEventType string) *gorm.DB {
	return query.
		Joins(eventJoinAttachedObjectReferenceClause, fullyQualifiedEventType).
		Where(apiserver_lib.LiveRowsFilter("v0_attached_object_references"))
}

// @Summary gets all events joined with attached object references.
// @Description Get all events joined with attached object references from the Threeport database.
// @ID get-v0-events-join-attached-object-references
// @Accept json
// @Produce json
// @Param objectid query string false "filter events by object ID"
// @Param objecttypename query string false "filter events by object type name (with objectname); CamelCase Go TypeName like 'KubernetesWorkloadInstance'"
// @Param objectversion query string false "narrow objecttypename match to one version (e.g. 'v0')"
// @Param objectnamespace query string false "narrow objecttypename match to one api namespace (e.g. 'threeport.io')"
// @Param objectname query string false "filter events by object name (with objecttypename)"
// @Param reason query string false "filter events by exact Reason match (case-sensitive [a-zA-Z0-9_]+, e.g. 'SuccessfulCreate')"
// @Param reasonprefix query string false "filter events by Reason prefix (case-sensitive [a-zA-Z0-9_]+, matches Reason values starting with this token)"
// @Success 200 {object} v0.Response "OK"
// @Failure 400 {object} v0.Response "Bad Request"
// @Failure 500 {object} v0.Response "Internal Server Error"
// @Router /v0/events-join-attached-object-references [GET]
func (h Handler) GetEventsJoinAttachedObjectReferences(c echo.Context) error {
	objectType := v0.ObjectTypeEvent

	// fully qualified type of Event - written to AOR.AttachedObjectType when an event
	// is recorded, and used here as the join key between v0_events
	// and v0_attached_object_references
	fullyQualifiedEventType := (&v0.Event{}).GetFullyQualifiedType()

	// get pagination parameters
	pageParams, err := c.(*apiserver_lib.CustomContext).GetPaginationParams()
	if err != nil {
		return apiserver_lib.ResponseStatus400(c, pageParams, err, objectType)
	}

	// bind filter
	var filter v0.Event
	if err := c.Bind(&filter); err != nil {
		h.Logger.Error("handler error: error binding filter", zap.Error(err))
		return apiserver_lib.ResponseStatusBindErr(c, pageParams, err, objectType)
	}

	// collect object IDs to filter on. The accepted shapes are:
	//   - nothing supplied                  -> no subject id filter
	//   - objectnamespace / objectversion   -> LIKE on object_type only
	//   - objecttypename + objectid         -> filter by id under type
	//   - objecttypename + objectname       -> resolve name to id(s) under type
	// Type is required with id or name; id+name together is ambiguous.
	targetTypeName := c.QueryParam("objecttypename")
	targetVersion := c.QueryParam("objectversion")
	targetNamespace := c.QueryParam("objectnamespace")
	targetName := c.QueryParam("objectname")
	directObjectId := c.QueryParam("objectid")
	targetReason := c.QueryParam("reason")
	targetReasonPrefix := c.QueryParam("reasonprefix")

	// validate namespace and version tokens before they enter a LIKE
	// predicate; the regexes reject anything outside the DNS-like /
	// alphanumeric shapes so caller text cannot inject SQL
	if targetNamespace != "" && !objectNamespacePattern.MatchString(targetNamespace) {
		return apiserver_lib.ResponseStatus400(c, pageParams,
			fmt.Errorf("invalid objectnamespace %q: expected DNS-like value", targetNamespace),
			objectType)
	}
	if targetVersion != "" && !objectVersionPattern.MatchString(targetVersion) {
		return apiserver_lib.ResponseStatus400(c, pageParams,
			fmt.Errorf("invalid objectversion %q: expected alphanumeric token", targetVersion),
			objectType)
	}
	// validate reason and reasonprefix before they enter equality or LIKE
	// predicates; reasonPattern rejects anything outside [a-zA-Z0-9_]+ so
	// caller text cannot inject SQL on the raw-SQL pagination paths
	if targetReason != "" && targetReasonPrefix != "" {
		return apiserver_lib.ResponseStatus400(c, pageParams,
			errors.New("provide either reason or reasonprefix, not both"),
			objectType)
	}
	if targetReason != "" && !reasonPattern.MatchString(targetReason) {
		return apiserver_lib.ResponseStatus400(c, pageParams,
			fmt.Errorf("invalid reason %q: expected CamelCase token", targetReason),
			objectType)
	}
	if targetReasonPrefix != "" && !reasonPattern.MatchString(targetReasonPrefix) {
		return apiserver_lib.ResponseStatus400(c, pageParams,
			fmt.Errorf("invalid reasonprefix %q: expected CamelCase token", targetReasonPrefix),
			objectType)
	}

	var ids []uint
	var fullyQualifiedTypes []string

	// resolveQualifiedTypes turns the targetTypeName into the set of
	// fully qualified types that match it, optionally narrowed by namespace/version.
	// shared by the type+id and type+name branches below so both
	// constrain the AOR subject filter to the right type set.
	resolveQualifiedTypes := func() ([]string, error) {
		types, err := apiserver_lib.GetObjectTypes(h.DB, targetTypeName)
		if err != nil {
			return nil, err
		}
		return apiserver_lib.FilterQualifiedTypes(types, targetNamespace, targetVersion), nil
	}

	// buildNamespaceVersionPattern returns the LIKE pattern that narrows
	// attached object type by namespace and version. Types are stored as
	// <namespace>/<version>.<TypeName>; active is false when neither is set.
	buildNamespaceVersionPattern := func() (pattern string, active bool) {
		switch {
		case targetNamespace != "" && targetVersion != "":
			return fmt.Sprintf("%s/%s.%%", targetNamespace, targetVersion), true
		case targetNamespace != "":
			return fmt.Sprintf("%s/%%", targetNamespace), true
		case targetVersion != "":
			return fmt.Sprintf("%%/%s.%%", targetVersion), true
		default:
			return "", false
		}
	}

	switch {
	case targetTypeName == "" && targetName == "" && directObjectId == "" && targetNamespace == "" && targetVersion == "":
		// no filter supplied; fall through to the unfiltered query

	case targetTypeName == "" && targetName == "" && directObjectId == "":
		// namespace or version without a bare kind, id, or name; skip type
		// resolution and let the object-type LIKE predicate below narrow rows

	case targetTypeName == "":
		// caller supplied a name or id but no type. type is the only
		// disambiguator for a polymorphic lookup, so this is rejected.
		return apiserver_lib.ResponseStatus400(
			c, pageParams,
			errors.New("objecttypename is required when filtering by objectid or objectname"),
			objectType,
		)

	case directObjectId != "" && targetName != "":
		// type + id + name is ambiguous - which filter wins?
		return apiserver_lib.ResponseStatus400(
			c, pageParams,
			errors.New("provide either objectid or objectname, not both"),
			objectType,
		)

	case directObjectId != "":
		// type + id. The type still has to resolve so the subject
		// filter can pin object_type; without it, an unrelated type
		// that happens to share the id leaks in. Multi-type bare
		// kinds surface every (resolved type, id) pair - narrow with
		// objectnamespace / objectversion.
		parsed, err := strconv.ParseUint(directObjectId, 10, 64)
		if err != nil {
			return apiserver_lib.ResponseStatus400(c, pageParams,
				fmt.Errorf("invalid objectid %q: %w", directObjectId, err), objectType)
		}
		ids = []uint{uint(parsed)}

		types, lookupErr := resolveQualifiedTypes()
		if lookupErr != nil {
			h.Logger.Error("handler error: error looking up object types", zap.Error(lookupErr))
			return apiserver_lib.ResponseStatus400(c, pageParams, lookupErr, objectType)
		}
		if len(types) == 0 {
			return apiserver_lib.ResponseStatus404(c, pageParams,
				fmt.Errorf("kind %q is not registered (or no version/namespace match)", targetTypeName), objectType)
		}
		fullyQualifiedTypes = types

	case targetName != "":
		// type + name - look up every fully qualified type that matches the type name,
		// then look up the named object under each
		types, lookupErr := resolveQualifiedTypes()
		if lookupErr != nil {
			h.Logger.Error("handler error: error looking up object types", zap.Error(lookupErr))
			return apiserver_lib.ResponseStatus400(c, pageParams, lookupErr, objectType)
		}
		if len(types) == 0 {
			return apiserver_lib.ResponseStatus404(c, pageParams,
				fmt.Errorf("kind %q is not registered (or no version/namespace match)", targetTypeName), objectType)
		}
		fullyQualifiedTypes = types

		// look up the named object across every matched fully qualified type; each
		// fully qualified type may yield zero or more ids - name uniqueness is not
		// enforced at the database level
		for _, fqt := range fullyQualifiedTypes {
			moreIds, lookupErr := GetObjectIDsByName(h.DB, fqt, targetName)
			if lookupErr == nil {
				ids = append(ids, moreIds...)
			}
		}
		if len(ids) == 0 {
			return apiserver_lib.ResponseStatus404(c, pageParams,
				fmt.Errorf("no object found with name %q for kind %q", targetName, targetTypeName), objectType)
		}

	default:
		// caller supplied type alone (no id, no name) - we don't
		// support type-only filtering, so report the supported shapes
		return apiserver_lib.ResponseStatus400(
			c, pageParams,
			errors.New("must provide either objecttypename + objectid, or objecttypename + objectname"),
			objectType,
		)
	}

	// pagination state is built up across the branches below and read
	// into the final response Meta
	pagination := new(apiserver_lib.Pagination)
	pagination.Limit = pageParams.Limit

	records := &[]v0.Event{}
	var returnedCount int64

	// buildReasonRawWhere returns a raw-SQL reason predicate, or
	// ("", false) when unset. Values are pre-validated against
	// reasonPattern, so they are safe to interpolate as literals.
	buildReasonRawWhere := func() (string, bool) {
		switch {
		case targetReason != "":
			return fmt.Sprintf("v0_events.reason = '%s'", targetReason), true
		case targetReasonPrefix != "":
			return fmt.Sprintf("v0_events.reason LIKE '%s%%'", targetReasonPrefix), true
		default:
			return "", false
		}
	}

	// apply the subject filter when ids or a namespace/version filter
	// were supplied. The id half is the Cartesian product of resolved
	// types and ids so a multi-type bare kind surfaces every matching
	// pair. The namespace/version half narrows object type via LIKE.
	applyObjectIdFilter := func(query *gorm.DB) *gorm.DB {
		if len(ids) > 0 {
			query = query.
				Where("v0_attached_object_references.object_type IN ?", fullyQualifiedTypes).
				Where("v0_attached_object_references.object_id IN ?", ids)
		}
		if pattern, active := buildNamespaceVersionPattern(); active {
			query = query.Where("v0_attached_object_references.object_type LIKE ?", pattern)
		}
		if targetReason != "" {
			query = query.Where("v0_events.reason = ?", targetReason)
		}
		if targetReasonPrefix != "" {
			query = query.Where("v0_events.reason LIKE ?", targetReasonPrefix+"%")
		}
		return query
	}

	switch {
	case pageParams.QueryId == "":
		// first-page request: no QueryId means the client is asking
		// for the start of a fresh result set, not a continuation

		// set the snapshot threshold to max(limit*10, floor) so larger
		// page sizes still paginate while small result sets stay one-shot
		threshold := pagination.Limit * 10
		if threshold < materializedViewThresholdFloor {
			threshold = materializedViewThresholdFloor
		}

		// probe with LIMIT threshold+1 so HasMore comes from row count
		// instead of a separate Count over the same join
		findQuery := JoinEventsToAttachedObjectReferences(
			h.DB.Order("event_time ASC, id ASC").Limit(int(threshold)+1),
			fullyQualifiedEventType,
		)
		if result := applyObjectIdFilter(findQuery).Where(&filter).Find(records); result.Error != nil {
			h.Logger.Error("handler error: error finding objects", zap.Error(result.Error))
			return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
		}
		pagination.HasMore = int64(len(*records)) > threshold

		switch pagination.HasMore {
		case false:
			// small result set: return the probe fetch in one shot
			returnedCount = int64(len(*records))

		case true:
			// large result set: pin a snapshot so subsequent cursor
			// pages see the same rows even under concurrent writes.
			// Materialized-view mode stores the join in a fresh view;
			// as-of-system-time mode captures an HLC and re-runs the join.

			// discard the probe rows so the snapshot path below refills
			// from its own query rather than appending to a partial result
			*records = (*records)[:0]

			// base WHERE excludes soft-deleted events and reference
			// rows; raw SQL doesn't pick up gorm's deleted_at
			// scoping. Subject filters (when supplied) AND on after.
			whereClause := " WHERE " + apiserver_lib.LiveRowsFilter("v0_events", "v0_attached_object_references")
			if len(ids) > 0 {
				// constrain both object_type and object_id; id alone
				// would let unrelated types with the same id leak in
				typeStrs := make([]string, len(fullyQualifiedTypes))
				for i, t := range fullyQualifiedTypes {
					typeStrs[i] = fmt.Sprintf("'%s'", t)
				}
				idStrs := make([]string, len(ids))
				for i, id := range ids {
					idStrs[i] = fmt.Sprintf("'%d'", id)
				}
				whereClause += fmt.Sprintf(
					" AND v0_attached_object_references.object_type IN (%s) AND v0_attached_object_references.object_id IN (%s)",
					strings.Join(typeStrs, ", "),
					strings.Join(idStrs, ", "),
				)
			}
			if pattern, active := buildNamespaceVersionPattern(); active {
				// narrow object type by namespace/version prefix; pattern
				// is regex-validated and safe to interpolate into LIKE
				whereClause += fmt.Sprintf(
					" AND v0_attached_object_references.object_type LIKE '%s'",
					pattern,
				)
			}
			if reasonFrag, active := buildReasonRawWhere(); active {
				// append the pre-validated reason predicate
				whereClause += " AND " + reasonFrag
			}

			// build the join clause once; used by both mode branches
			// below. Raw SQL, so substitute the literal event type in
			// place of gorm's `?` placeholder.
			joinClause := strings.Replace(
				eventJoinAttachedObjectReferenceClause,
				"?",
				fmt.Sprintf("'%s'", fullyQualifiedEventType),
				1,
			)

			switch h.paginationMode() {
			case apiserver_lib.PaginationModeAsOfSystemTime:
				// capture the HLC once; the client echoes it back on
				// every continuation so all pages read the same snapshot
				var hlc string
				if result := h.DB.Raw("SELECT cluster_logical_timestamp()").Scan(&hlc); result.Error != nil {
					h.Logger.Error("handler error: error capturing HLC snapshot", zap.Error(result.Error))
					return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
				}
				pagination.QueryId = hlc

				// re-run the join at the captured snapshot for the
				// first page. AS OF SYSTEM TIME sits between FROM and
				// WHERE (CRDB syntax); id ordering matches MV mode.
				query := fmt.Sprintf(`
					SELECT v0_events.*
					FROM v0_events
					%s
					AS OF SYSTEM TIME '%s'
					%s
					ORDER BY v0_events.id ASC
					LIMIT %d
				`,
					joinClause,
					hlc,
					whereClause,
					pageParams.Limit,
				)
				if result := h.DB.Raw(query).Find(records); result.Error != nil {
					h.Logger.Error("handler error: error finding objects", zap.Error(result.Error))
					return apiserver_lib.ResponseStatus500(c, pageParams, apiserver_lib.TranslatePaginationSessionError(result.Error), objectType)
				}
				returnedCount = int64(len(*records))

			default:
				// materialize the join so subsequent cursor-based page
				// requests can scan a stable view instead of re-running
				// the join each time
				viewName, queryId := GenerateMaterializedViewName()

				// create the materialized view in event-time order, with
				// id breaking ties within the same second
				createView := fmt.Sprintf(`
					CREATE MATERIALIZED VIEW %s AS
					SELECT v0_events.*
					FROM v0_events
					%s
					%s
					ORDER BY v0_events.event_time ASC, v0_events.id ASC
				`,
					viewName,
					joinClause,
					whereClause,
				)
				if result := h.DB.Exec(createView); result.Error != nil {
					h.Logger.Error("handler error: error creating materialized view", zap.Error(result.Error))
					return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
				}

				// index on ID so subsequent cursor pagination (WHERE ID > cursor)
				// doesn't full-scan the view
				createIdIndex := fmt.Sprintf("CREATE INDEX ON %s (ID)", viewName)
				if result := h.DB.Exec(createIdIndex); result.Error != nil {
					h.Logger.Error("handler error: error creating ID index", zap.Error(result.Error))
					return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
				}

				// expose the queryId so the client can request subsequent pages
				pagination.QueryId = queryId

				// fetch the first page off the new materialized view
				query := fmt.Sprintf("SELECT * FROM %s ORDER BY ID ASC LIMIT %d", viewName, pageParams.Limit)
				if result := h.DB.Raw(query).Find(records); result.Error != nil {
					h.Logger.Error("handler error: error finding objects", zap.Error(result.Error))
					return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
				}
				returnedCount = int64(len(*records))
			}

			// set NextCursor to the last record's ID so the client's
			// next request resumes at the row right after this one
			if len(*records) > 0 {
				pagination.NextCursor = *(*records)[len(*records)-1].ID
			} else {
				pagination.NextCursor = 0
			}
		}

	case pageParams.QueryId != "" && pageParams.Cursor == 0:
		// QueryId without Cursor is incoherent - we can't know which
		// page to return without a cursor position
		return apiserver_lib.ResponseStatus400(c, pageParams, errors.New("cursor is required when query ID is provided"), objectType)

	case pageParams.QueryId != "" && pageParams.Cursor != 0:
		// continuation request: client gave a QueryId+Cursor pair,
		// resume from the snapshot the first page anchored. QueryId
		// stays opaque: materialized-view mode reads a view suffix,
		// as-of-system-time mode reads an HLC decimal.

		// preserve the queryId across pages so the client keeps using
		// the same snapshot for subsequent continuation requests
		pagination.QueryId = pageParams.QueryId

		// materialized-view mode pages a named view and drops it at the
		// end; as-of-system-time mode leaves this empty so the drop skips
		var viewName string

		switch h.paginationMode() {
		case apiserver_lib.PaginationModeAsOfSystemTime:
			// treat the caller queryId as an HLC token; validate to
			// reject anything that would smuggle SQL into AS OF SYSTEM
			// TIME. On mismatch answer 400 with the restart hint.
			if !apiserver_lib.ValidHLCToken(pageParams.QueryId) {
				return apiserver_lib.ResponseStatus400(c, pageParams,
					errors.New("invalid queryid: not a valid HLC token; restart pagination with no queryid to obtain a fresh snapshot"),
					objectType)
			}

			// rebuild the join clause the first page used so the tail
			// of the result set is scanned at the same snapshot
			joinClause := strings.Replace(
				eventJoinAttachedObjectReferenceClause,
				"?",
				fmt.Sprintf("'%s'", fullyQualifiedEventType),
				1,
			)
			whereClause := " WHERE " + apiserver_lib.LiveRowsFilter("v0_events", "v0_attached_object_references")
			if len(ids) > 0 {
				typeStrs := make([]string, len(fullyQualifiedTypes))
				for i, t := range fullyQualifiedTypes {
					typeStrs[i] = fmt.Sprintf("'%s'", t)
				}
				idStrs := make([]string, len(ids))
				for i, id := range ids {
					idStrs[i] = fmt.Sprintf("'%d'", id)
				}
				whereClause += fmt.Sprintf(
					" AND v0_attached_object_references.object_type IN (%s) AND v0_attached_object_references.object_id IN (%s)",
					strings.Join(typeStrs, ", "),
					strings.Join(idStrs, ", "),
				)
			}
			if pattern, active := buildNamespaceVersionPattern(); active {
				// mirror the first-page object-type filter under the same snapshot
				whereClause += fmt.Sprintf(
					" AND v0_attached_object_references.object_type LIKE '%s'",
					pattern,
				)
			}
			if reasonFrag, active := buildReasonRawWhere(); active {
				// mirror the first-page reason filter under the same snapshot
				whereClause += " AND " + reasonFrag
			}
			whereClause += fmt.Sprintf(" AND v0_events.id > %d", pageParams.Cursor)

			recordsQuery := fmt.Sprintf(`
				SELECT v0_events.*
				FROM v0_events
				%s
				AS OF SYSTEM TIME '%s'
				%s
				ORDER BY v0_events.id ASC
				LIMIT %d
			`,
				joinClause,
				pageParams.QueryId,
				whereClause,
				pageParams.Limit,
			)
			if result := h.DB.Raw(recordsQuery).Find(records); result.Error != nil {
				pageErr := apiserver_lib.TranslatePaginationSessionError(result.Error)
				if errors.Is(pageErr, apiserver_lib.ErrPaginationSessionExpired) {
					return apiserver_lib.ResponseStatus400(c, pageParams, pageErr, objectType)
				}
				h.Logger.Error("handler error: error finding records", zap.Error(result.Error))
				return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
			}
			returnedCount = int64(len(*records))

		default:
			resolvedViewName, err := h.GetMaterializedViewName(pageParams.QueryId)
			if err != nil {
				if errors.Is(err, apiserver_lib.ErrInvalidPaginationQueryId) {
					return apiserver_lib.ResponseStatus400(c, pageParams, err, objectType)
				}
				h.Logger.Error("handler error: error finding materialized view", zap.Error(err))
				return apiserver_lib.ResponseStatus500(c, pageParams, err, objectType)
			}
			viewName = resolvedViewName
			if viewName == "" {
				return apiserver_lib.ResponseStatus400(c, pageParams,
					apiserver_lib.ErrPaginationSessionExpired, objectType)
			}

			// fetch the next page from the view starting just past the
			// previous cursor. the ID index built at create-time keeps
			// this O(limit) rather than O(view size)
			recordsQuery := fmt.Sprintf("SELECT * FROM %s WHERE ID > %d ORDER BY ID ASC LIMIT %d", viewName, pageParams.Cursor, pageParams.Limit)
			if result := h.DB.Raw(recordsQuery).Find(records); result.Error != nil {
				pageErr := apiserver_lib.TranslateDroppedViewError(result.Error, viewName)
				if errors.Is(pageErr, apiserver_lib.ErrPaginationSessionExpired) {
					return apiserver_lib.ResponseStatus400(c, pageParams, pageErr, objectType)
				}
				h.Logger.Error("handler error: error finding records", zap.Error(result.Error))
				return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
			}
			returnedCount = int64(len(*records))
		}

		// set the next cursor to the last record's ID, or 0 when the
		// page came back empty (caller can treat 0 as "no more")
		if len(*records) > 0 {
			pagination.NextCursor = *(*records)[len(*records)-1].ID
		} else {
			pagination.NextCursor = 0
		}

		// returnedCount >= limit means there's likely another page; a
		// smaller-than-limit page means we hit the tail
		pagination.HasMore = returnedCount >= pagination.Limit

		// drop the materialized view once the client reaches the last
		// page so storage is freed now rather than by the TTL sweeper.
		// Log failures without failing the response; only
		// materialized-view mode sets viewName.
		if !pagination.HasMore && viewName != "" {
			dropQuery := fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s", viewName)
			if result := h.DB.Exec(dropQuery); result.Error != nil {
				h.Logger.Error("handler error: error dropping materialized view on last page", zap.String("viewName", viewName), zap.Error(result.Error))
			}
		}
	}

	// enrich records with attached object reference fields and resolved
	// object names; failures are logged so events still come back when
	// resolution cannot fully complete
	if err := enrichEventsWithObjectInfo(c.Request().Context(), h.DB, *records, h.Logger); err != nil {
		h.Logger.Error("handler error: error enriching events with object info", zap.Error(err))
	}

	// encode the concrete []Event envelope directly. CreateResponse
	// reflect-copies each element into []Object first; on large pages
	// that boxing plus per-element json reflection dominates the tail.
	w := c.Response()
	w.Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(struct {
		Meta   apiserver_lib.Meta
		Type   string
		Data   []v0.Event
		Status apiserver_lib.Status
	}{
		Meta:   apiserver_lib.Meta{Pagination: *pagination, ObjectCount: returnedCount},
		Type:   objectType,
		Data:   *records,
		Status: apiserver_lib.Status{Code: http.StatusOK, Message: http.StatusText(http.StatusOK)},
	})
}

// enrichEventsWithObjectInfo populates ObjectType, ObjectID, and ObjectName
// on each event from the joined attached object reference and a per-type
// batched name lookup.
func enrichEventsWithObjectInfo(ctx context.Context, db *gorm.DB, events []v0.Event, log *zap.Logger) error {
	// no events to enrich - nothing to do
	if len(events) == 0 {
		return nil
	}

	// collect distinct event ids for the AOR lookup below
	eventIDs := make([]uint, 0, len(events))
	seen := map[uint]struct{}{}
	for _, e := range events {
		// skip events without an ID (shouldn't happen for persisted rows
		// but avoids a nil deref if it does)
		if e.ID == nil {
			continue
		}
		// dedupe so the AOR IN list stays one entry per event id
		if _, ok := seen[*e.ID]; ok {
			continue
		}
		seen[*e.ID] = struct{}{}
		eventIDs = append(eventIDs, *e.ID)
	}

	// nothing left after the ID dedupe pass; bail before issuing the AOR query
	if len(eventIDs) == 0 {
		return nil
	}

	// load AOR rows where this event is the attached side. The map below
	// keeps one AOR per event id (last write wins).
	fullyQualifiedEventType := (&v0.Event{}).GetFullyQualifiedType()
	var aors []v0.AttachedObjectReference
	if err := db.
		Where("attached_object_type = ? AND attached_object_id IN ?", fullyQualifiedEventType, eventIDs).
		Find(&aors).Error; err != nil {
		return fmt.Errorf("failed to load attached object references: %w", err)
	}

	// build an event-id -> AOR map so the per-event projection step
	// below is O(1) per lookup
	aorByEventID := make(map[uint]v0.AttachedObjectReference, len(aors))
	for _, a := range aors {
		if a.AttachedObjectID != nil {
			aorByEventID[*a.AttachedObjectID] = a
		}
	}

	// project AOR.ObjectType and AOR.ObjectID onto each event row
	// (these are gorm:"-" projection-only fields on the Event struct)
	for i := range events {
		e := &events[i]
		if e.ID == nil {
			continue
		}
		a, ok := aorByEventID[*e.ID]
		if !ok {
			// event has no matching AOR; leave the projection fields nil
			// and let the response render id-only when shown
			continue
		}
		e.ObjectType = a.ObjectType
		e.ObjectID = a.ObjectID
	}

	// group object ids by their qualified type so the name lookup can
	// fan out one batch per type (each batch hits either core SQL or
	// one module HTTP endpoint - see GetObjectNames)
	idsByType := map[string]map[uint]struct{}{}
	for _, e := range events {
		if e.ObjectType == nil || e.ObjectID == nil {
			continue
		}
		if idsByType[*e.ObjectType] == nil {
			idsByType[*e.ObjectType] = map[uint]struct{}{}
		}
		idsByType[*e.ObjectType][*e.ObjectID] = struct{}{}
	}

	// resolve names from the in-process cache first, then fetch misses
	// from core sql or module http. Log resolution failures so events
	// still return id-only for types that fail.
	namesByType := make(map[string]map[uint]string, len(idsByType))
	for typ, idSet := range idsByType {
		resolved := make(map[uint]string, len(idSet))
		misses := make([]uint, 0, len(idSet))
		for id := range idSet {
			if cached, ok := moduleNameCache.Get(typ, id); ok {
				resolved[id] = cached
				continue
			}
			misses = append(misses, id)
		}
		if len(misses) > 0 {
			fetched, err := GetObjectNames(ctx, db, typ, misses, true)
			if err != nil {
				log.Error("failed to resolve object names", zap.String("objectType", typ), zap.Error(err))
				// keep cache-hit names when the fetch fails so partial
				// resolution still beats id-only
				if len(resolved) > 0 {
					namesByType[typ] = resolved
				}
				continue
			}
			for id, name := range fetched {
				resolved[id] = name
				moduleNameCache.Put(typ, id, name)
			}
		}
		namesByType[typ] = resolved
	}

	// project the resolved name onto each event row when available;
	// events whose subject lookup failed keep ObjectName=nil
	for i := range events {
		e := &events[i]
		if e.ObjectType == nil || e.ObjectID == nil {
			continue
		}
		names, ok := namesByType[*e.ObjectType]
		if !ok {
			continue
		}
		if name, ok := names[*e.ObjectID]; ok {
			e.ObjectName = util.Ptr(name)
		}
	}

	return nil
}
