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

	// collect object IDs to filter on, either from a direct objectid
	// query param or by resolving the object-type filter against the
	// registry. objecttypename is required; objectversion and
	// objectnamespace progressively narrow the resolved set.
	var ids []uint
	if directObjectId := c.QueryParam("objectid"); directObjectId != "" {
		parsed, err := strconv.ParseUint(directObjectId, 10, 64)
		if err != nil {
			return apiserver_lib.ResponseStatus400(c, pageParams,
				fmt.Errorf("invalid objectid %q: %w", directObjectId, err), objectType)
		}
		ids = []uint{uint(parsed)}
	}

	targetTypeName := c.QueryParam("objecttypename")
	targetVersion := c.QueryParam("objectversion")
	targetNamespace := c.QueryParam("objectnamespace")
	targetName := c.QueryParam("objectname")
	switch {
	case targetTypeName != "" && targetName != "":
		if len(ids) > 0 {
			return apiserver_lib.ResponseStatus400(
				c, pageParams,
				errors.New("provide either objectid or (objecttypename + objectname), not both"),
				objectType,
			)
		}
		qualifiedTypes, resolveErr := resolveObjectType(h.DB, targetTypeName)
		if resolveErr != nil {
			h.Logger.Error("handler error: error resolving kind", zap.Error(resolveErr))
			return apiserver_lib.ResponseStatus400(c, pageParams, resolveErr, objectType)
		}
		// progressively narrow the resolved set by version and namespace
		// when those params were supplied
		qualifiedTypes = filterQualifiedTypes(qualifiedTypes, targetNamespace, targetVersion)
		if len(qualifiedTypes) == 0 {
			return apiserver_lib.ResponseStatus400(c, pageParams,
				fmt.Errorf("kind %q is not registered (or no version/namespace match)", targetTypeName), objectType)
		}
		// look up the named object across every registered version
		for _, qt := range qualifiedTypes {
			id, lookupErr := GetObjectIDByName(h.DB, qt, targetName)
			if lookupErr == nil {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return apiserver_lib.ResponseStatus400(c, pageParams,
				fmt.Errorf("no object found with name %q for kind %q", targetName, targetTypeName), objectType)
		}
	case targetTypeName != "" || targetName != "":
		return apiserver_lib.ResponseStatus400(
			c, pageParams,
			errors.New("objecttypename and objectname must be provided together"),
			objectType,
		)
	}

	pagination := new(apiserver_lib.Pagination)
	pagination.Limit = pageParams.Limit

	records := &[]v0.Event{}
	var returnedCount int64

	// apply the object id filter only when ids were supplied
	applyObjectIdFilter := func(query *gorm.DB) *gorm.DB {
		if len(ids) == 0 {
			return query
		}
		return query.Where("v0_attached_object_references.object_id IN ?", ids)
	}

	switch {
	case pageParams.QueryId == "":
		// no query ID provided, so the client is not requesting a specific page of results
		// count total number of objects
		var totalCount int64
		countQuery := h.DB.Model(&v0.Event{}).Joins(
			"INNER JOIN v0_attached_object_references ON v0_events.attached_object_reference_id = v0_attached_object_references.id",
		)
		if result := applyObjectIdFilter(countQuery).Where(&filter).Count(&totalCount); result.Error != nil {
			h.Logger.Error("handler error: error counting objects", zap.Error(result.Error))
			return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
		}

		// see if total count is greater than the limit
		pagination.HasMore = totalCount > pagination.Limit

		switch pagination.HasMore {
		case false:
			// if we don't have to paginate, return all records
			findQuery := h.DB.Order("ID asc").Joins(
				"INNER JOIN v0_attached_object_references ON v0_events.attached_object_reference_id = v0_attached_object_references.id",
			)
			if result := applyObjectIdFilter(findQuery).Where(&filter).Find(records); result.Error != nil {
				h.Logger.Error("handler error: error finding objects", zap.Error(result.Error))
				return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
			}
			returnedCount = int64(len(*records))

		case true:
			viewName, queryId := GenerateMaterializedViewName()

			// create the materialized view; object id filter is included only when ids were supplied
			whereClause := ""
			if len(ids) > 0 {
				idStrs := make([]string, len(ids))
				for i, id := range ids {
					idStrs[i] = fmt.Sprintf("'%d'", id)
				}
				whereClause = fmt.Sprintf(" WHERE v0_attached_object_references.object_id IN (%s)", strings.Join(idStrs, ", "))
			}
			createView := fmt.Sprintf(
				"CREATE MATERIALIZED VIEW %s AS SELECT v0_events.* FROM v0_events INNER JOIN v0_attached_object_references ON v0_events.attached_object_reference_id = v0_attached_object_references.id%s ORDER BY v0_events.id ASC",
				viewName,
				whereClause,
			)
			if result := h.DB.Exec(createView); result.Error != nil {
				h.Logger.Error("handler error: error creating materialized view", zap.Error(result.Error))
				return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
			}

			// create an ID index on the materialized view
			createIdIndex := fmt.Sprintf("CREATE INDEX ON %s (ID)", viewName)
			if result := h.DB.Exec(createIdIndex); result.Error != nil {
				h.Logger.Error("handler error: error creating ID index", zap.Error(result.Error))
				return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
			}

			pagination.QueryId = queryId

			query := fmt.Sprintf("SELECT * FROM %s ORDER BY ID ASC LIMIT %d", viewName, pageParams.Limit)
			if result := h.DB.Raw(query).Find(records); result.Error != nil {
				h.Logger.Error("handler error: error finding objects", zap.Error(result.Error))
				return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
			}
			returnedCount = int64(len(*records))
			if len(*records) > 0 {
				pagination.NextCursor = *(*records)[len(*records)-1].ID
			} else {
				pagination.NextCursor = 0
			}
		}

	case pageParams.QueryId != "" && pageParams.Cursor == 0:
		// client provided a query ID but no cursor, so we cannot fetch the next page of results
		return apiserver_lib.ResponseStatus400(c, pageParams, errors.New("cursor is required when query ID is provided"), objectType)

	case pageParams.QueryId != "" && pageParams.Cursor != 0:
		// use query ID to find the materialized view name
		viewName, err := h.GetMaterializedViewName(pageParams.QueryId)
		if err != nil {
			h.Logger.Error("handler error: error finding materialized view", zap.Error(err))
			return apiserver_lib.ResponseStatus500(c, pageParams, err, objectType)
		}

		pagination.QueryId = pageParams.QueryId

		// fetch records from the materialized view based on cursor
		recordsQuery := fmt.Sprintf("SELECT * FROM %s WHERE ID > %d ORDER BY ID ASC LIMIT %d", viewName, pageParams.Cursor, pageParams.Limit)
		if result := h.DB.Raw(recordsQuery).Find(records); result.Error != nil {
			h.Logger.Error("handler error: error finding records", zap.Error(result.Error))
			return apiserver_lib.ResponseStatus500(c, pageParams, result.Error, objectType)
		}

		returnedCount = int64(len(*records))

		// set the next cursor
		if len(*records) > 0 {
			pagination.NextCursor = *(*records)[len(*records)-1].ID
		} else {
			pagination.NextCursor = 0
		}

		// see if we fetched the last of the records
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

// filterQualifiedTypes narrows the resolved-type list to entries
// matching the optional namespace and/or version. Empty filter values
// match anything. FQTNs are "<namespace>/<version>.<TypeName>".
func filterQualifiedTypes(qualifiedTypes []string, namespace, version string) []string {
	// no filters supplied - everything passes through unchanged
	if namespace == "" && version == "" {
		return qualifiedTypes
	}

	// allocate a fresh backing array sized for the worst case (all
	// entries pass); the [:0:0] form keeps cap separate from the
	// input slice so we don't accidentally overwrite the caller's data
	out := qualifiedTypes[:0:0]

	for _, qt := range qualifiedTypes {
		// parse the FQTN into parts so we can match on namespace and
		// version independently
		ns, ver, _, ok := parseQualifiedType(qt)
		if !ok {
			// malformed FQTN; drop rather than fail the whole query
			continue
		}

		// drop entries whose namespace doesn't match the filter (when set)
		if namespace != "" && ns != namespace {
			continue
		}

		// drop entries whose version doesn't match the filter (when set)
		if version != "" && ver != version {
			continue
		}

		// passed all active filters - keep it
		out = append(out, qt)
	}
	return out
}

// enrichEventsWithObjectInfo populates ObjectType, ObjectID, and ObjectName
// on each event from the joined attached object reference and a per-type
// batched name lookup.
func enrichEventsWithObjectInfo(db *gorm.DB, events []v0.Event, log *zap.Logger) error {
	if len(events) == 0 {
		return nil
	}

	// collect distinct event ids to look up their AOR rows via the
	// (attached_object_type, attached_object_id) composite index
	eventIDs := make([]uint, 0, len(events))
	seen := map[uint]struct{}{}
	for _, e := range events {
		if e.ID == nil {
			continue
		}
		if _, ok := seen[*e.ID]; ok {
			continue
		}
		seen[*e.ID] = struct{}{}
		eventIDs = append(eventIDs, *e.ID)
	}
	if len(eventIDs) == 0 {
		return nil
	}

	eventType := util.TypeName(v0.Event{})
	var aors []v0.AttachedObjectReference
	if err := db.
		Where("attached_object_type = ? AND attached_object_id IN ?", eventType, eventIDs).
		Find(&aors).Error; err != nil {
		return fmt.Errorf("failed to load attached object references: %w", err)
	}
	aorByEventID := make(map[uint]v0.AttachedObjectReference, len(aors))
	for _, a := range aors {
		if a.AttachedObjectID != nil {
			aorByEventID[*a.AttachedObjectID] = a
		}
	}

	for i := range events {
		e := &events[i]
		if e.ID == nil {
			continue
		}
		a, ok := aorByEventID[*e.ID]
		if !ok {
			continue
		}
		e.ObjectType = a.ObjectType
		e.ObjectID = a.ObjectID
	}

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

	// dispatch each type to core sql or module http and collect resolved names
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
