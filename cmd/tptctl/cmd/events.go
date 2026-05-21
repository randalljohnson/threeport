package cmd

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	strcase "github.com/iancoleman/strcase"
	cobra "github.com/spf13/cobra"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	cli "github.com/threeport/threeport/pkg/cli/v0"
	client_v0 "github.com/threeport/threeport/pkg/client/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

const eventMessageTableMax = 80

var (
	eventsFor    string
	eventsOutput string
	eventsSort   string
	eventsLimit  int
)

// GetEventsCmd represents the command 'tptctl get events'
var GetEventsCmd = &cobra.Command{
	Aliases: []string{"event"},
	Example: `  # get all events
  tptctl get events

  # filter to a specific object
  tptctl get events --for workload-instance/some-workload
  tptctl get events --for machine-runtime-instance/some-host`,
	Long: `Get events from the system.

Use --for <kind>/<name> to filter events to a specific object. The kind is the kebab-case form of the API type name; the name is the object's Name field. Both core and module types are supported.

Use --sort to control row order: newest (default) puts the most recent activity at the top; oldest is reverse.

Use --limit N to cap the number of rows shown (after sort). 0 means no cap.

Full event notes (including captured script stdout/stderr) can be viewed with -o yaml.`,
	PreRun:  CommandPreRunFunc,
	Run: func(cmd *cobra.Command, args []string) {
		apiClient, _, apiEndpoint, requestedControlPlane := GetClientContext(cmd)

		// build query string from --for
		queryString, err := buildEventsQueryString(eventsFor)
		if err != nil {
			cli.Error("failed to build events query", err)
			os.Exit(1)
		}

		// fetch events
		events, err := client_v0.GetEventsJoinAttachedObjectReferenceByQueryString(apiClient, apiEndpoint, queryString)
		if err != nil {
			cli.Error("failed to retrieve events", err)
			os.Exit(1)
		}

		if len(*events) == 0 {
			cli.Info(fmt.Sprintf(
				"no events found that are currently managed by %s threeport control plane",
				requestedControlPlane,
			))
			os.Exit(0)
		}

		// sort by event time per --sort
		newestFirst := true
		switch eventsSort {
		case "newest":
			newestFirst = true
		case "oldest":
			newestFirst = false
		default:
			cli.Error("", fmt.Errorf("unrecognized sort: %s (expected newest or oldest)", eventsSort))
			os.Exit(1)
		}
		sort.SliceStable(*events, func(i, j int) bool {
			ti, tj := (*events)[i].EventTime, (*events)[j].EventTime
			if ti == nil || tj == nil {
				return ti != nil
			}
			if newestFirst {
				return ti.After(*tj)
			}
			return ti.Before(*tj)
		})

		// cap to --limit after sort so the cap respects user's chosen direction
		if eventsLimit > 0 && len(*events) > eventsLimit {
			truncated := (*events)[:eventsLimit]
			events = &truncated
		}

		// write output
		switch eventsOutput {
		case "tabular":
			if err := outputEventsTable(events); err != nil {
				cli.Error("failed to produce output", err)
				os.Exit(1)
			}
		case "yaml":
			if err := cli.YamlObjectOutput(*events); err != nil {
				cli.Error("failed to produce YAML output", err)
				os.Exit(1)
			}
		case "json":
			if err := cli.JsonObjectOutput(*events); err != nil {
				cli.Error("failed to produce JSON output", err)
				os.Exit(1)
			}
		default:
			cli.Error("", fmt.Errorf("unrecognized output format: %s", eventsOutput))
			os.Exit(1)
		}
	},
	Short:        "Get events from the system",
	SilenceUsage: true,
	Use:          "events",
}

func init() {
	GetCmd.AddCommand(GetEventsCmd)

	GetEventsCmd.Flags().StringVar(
		&eventsFor,
		"for", "", "Filter events by object, in the form <kind>/<name>. Kind is the kebab-case form of the API type name (e.g. machine-runtime-instance, router-definition).",
	)
	GetEventsCmd.Flags().StringVarP(
		&eventsOutput,
		"output", "o", "tabular", "Output format for events. One of: [yaml, json]",
	)
	GetEventsCmd.Flags().StringVar(
		&eventsSort,
		"sort", "newest", "Sort order. One of: [newest, oldest]",
	)
	GetEventsCmd.Flags().IntVar(
		&eventsLimit,
		"limit", 0, "Maximum number of events to display after sort. 0 means no cap.",
	)
	GetEventsCmd.Flags().StringVarP(
		&cliArgs.ControlPlaneName,
		"control-plane-name", "i", "", "Optional. Name of control plane. Will default to current control plane if not provided.",
	)
}

