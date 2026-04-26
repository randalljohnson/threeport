package handlers

import (
	"errors"
	"fmt"

	echo "github.com/labstack/echo/v4"
	zap "go.uber.org/zap"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// @Summary gets all events joined with attached object references.
// @Description Get all events joined with attached object references from
// the Threeport database. When an objectid query parameter is provided,
// results are filtered to events whose attached object reference points at
// that object; otherwise all events that have an attached object reference
// are returned. Alternatively, --for-style filtering accepts objecttype +
// objectname which are resolved server-side to an objectid.
// @ID get-v0-events-join-attached-object-references
// @Accept json
// @Produce json
// @Param objectid query string false "filter to events for this object ID"
// @Param objecttype query string false "filter to events for this object type (paired with objectname)"
// @Param objectname query string false "filter to events for this object name (paired with objecttype)"
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

	objectId := c.QueryParam("objectid")

	// resolve objecttype+objectname server-side into an objectid filter
	targetType := c.QueryParam("objecttype")
	targetName := c.QueryParam("objectname")
	switch {
	case targetType != "" && targetName != "":
		if objectId != "" {
			return apiserver_lib.ResponseStatus400(
				c,
				pageParams,
				errors.New("provide either objectid or (objecttype + objectname), not both"),
				objectType,
			)
		}
		id, lookupErr := LookupObjectIDByName(h.DB, targetType, targetName)
		if lookupErr != nil {
			h.Logger.Error("handler error: error resolving --for", zap.Error(lookupErr))
			return apiserver_lib.ResponseStatus400(c, pageParams, lookupErr, objectType)
		}
		objectId = fmt.Sprintf("%d", id)
	case targetType != "" || targetName != "":
		return apiserver_lib.ResponseStatus400(
			c,
			pageParams,
			errors.New("objecttype and objectname must be provided together"),
			objectType,
		)
	}

	pagination := new(apiserver_lib.Pagination)
	pagination.Limit = pageParams.Limit

	records := &[]v0.Event{}
	var returnedCount int64

	// apply the objectid filter only when one was supplied
	applyObjectIdFilter := func(query *gorm.DB) *gorm.DB {
		if objectId == "" {
			return query
		}
		return query.Where("v0_attached_object_references.object_id = ?", objectId)
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

			// create the materialized view; objectid filter is included only when supplied
			whereClause := ""
			if objectId != "" {
				whereClause = fmt.Sprintf(" WHERE v0_attached_object_references.object_id = '%s'", objectId)
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

	// enrich records with AOR fields and resolved object names; failures are
	// logged but don't fail the whole query so events still come back when
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
// on each event from the joined AttachedObjectReference and a per-type
// batched name lookup. Mutates events in place via gorm:"-" projection
// columns on v0.Event.
func enrichEventsWithObjectInfo(db *gorm.DB, events []v0.Event, log *zap.Logger) error {
	if len(events) == 0 {
		return nil
	}

	// collect distinct attached object reference ids
	aorIDs := make([]uint, 0, len(events))
	seen := map[uint]struct{}{}
	for _, e := range events {
		if e.AttachedObjectReferenceID == nil {
			continue
		}
		if _, ok := seen[*e.AttachedObjectReferenceID]; ok {
			continue
		}
		seen[*e.AttachedObjectReferenceID] = struct{}{}
		aorIDs = append(aorIDs, *e.AttachedObjectReferenceID)
	}
	if len(aorIDs) == 0 {
		return nil
	}

	var aors []v0.AttachedObjectReference
	if err := db.Where("id IN ?", aorIDs).Find(&aors).Error; err != nil {
		return fmt.Errorf("failed to load attached object references: %w", err)
	}
	aorByID := make(map[uint]v0.AttachedObjectReference, len(aors))
	for _, a := range aors {
		if a.ID != nil {
			aorByID[*a.ID] = a
		}
	}

	for i := range events {
		e := &events[i]
		if e.AttachedObjectReferenceID == nil {
			continue
		}
		a, ok := aorByID[*e.AttachedObjectReferenceID]
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
		names, err := LookupObjectNames(db, typ, ids)
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
