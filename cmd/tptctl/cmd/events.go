package cmd

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

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

// GetEventsCmd represents the command 'tptctl get events'.
//
// Mirrors `kubectl events` conventions: a flat `events` resource filterable
// by `--for <kind>/<name>`. Full event details (including the complete
// script stdout/stderr captured in Note) are available via `-o yaml`.
var GetEventsCmd = &cobra.Command{
	Aliases: []string{"event"},
	Example: "  # get all events\n  tptctl get events\n\n  # get events for a specific machine workload instance\n  tptctl get events --for machine-workload-instance/my-workload\n\n  # get events for a specific machine runtime instance\n  tptctl get events --for machine-runtime-instance/my-runtime",
	Long:    "Get events from the system.\n\nUse --for <kind>/<name> to filter events to a specific object. Currently supported kinds: machine-workload-instance, machine-runtime-instance.\n\nFull event notes (including captured script stdout/stderr) can be viewed with -o yaml.",
	PreRun:  CommandPreRunFunc,
	Run: func(cmd *cobra.Command, args []string) {
		apiClient, _, apiEndpoint, requestedControlPlane := GetClientContext(cmd)

		// build query string from --for
		queryString, err := buildEventsQueryString(apiClient, apiEndpoint, eventsFor)
		if err != nil {
			cli.Error("failed to build events query", err)
			os.Exit(1)
		}

		// fetch events joined to attached object references
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

		// sort oldest → newest
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
			if err := outputGetv0EventsCmd(apiClient, apiEndpoint, events); err != nil {
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
		"for", "", "Filter events by object, in the form <kind>/<name>. Supported kinds: machine-workload-instance, machine-runtime-instance.",
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

// buildEventsQueryString resolves the --for flag into a query string suitable
// for client_v0.GetEventsJoinAttachedObjectReferenceByQueryString. An empty
// --for returns an empty query (all events). The query filters via the joined
// AttachedObjectReference's ObjectID column.
func buildEventsQueryString(apiClient *http.Client, apiEndpoint string, forFlag string) (string, error) {
	if forFlag == "" {
		return "", nil
	}

	kind, name, ok := strings.Cut(forFlag, "/")
	if !ok || kind == "" || name == "" {
		return "", fmt.Errorf("invalid --for value %q: expected <kind>/<name>", forFlag)
	}

	switch kind {
	case "machine-workload-instance":
		mwi, err := client_v0.GetMachineWorkloadInstanceByName(apiClient, apiEndpoint, name)
		if err != nil {
			return "", fmt.Errorf("failed to find machine workload instance %q: %w", name, err)
		}
		return fmt.Sprintf("objectid=%d", *mwi.ID), nil
	case "machine-runtime-instance":
		mri, err := client_v0.GetMachineRuntimeInstanceByName(apiClient, apiEndpoint, name)
		if err != nil {
			return "", fmt.Errorf("failed to find machine runtime instance %q: %w", name, err)
		}
		return fmt.Sprintf("objectid=%d", *mri.ID), nil
	default:
		return "", fmt.Errorf("unsupported kind %q in --for. Supported kinds: machine-workload-instance, machine-runtime-instance", kind)
	}
}

// outputGetv0EventsCmd produces the tabular output for the
// 'get events' command. NOTE is truncated to eventMessageTableMax chars
// with newlines collapsed so the column stays one-line per event; use
// -o yaml to see the full note.
func outputGetv0EventsCmd(
	apiClient *http.Client,
	apiEndpoint string,
	events *[]v0.Event,
) error {
	writer := tabwriter.NewWriter(os.Stdout, 4, 4, 4, ' ', 0)
	fmt.Fprintln(writer, "AGE\t TYPE\t REASON\t OBJECT\t NOTE")

	// cache per-event object resolutions so we don't hit the API once per row
	objectCache := map[uint]string{}

	anyTruncated := false
	for _, e := range *events {
		age := util.GetAgeFormatted(e.EventTime)
		eventType := util.DerefString(e.Type)
		reason := util.DerefString(e.Reason)

		object := resolveEventObject(apiClient, apiEndpoint, &e, objectCache)

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

// resolveEventObject formats the owning object of an event as `<kind>/<name>`.
// It looks up the associated AttachedObjectReference to determine the object
// type and id, then resolves a friendly name for supported kinds. Resolutions
// are cached in the caller-supplied map keyed by event id to avoid repeat
// API calls within a single output run.
func resolveEventObject(
	apiClient *http.Client,
	apiEndpoint string,
	e *v0.Event,
	cache map[uint]string,
) string {
	if e.ID == nil {
		return ""
	}
	if cached, ok := cache[*e.ID]; ok {
		return cached
	}

	// look up the attached object reference for this event
	aors, err := client_v0.GetAttachedObjectReferencesByQueryString(
		apiClient,
		apiEndpoint,
		fmt.Sprintf("attachedobjectid=%d", *e.ID),
	)
	if err != nil || aors == nil || len(*aors) == 0 {
		cache[*e.ID] = ""
		return ""
	}
	aor := (*aors)[0]
	if aor.ObjectType == nil || aor.ObjectID == nil {
		cache[*e.ID] = ""
		return ""
	}

	// strip version prefix like "v0." from stored ObjectType
	rawType := *aor.ObjectType
	typeSuffix := rawType
	if idx := strings.LastIndex(rawType, "."); idx >= 0 {
		typeSuffix = rawType[idx+1:]
	}

	var result string
	switch typeSuffix {
	case "MachineWorkloadInstance":
		mwi, err := client_v0.GetMachineWorkloadInstanceByID(apiClient, apiEndpoint, *aor.ObjectID)
		if err == nil && mwi.Name != nil {
			result = fmt.Sprintf("machine-workload-instance/%s", *mwi.Name)
		} else {
			result = fmt.Sprintf("machine-workload-instance/<id=%d>", *aor.ObjectID)
		}
	case "MachineRuntimeInstance":
		mri, err := client_v0.GetMachineRuntimeInstanceByID(apiClient, apiEndpoint, *aor.ObjectID)
		if err == nil && mri.Name != nil {
			result = fmt.Sprintf("machine-runtime-instance/%s", *mri.Name)
		} else {
			result = fmt.Sprintf("machine-runtime-instance/<id=%d>", *aor.ObjectID)
		}
	default:
		result = fmt.Sprintf("%s/<id=%d>", rawType, *aor.ObjectID)
	}

	cache[*e.ID] = result
	return result
}