// buildEventsQueryString turns the --for flag into the events query
// string. Accepts three input shapes, narrowing the query as more
// parts are supplied:
//
//   <kebab-kind>/<name>                                 - broad, any namespace/version
//   <version>/<kebab-kind>/<name>                       - narrow to one version
//   <namespace>/<version>/<kebab-kind>/<name>           - exact FQTN match
//
// The flag is split right-to-left so the last segment is always
// the object name, the second-to-last is always the kebab kind, etc.
// Empty flag returns empty string so the events list isn't filtered.
func buildEventsQueryString(forFlag string) (string, error) {
	// no filter requested - return empty so the caller queries every event
	if forFlag == "" {
		return "", nil
	}

	// segments are slash-delimited; parse right-to-left so the
	// optional namespace and version land in the right slots
	parts := strings.Split(forFlag, "/")
	if len(parts) < 2 || len(parts) > 4 {
		return "", fmt.Errorf(
			"invalid --for value %q: expected [<namespace>/][<version>/]<kind>/<name>",
			forFlag,
		)
	}

	q := url.Values{}

	// last segment is always the object name
	name := parts[len(parts)-1]
	if name == "" {
		return "", fmt.Errorf("invalid --for value %q: empty name", forFlag)
	}
	q.Set("objectname", name)

	// second-to-last is always the kebab kind; convert to CamelCase
	// to match the TypeName segment of FQTN ("workload-instance" ->
	// "WorkloadInstance")
	kind := parts[len(parts)-2]
	if kind == "" {
		return "", fmt.Errorf("invalid --for value %q: empty kind", forFlag)
	}
	q.Set("objecttypename", strcase.ToCamel(kind))

	// third-to-last (if present) is the version
	if len(parts) >= 3 {
		version := parts[len(parts)-3]
		if version == "" {
			return "", fmt.Errorf("invalid --for value %q: empty version", forFlag)
		}
		q.Set("objectversion", version)
	}

	// fourth-to-last (if present) is the api namespace
	if len(parts) == 4 {
		namespace := parts[0]
		if namespace == "" {
			return "", fmt.Errorf("invalid --for value %q: empty namespace", forFlag)
		}
		q.Set("objectnamespace", namespace)
	}

	return q.Encode(), nil
}

// outputEventsTable produces the tabular output for the events list.
func outputEventsTable(events *[]v0.Event) error {
	// configure a tabwriter so the columns align regardless of the
	// width of any individual cell's content
	writer := tabwriter.NewWriter(os.Stdout, 4, 4, 4, ' ', 0)
	fmt.Fprintln(writer, "AGE\t TYPE\t REASON\t OBJECT\t NOTE")

	// track whether any note got truncated so we can hint about -o yaml
	// at the bottom of the output
	anyTruncated := false
	for _, e := range *events {
		// derive the human-readable cell values per event
		age := util.GetAgeFormatted(e.EventTime)
		eventType := util.DerefString(e.Type)
		reason := util.DerefString(e.Reason)

		// resolve the OBJECT column from the AOR-projected fields
		// on the event row (see Event.ObjectType/ID/Name)
		object := formatEventObject(&e)

		// collapse whitespace runs in the note so multi-line script
		// output renders on one row
		rawNote := util.DerefString(e.Note)
		note := strings.Join(strings.Fields(rawNote), " ")

		// truncate over-long notes so a single noisy event doesn't
		// wreck the table layout
		if len(note) > eventMessageTableMax {
			note = util.TruncateString(note, eventMessageTableMax)
			anyTruncated = true
		}

		// emit one tab-separated row through the writer; column
		// alignment is finalized at Flush() below
		fmt.Fprintln(
			writer,
			age, "\t",
			eventType, "\t",
			reason, "\t",
			object, "\t",
			note,
		)
	}
	writer.Flush()

	// nudge the reader toward -o yaml when at least one note was
	// shortened so they can see the full content
	if anyTruncated {
		fmt.Println("(use -o yaml to see full note)")
	}
	return nil
}

// formatEventObject formats an event's target object as <kebab-kind>/<name>.
// For an event with ObjectType="example.com/v0.RouterInstance",
// ObjectID=42, ObjectName="some-router" the result is
// "router-instance/some-router". Falls back to "<kind>/<id>" if the
// name wasn't resolved (e.g. lookup failed), or just "<kind>" if the
// id is nil too.
func formatEventObject(e *v0.Event) string {
	// no recorded subject - nothing to render
	rawType := util.DerefString(e.ObjectType)
	if rawType == "" {
		return ""
	}

	// FQTN is always "<namespace>/<version>.<TypeName>" now.
	// "example.com/v0.RouterInstance" -> typeName = "RouterInstance"
	dotIdx := strings.LastIndex(rawType, ".")
	if dotIdx < 0 {
		// malformed; surface the raw value so the user can still grep
		return rawType
	}
	typeName := rawType[dotIdx+1:]

	// CamelCase -> kebab so the output matches the --for flag shape.
	// "RouterInstance" -> kind = "router-instance"
	kind := strcase.ToKebab(typeName)

	// prefer name when resolved; this is the common case after the
	// events-join handler enriches the row
	if name := util.DerefString(e.ObjectName); name != "" {
		return fmt.Sprintf("%s/%s", kind, name)
	}

	// name wasn't resolved (lookup failed, deleted subject, etc.);
	// fall back to id so the user still has something to grep
	if e.ObjectID != nil {
		return fmt.Sprintf("%s/%d", kind, *e.ObjectID)
	}
	return kind
}
