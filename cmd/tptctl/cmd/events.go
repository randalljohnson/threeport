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
)

// GetEventsCmd represents the command 'tptctl get events'
var GetEventsCmd = &cobra.Command{
	Aliases: []string{"event"},
	Example: "  # get all events\n  tptctl get events\n\n  # filter to a specific object\n  tptctl get events --for machine-runtime-instance/oci-test-host\n  tptctl get events --for router-instance/sxalable-router-demo-1",
	Long:    "Get events from the system.\n\nUse --for <kind>/<name> to filter events to a specific object. The kind is the kebab-case form of the API type name; the name is the object's Name field. Both core and module types are supported.\n\nFull event notes (including captured script stdout/stderr) can be viewed with -o yaml.",
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

		// sort oldest first
		sort.SliceStable(*events, func(i, j int) bool {
			ti, tj := (*events)[i].EventTime, (*events)[j].EventTime
			if ti == nil || tj == nil {
				return ti != nil
			}
			return ti.Before(*tj)
		})

		// write output
		switch eventsOutput {
		case "tabular":
			if err := outputGetv0EventsCmd(events); err != nil {
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
		"output", "o", "tabular", "Output format for events. One of: [tabular, yaml, json]",
	)
	GetEventsCmd.Flags().StringVarP(
		&cliArgs.ControlPlaneName,
		"control-plane-name", "i", "", "Optional. Name of control plane. Will default to current control plane if not provided.",
	)
}

// buildEventsQueryString turns the --for flag into the events query string.
func buildEventsQueryString(forFlag string) (string, error) {
	if forFlag == "" {
		return "", nil
	}

	kind, name, ok := strings.Cut(forFlag, "/")
	if !ok || kind == "" || name == "" {
		return "", fmt.Errorf("invalid --for value %q: expected <kind>/<name>", forFlag)
	}

	objectType := fmt.Sprintf("v0.%s", strcase.ToCamel(kind))
	q := url.Values{}
	q.Set("objecttype", objectType)
	q.Set("objectname", name)
	return q.Encode(), nil
}

// outputGetv0EventsCmd produces the tabular output for the
// 'get events' command.
func outputGetv0EventsCmd(events *[]v0.Event) error {
	writer := tabwriter.NewWriter(os.Stdout, 4, 4, 4, ' ', 0)
	fmt.Fprintln(writer, "AGE\t TYPE\t REASON\t OBJECT\t NOTE")

	anyTruncated := false
	for _, e := range *events {
		age := util.GetAgeFormatted(e.EventTime)
		eventType := util.DerefString(e.Type)
		reason := util.DerefString(e.Reason)

		object := formatEventObject(&e)

		rawNote := util.DerefString(e.Note)
		note := strings.Join(strings.Fields(rawNote), " ")
		if len(note) > eventMessageTableMax {
			note = util.TruncateString(note, eventMessageTableMax)
			anyTruncated = true
		}

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

	if anyTruncated {
		fmt.Println("(use -o yaml to see full note)")
	}
	return nil
}

// formatEventObject builds the OBJECT column as <kebab-kind>/<name>.
func formatEventObject(e *v0.Event) string {
	rawType := util.DerefString(e.ObjectType)
	if rawType == "" {
		return ""
	}

	// strip the optional namespace prefix and isolate just the type name
	versionedName := rawType
	if slashIdx := strings.Index(rawType, "/"); slashIdx >= 0 {
		versionedName = rawType[slashIdx+1:]
	}
	typeName := versionedName
	if dotIdx := strings.LastIndex(versionedName, "."); dotIdx >= 0 {
		typeName = versionedName[dotIdx+1:]
	}
	kind := strcase.ToKebab(typeName)

	if name := util.DerefString(e.ObjectName); name != "" {
		return fmt.Sprintf("%s/%s", kind, name)
	}
	if e.ObjectID != nil {
		return fmt.Sprintf("%s/%d", kind, *e.ObjectID)
	}
	return kind
}
