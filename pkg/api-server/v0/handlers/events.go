package handlers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	echo "github.com/labstack/echo/v4"
	zap "go.uber.org/zap"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// @Summary gets all events joined with attached object references.
// @Description Get all events joined with attached object references from the Threeport database.
// @ID get-v0-events-join-attached-object-references
// @Accept json
// @Produce json
// @Param objectid query string false "filter events by object ID"
// @Param objecttypename query string false "filter events by object type name (with objectname); CamelCase Go TypeName like 'WorkloadInstance'"
// @Param objectversion query string false "narrow objecttypename match to one version (e.g. 'v0')"
// @Param objectnamespace query string false "narrow objecttypename match to one api namespace (e.g. 'threeport.io')"
// @Param objectname query string false "filter events by object name (with objecttypename)"
// @Success 200 {object} v0.Response "OK"
// @Failure 400 {object} v0.Response "Bad Request"
// @Failure 500 {object} v0.Response "Internal Server Error"
// @Router /v0/events-join-attached-object-references [GET]
func (h Handler) GetEventsJoinAttachedObjectReferences(c echo.Context) error {
	objectType := v0.ObjectTypeEvent

	// FQTN of Event - written to AOR.AttachedObjectType when an event
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
		return apiserver_lib.ResponseStatus500(c, pageParams, err, objectType)
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

	var ids []uint
	switch {
	case targetTypeName == "" && targetName == "" && directObjectId == "":
		// no filter supplied; fall through to the unfiltered query

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
		// type + id - id is already the row, no name lookup needed
		parsed, err := strconv.ParseUint(directObjectId, 10, 64)
		if err != nil {
			return apiserver_lib.ResponseStatus400(c, pageParams,
				fmt.Errorf("invalid objectid %q: %w", directObjectId, err), objectType)
		}
		ids = []uint{uint(parsed)}

	case targetName != "":
		// type + name - resolve the type name to one or more FQTNs,
		// then look up the named object under each
		fullyQualifiedTypes, resolveErr := apiserver_lib.ResolveObjectType(h.DB, targetTypeName)
		if resolveErr != nil {
			h.Logger.Error("handler error: error resolving kind", zap.Error(resolveErr))
			return apiserver_lib.ResponseStatus400(c, pageParams, resolveErr, objectType)
		}
		// progressively narrow the resolved set by version and namespace
		// when those params were supplied
		fullyQualifiedTypes = apiserver_lib.FilterQualifiedTypes(fullyQualifiedTypes, targetNamespace, targetVersion)
		if len(fullyQualifiedTypes) == 0 {
			return apiserver_lib.ResponseStatus404(c, pageParams,
				fmt.Errorf("kind %q is not registered (or no version/namespace match)", targetTypeName), objectType)
		}
		// look up the named object across every registered version;
		// each version may yield zero or more ids (duplicates are a
		// data integrity bug and the resolver returns them all)
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

	// apply the object id filter only when ids were supplied; with no
	// ids the caller wants every event row to come back through the
	// JOIN to AOR
	applyObjectIdFilter := func(query *gorm.DB) *gorm.DB {
		if len(ids) == 0 {
			return query
		}
		return query.Where("v0_attached_object_references.object_id IN ?", ids)
	}

	switch {
	case pageParams.QueryId == "":
		// first-page request: no QueryId means the client is asking
		// for the start of a fresh result set, not a continuation

		// count total number of objects so we can decide whether
		// pagination is needed at all. The JOIN matches each event row
		// to its AOR row via AOR.attached_object_type = <event FQTN>
		// AND AOR.attached_object_id = v0_events.id (the polymorphic
		// FK that AOR uses to point at any kind of attached object).
		var totalCount int64
		countQuery := h.DB.Model(&v0.Event{}).Joins(
			`INNER JOIN v0_attached_object_references
				ON v0_attached_object_references.attached_object_type = ?
				AND v0_attached_object_references.attached_object_id = v0_events.id`,
			fullyQualifiedEventType,
		)
		if result := applyObjectIdFilter(countQuery).Where(&filter).Count(&totalCount); result.Error != nil {
			h.Logger.Error("handler error: error counting objects", zap.Error(result.Error))
			return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
		}

		// total greater than the limit means the client will need to
		// page through the result set; HasMore signals that
		pagination.HasMore = totalCount > pagination.Limit

		switch pagination.HasMore {
		case false:
			// small result set: skip the materialized view machinery
			// and return everything in one shot. Same JOIN shape as
			// the count query above.
			findQuery := h.DB.Order("ID asc").Joins(
				`INNER JOIN v0_attached_object_references
					ON v0_attached_object_references.attached_object_type = ?
					AND v0_attached_object_references.attached_object_id = v0_events.id`,
				fullyQualifiedEventType,
			)
			if result := applyObjectIdFilter(findQuery).Where(&filter).Find(records); result.Error != nil {
				h.Logger.Error("handler error: error finding objects", zap.Error(result.Error))
				return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
			}
			returnedCount = int64(len(*records))

		case true:
			// large result set: materialize the join so subsequent
			// cursor-based page requests can scan a stable view
			// instead of re-running the join each time
			viewName, queryId := GenerateMaterializedViewName()

			// add the object id filter when ids were supplied so the
			// materialized view only contains rows the client cares
			// about. ids are formatted as quoted strings to keep the
			// substitution simple even though they're uints
			whereClause := ""
			if len(ids) > 0 {
				idStrs := make([]string, len(ids))
				for i, id := range ids {
					idStrs[i] = fmt.Sprintf("'%d'", id)
				}
				whereClause = fmt.Sprintf(" WHERE v0_attached_object_references.object_id IN (%s)", strings.Join(idStrs, ", "))
			}

			// build and execute the CREATE MATERIALIZED VIEW. Same
			// JOIN shape as the count/find queries above, embedded
			// inline since the view definition is raw SQL.
			createView := fmt.Sprintf(`
				CREATE MATERIALIZED VIEW %s AS
				SELECT v0_events.*
				FROM v0_events
				INNER JOIN v0_attached_object_references
					ON v0_attached_object_references.attached_object_type = '%s'
					AND v0_attached_object_references.attached_object_id = v0_events.id
				%s
				ORDER BY v0_events.id ASC
			`,
				viewName,
				fullyQualifiedEventType,
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
		// continuation request: client gave a QueryId+Cursor pair, so
		// resume from the materialized view we created in a prior call

		// use the query ID to find the materialized view name (the view
		// name is deterministic from the queryId)
		viewName, err := h.GetMaterializedViewName(pageParams.QueryId)
		if err != nil {
			h.Logger.Error("handler error: error finding materialized view", zap.Error(err))
			return apiserver_lib.ResponseStatus500(c, pageParams, err, objectType)
		}

		// preserve the queryId across pages so the client keeps using
		// the same view for subsequent continuation requests
		pagination.QueryId = pageParams.QueryId

		// fetch the next page from the view starting just past the
		// previous cursor. the ID index built at create-time keeps
		// this O(limit) rather than O(view size)
		recordsQuery := fmt.Sprintf("SELECT * FROM %s WHERE ID > %d ORDER BY ID ASC LIMIT %d", viewName, pageParams.Cursor, pageParams.Limit)
		if result := h.DB.Raw(recordsQuery).Find(records); result.Error != nil {
			h.Logger.Error("handler error: error finding records", zap.Error(result.Error))
			return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
		}

		returnedCount = int64(len(*records))

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
	}

	// enrich records with attached object reference fields and resolved
	// object names; failures are logged so events still come back when
	// resolution can't fully complete
	if err := enrichEventsWithObjectInfo(h.DB, *records, h.Logger); err != nil {
		h.Logger.Error("handler error: error enriching events with object info", zap.Error(err))
	}

	// construct response
	response, err := apiserver_lib.CreateResponse(
		&apiserver_lib.Meta{
			Pagination:  *pagination,
			ObjectCount: returnedCount,
		},
		*records,
		objectType,
	)
	if err != nil {
		h.Logger.Error("handler error: error creating response", zap.Error(err))
		return apiserver_lib.ResponseStatus500(c, pageParams, err, objectType)
	}

	return apiserver_lib.ResponseStatus200(c, *response)
}

// enrichEventsWithObjectInfo populates ObjectType, ObjectID, and ObjectName
// on each event from the joined attached object reference and a per-type
// batched name lookup.
func enrichEventsWithObjectInfo(db *gorm.DB, events []v0.Event, log *zap.Logger) error {
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

	// dispatch each type to core sql or module http and collect
	// resolved names; failures are logged so events still come back
	// (rendered id-only) when name resolution fails for some types
	namesByType := make(map[string]map[uint]string, len(idsByType))
	for typ, idSet := range idsByType {
		ids := make([]uint, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		names, err := GetObjectNames(db, typ, ids, true)
		if err != nil {
			log.Error("failed to resolve object names", zap.String("objectType", typ), zap.Error(err))
			continue
		}
		namesByType[typ] = names
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
