package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	echo "github.com/labstack/echo/v4"
	zap "go.uber.org/zap"
	"gorm.io/gorm"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// materializedViewThresholdFloor is the minimum total-count above which
// the event listing spins up a materialized view for cursor pagination.
// Below the floor (or below limit*10 when the caller asks for larger
// pages), return the whole result set in a single query and skip the
// CREATE MATERIALIZED VIEW / DROP MATERIALIZED VIEW round trip.
const materializedViewThresholdFloor = 5000

// eventAggregationWindow is the rolling window used to collapse
// repeat raw event rows into one bucket. Two rows sharing the dedup
// key belong to the same bucket when the next row's EventTime is
// within this window of the bucket's LastObservedTime; the window
// slides forward on every emit (kubernetes-style semantics).
const eventAggregationWindow = 10 * time.Minute

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

	// apply the subject filter only when ids were supplied. The
	// filter is the Cartesian product (object_type IN types AND
	// object_id IN ids) - intentional, so a multi-type bare kind
	// surfaces every (resolved type, id) pair instead of forcing
	// namespace disambiguation up front.
	applyObjectIdFilter := func(query *gorm.DB) *gorm.DB {
		if len(ids) == 0 {
			return query
		}
		return query.
			Where("v0_attached_object_references.object_type IN ?", fullyQualifiedTypes).
			Where("v0_attached_object_references.object_id IN ?", ids)
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
			// through the materialized view path for stable pagination
			*records = (*records)[:0]
			// large result set: materialize the join so subsequent
			// cursor-based page requests can scan a stable view
			// instead of re-running the join each time
			viewName, queryId := GenerateMaterializedViewName()

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

			// build and execute the CREATE MATERIALIZED VIEW. Raw SQL,
			// so substitute the literal type into the shared join
			// clause instead of binding via a gorm placeholder.
			joinClause := strings.Replace(
				eventJoinAttachedObjectReferenceClause,
				"?",
				fmt.Sprintf("'%s'", fullyQualifiedEventType),
				1,
			)
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

		// drop the materialized view inline the moment the client walks
		// off the end of the result set so the backing storage is freed
		// immediately, not deferred to the TTL sweeper. Failures here are
		// logged, not returned: the response body is already correct, and
		// the TTL sweeper still drops the view on its next pass.
		if !pagination.HasMore {
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
	// collapse raw event rows into aggregated buckets by (Reason,
	// ObjectType, ObjectID, Note) with a rolling 10-minute window.
	// runs on the current page after the raw fetch and enrichment; a
	// bucket whose raw rows straddle a page boundary appears as two
	// adjacent buckets rather than one, which is accepted for now
	// because the raw-row cursor keeps pagination termination correct.
	aggregated := aggregateEvents(*records)

	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(struct {
		Meta   apiserver_lib.Meta
		Type   string
		Data   []v0.Event
		Status apiserver_lib.Status
	}{
		Meta:   apiserver_lib.Meta{Pagination: *pagination, ObjectCount: returnedCount},
		Type:   objectType,
		Data:   aggregated,
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

// aggregateEvents collapses raw event rows into bucketed events keyed
// on (Reason, ObjectType, ObjectID). Within one key group, rows are
// walked in EventTime order and start a new bucket whenever the next
// row's EventTime is more than eventAggregationWindow after the
// current bucket's LastObservedTime (rolling window). The returned
// slice is sorted oldest-first by bucket EventTime so the response
// reads in causal order. Each bucket's ID is copied from its first
// raw row so cursor-based pagination in the caller stays stable
// across the collapse. Rows in the same bucket may carry different
// Note strings; the bucket's Note is the seed row's Note (earliest
// EventTime, lowest ID for tiebreak).
func aggregateEvents(events []v0.Event) []v0.Event {
	if len(events) == 0 {
		return events
	}

	// sort the raw rows by (EventTime, ID) ascending; the group walk
	// below depends on a stable causal-order stream. copy first so
	// the caller's slice is not reordered as a side effect. ID
	// breaks intra-second ties in insertion order (CRDB's monotonic
	// unique_rowid approximates emit order); nil IDs sort after
	// non-nil so bad rows do not front the response.
	sorted := make([]v0.Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := timeOrZero(sorted[i].EventTime)
		tj := timeOrZero(sorted[j].EventTime)
		if ti.Before(tj) {
			return true
		}
		if ti.Equal(tj) {
			return eventIDLess(sorted[i].ID, sorted[j].ID)
		}
		return false
	})

	// bucketKey builds the map key from the dedup fields. nil ObjectType
	// or ObjectID render as an empty string in that slot so pathological
	// rows still bucket deterministically. the null byte separator keeps
	// the parts from bleeding into each other on values that happen to
	// embed a slash.
	bucketKey := func(e v0.Event) string {
		var reason, objectType string
		var objectID uint
		if e.Reason != nil {
			reason = *e.Reason
		}
		if e.ObjectType != nil {
			objectType = *e.ObjectType
		}
		if e.ObjectID != nil {
			objectID = *e.ObjectID
		}
		return fmt.Sprintf("%s\x00%s\x00%d", reason, objectType, objectID)
	}

	// grouped preserves per-key ordering; buckets fill in the order
	// rows are visited so the final sort by EventTime is stable when
	// buckets end at the same instant. each bucket is stored by
	// pointer so per-row mutation in the walk below stays in place.
	grouped := make(map[string][]*v0.Event)
	keyOrder := make([]string, 0)

	for _, e := range sorted {
		key := bucketKey(e)
		buckets, seen := grouped[key]
		if !seen {
			keyOrder = append(keyOrder, key)
		}
		eventTime := timeOrZero(e.EventTime)
		lastObserved := timeOrZero(e.LastObservedTime)

		// start a new bucket when nothing in this key has been seen
		// yet or when the current row falls outside the rolling
		// window of the most recent bucket for this key.
		if len(buckets) == 0 {
			grouped[key] = append(buckets, seedBucket(e))
			continue
		}
		last := buckets[len(buckets)-1]
		lastBucketEnd := timeOrZero(last.LastObservedTime)
		if eventTime.Sub(lastBucketEnd) > eventAggregationWindow {
			grouped[key] = append(buckets, seedBucket(e))
			continue
		}

		// extend the current bucket: add the incoming row's stored
		// count (default 1 when nil) to the bucket total and slide
		// the LastObservedTime forward to the later of the current
		// end and the incoming row's LastObservedTime.
		incoming := uint(1)
		if e.Count != nil {
			incoming = *e.Count
		}
		if last.Count != nil {
			last.Count = util.Ptr(*last.Count + incoming)
		} else {
			last.Count = util.Ptr(incoming)
		}
		if lastObserved.After(lastBucketEnd) {
			last.LastObservedTime = util.Ptr(lastObserved)
		}
	}

	// flatten grouped map into a slice, then sort by bucket EventTime
	// so the response reads oldest-first regardless of key iteration
	// order.
	out := make([]v0.Event, 0, len(sorted))
	for _, key := range keyOrder {
		for _, b := range grouped[key] {
			out = append(out, *b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti := timeOrZero(out[i].EventTime)
		tj := timeOrZero(out[j].EventTime)
		if ti.Before(tj) {
			return true
		}
		if ti.Equal(tj) {
			return eventIDLess(out[i].ID, out[j].ID)
		}
		return false
	})
	return out
}

// eventIDLess reports whether the left ID sorts before the right
// under the aggregator's tiebreak rule. Nil IDs sort after non-nil
// so malformed rows stay out of the front of the response.
func eventIDLess(a, b *uint) bool {
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return *a < *b
}

// seedBucket seeds the bucket from the first raw row, defaulting
// Count to 1 only when the row was written without one.
func seedBucket(seed v0.Event) *v0.Event {
	b := seed
	if b.Count == nil {
		b.Count = util.Ptr(uint(1))
	}
	return &b
}

// timeOrZero dereferences a nullable time or returns the zero value.
func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
