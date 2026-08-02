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

// objectNamespacePattern matches DNS-like api namespaces such as
// "sxalable.io" or "threeport.io". Anchored so the value can be safely
// interpolated into a LIKE clause on v0_attached_object_references.object_type.
var objectNamespacePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)

// objectVersionPattern matches api version tokens such as "v0" or
// "v1alpha1". Anchored so the value can be safely interpolated into a
// LIKE clause on v0_attached_object_references.object_type.
var objectVersionPattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// reasonPattern matches event Reason values, which are Go-identifier
// CamelCase tokens (e.g. "SuccessfulCreate", "Reconcile_Fail"). Anchored
// so the value can be safely interpolated into equality and LIKE
// predicates on v0_events.reason.
var reasonPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// materializedViewThresholdFloor is the minimum total-count above which
// the event listing spins up a materialized view for cursor pagination.
// Below the floor (or below limit*10 when the caller asks for larger
// pages), return the whole result set in a single query and skip the
// CREATE MATERIALIZED VIEW / DROP MATERIALIZED VIEW round trip.
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
// soft-delete predicate on the reference rows, so live events and live
// references come back together.
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
// @Param reason query string false "filter events by exact Reason match (case-sensitive CamelCase, e.g. 'SuccessfulCreate')"
// @Param reasonprefix query string false "filter events by Reason prefix (case-sensitive CamelCase, matches Reason values starting with this token)"
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
	//   - nothing supplied                  -> return every event
	//   - objecttypename + objectid         -> filter by id under type
	//   - objecttypename + objectname       -> resolve name to id(s) under type
	// Any other combination is rejected: type is always required when
	// filtering, and id+name together is ambiguous.
	targetTypeName := c.QueryParam("objecttypename")
	targetVersion := c.QueryParam("objectversion")
	targetNamespace := c.QueryParam("objectnamespace")
	targetName := c.QueryParam("objectname")
	directObjectId := c.QueryParam("objectid")
	targetReason := c.QueryParam("reason")
	targetReasonPrefix := c.QueryParam("reasonprefix")

	// validate the narrow-filter tokens that get interpolated into the
	// AOR object_type LIKE clause below. The regexes reject anything
	// outside the DNS-like namespace / alphanumeric-version shape so
	// caller-supplied text cannot inject SQL.
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
	// reason and reasonprefix flow into equality / LIKE predicates on
	// v0_events.reason. The regex restricts the accepted alphabet to
	// Go-identifier CamelCase tokens so caller text cannot inject SQL
	// when interpolated into the raw-SQL pagination paths below.
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
	// AOR.object_type to the caller-supplied namespace and version.
	// Types are stored as "<namespace>/<version>.<TypeName>", so patterns
	// anchor on the slash and dot separators. Returns active=false when
	// neither filter is set so callers can skip the extra predicate.
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
		// namespace or version supplied without a bare kind, id, or name.
		// skip type/id resolution and let the AOR object_type LIKE
		// predicate below narrow events to the requested api group.

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
		// enforced at the database level.
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

	// buildReasonRawWhere returns a raw-SQL predicate fragment for the
	// reason / reasonprefix filter, or ("", false) when neither is set.
	// Values are pre-validated against reasonPattern above, so they are
	// safe to interpolate into the returned literal.
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
	// were supplied. The id half is the Cartesian product
	// (object_type IN types AND object_id IN ids) - intentional, so a
	// multi-type bare kind surfaces every (resolved type, id) pair
	// instead of forcing namespace disambiguation up front. The
	// namespace/version half narrows AOR.object_type via a LIKE prefix
	// so an --api-group / --object-version call without a bare kind
	// still constrains the row set.
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

		// only spin up a materialized view when the result set is large
		// enough that keyset paging over a stable snapshot is worth the
		// CREATE MATERIALIZED VIEW cost. Under the threshold, return
		// everything in one shot and skip the view machinery entirely.
		// Threshold is max(limit*10, 5000): scales with client-requested
		// limit so a caller asking for larger pages still gets multi-page
		// behavior, and a hard floor keeps small result sets on the
		// single-shot path even when limit is low.
		threshold := pagination.Limit * 10
		if threshold < materializedViewThresholdFloor {
			threshold = materializedViewThresholdFloor
		}

		// probe the result set with a LIMIT threshold+1 fetch so the
		// pagination decision is made from returned row count instead
		// of a separate Count query duplicating the JOIN. When the row
		// count fits under the threshold, serve the fetched records
		// directly and skip the materialized view path.
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
			// small result set: everything already loaded in the probe
			// fetch above; return it in one shot
			returnedCount = int64(len(*records))

		case true:
			// large result set: discard the probe fetch and rebuild
			// through the pagination-mode path for stable pagination
			*records = (*records)[:0]
			// large result set: pin a snapshot so subsequent cursor
			// pages see the same rows even under concurrent writes.
			// the two modes are peers: MV materializes the join into a
			// fresh view; AOST captures an HLC and re-runs the join at
			// that timestamp on every page.

			// the probe fetch above already loaded threshold+1 rows;
			// discard them so the snapshot path below refills from its
			// own query rather than appending to a partial result
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
				// narrow AOR.object_type by qualified-type prefix so a
				// namespace-only or version-only filter still constrains
				// the row set. The regex-validated pattern is safe to
				// interpolate into the LIKE literal.
				whereClause += fmt.Sprintf(
					" AND v0_attached_object_references.object_type LIKE '%s'",
					pattern,
				)
			}
			if reasonFrag, active := buildReasonRawWhere(); active {
				// pre-validated by reasonPattern, safe to interpolate
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

			switch h.PaginationMode {
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

				// build and execute the CREATE MATERIALIZED VIEW.
				// materialize in causal order so the view reads as the
				// sequence the events actually happened in, with id
				// breaking intra-second ties.
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
		// resume from the snapshot the first-page call anchored. The
		// queryId opacity is preserved end-to-end: MV mode reads it as
		// a view suffix, AOST mode reads it as an HLC.

		// preserve the queryId across pages so the client keeps using
		// the same snapshot for subsequent continuation requests
		pagination.QueryId = pageParams.QueryId

		// MV mode pages over a named view and drops it once the client
		// walks off the end. AOST mode has no view to drop, so this
		// stays empty and the drop below is skipped.
		var viewName string

		switch h.PaginationMode {
		case apiserver_lib.PaginationModeAsOfSystemTime:
			// treat the caller queryId as an HLC token; validate to
			// reject anything that would smuggle SQL into AS OF SYSTEM
			// TIME. On mismatch we surface a 400 with the restart hint.
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
				// mirror the first-page filter so the continuation reads
				// the same subset of the snapshot; the regex-validated
				// pattern is safe to interpolate into the LIKE literal.
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
				h.Logger.Error("handler error: error finding records", zap.Error(result.Error))
				return apiserver_lib.ResponseStatus500(c, pageParams, apiserver_lib.TranslatePaginationSessionError(result.Error), objectType)
			}
			returnedCount = int64(len(*records))

		default:
			// use the query ID to find the materialized view name (the view
			// name is deterministic from the queryId)
			resolvedViewName, err := h.GetMaterializedViewName(pageParams.QueryId)
			if err != nil {
				h.Logger.Error("handler error: error finding materialized view", zap.Error(err))
				return apiserver_lib.ResponseStatus500(c, pageParams, err, objectType)
			}
			viewName = resolvedViewName

			// fetch the next page from the view starting just past the
			// previous cursor. the ID index built at create-time keeps
			// this O(limit) rather than O(view size)
			recordsQuery := fmt.Sprintf("SELECT * FROM %s WHERE ID > %d ORDER BY ID ASC LIMIT %d", viewName, pageParams.Cursor, pageParams.Limit)
			if result := h.DB.Raw(recordsQuery).Find(records); result.Error != nil {
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

		// drop the materialized view inline the moment the client walks
		// off the end of the result set so the backing storage is freed
		// immediately, not deferred to the TTL sweeper. Failures here are
		// logged, not returned: the response body is already correct, and
		// the TTL sweeper still drops the view on its next pass. Only MV
		// mode reaches this; AOST mode leaves viewName empty.
		if !pagination.HasMore && viewName != "" {
			dropQuery := fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s", viewName)
			if result := h.DB.Exec(dropQuery); result.Error != nil {
				h.Logger.Error("handler error: error dropping materialized view on last page", zap.String("viewName", viewName), zap.Error(result.Error))
			}
		}
	}

	// enrich records with attached object reference fields and resolved
	// object names; failures are logged so events still come back when
	// resolution can't fully complete.
	if err := enrichEventsWithObjectInfo(c.Request().Context(), h.DB, *records, h.Logger); err != nil {
		h.Logger.Error("handler error: error enriching events with object info", zap.Error(err))
	}

	// stream the response directly to the wire instead of round-tripping
	// through CreateResponse. CreateResponse builds a parallel []Object
	// slice by reflect-copying every event into an interface{}; on large
	// pages that boxing loop plus the follow-on per-element type reflection
	// inside encoding/json dominates the handler's tail latency.
	// Marshalling the concrete []v0.Event lets json cache the type once and
	// avoids the intermediate slice entirely.
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

	// collect distinct event ids to look up their AOR rows via the
	// (attached_object_type, attached_object_id) composite index
	eventIDs := make([]uint, 0, len(events))
	seen := map[uint]struct{}{}
	for _, e := range events {
		// skip events without an ID (shouldn't happen for persisted rows
		// but avoids a nil deref if it does)
		if e.ID == nil {
			continue
		}
		// dedupe: an event id may appear more than once in events when
		// pagination retries land overlapping pages
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

	// load every AOR row where this event is the attached side. The
	// (attached_object_type, attached_object_id) composite uniquely
	// identifies one AOR per event id.
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

	// consult the in-process name cache first, then dispatch the
	// remaining misses to core sql or module http; failures are logged
	// so events still come back (rendered id-only) when name resolution
	// fails for some types. Cache hits skip the resolver round trip
	// entirely so a hot repeat page pays only for the AOR load above.
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
				// keep any cache-hit names for this type so partial
				// resolution still degrades better than id-only
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
