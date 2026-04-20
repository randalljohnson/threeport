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
// script stdout/stderr captured in Message) are available via `-o yaml`.
var GetEventsCmd = &cobra.Command{
	Aliases: []string{"event"},
	Example: "  # get all workload events\n  tptctl get events\n\n  # get events for a specific machine workload instance\n  tptctl get events --for machine-workload-instance/my-workload",
	Long:    "Get workload events from the system.\n\nUse --for <kind>/<name> to filter events to a specific object. Currently supported kinds: machine-workload-instance.\n\nFull event messages (including captured script stdout/stderr) can be viewed with -o yaml.",
	PreRun:  CommandPreRunFunc,
	Run: func(cmd *cobra.Command, args []string) {
		apiClient, _, apiEndpoint, requestedControlPlane := GetClientContext(cmd)

		// build query string from --for
		queryString, err := buildEventsQueryString(apiClient, apiEndpoint, eventsFor)
		if err != nil {
			cli.Error("failed to build events query", err)
			os.Exit(1)
		}

		// fetch events
		events, err := client_v0.GetWorkloadEventsByQueryString(apiClient, apiEndpoint, queryString)
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
			ti, tj := (*events)[i].Timestamp, (*events)[j].Timestamp
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
	Short:        "Get workload events from the system",
	SilenceUsage: true,
	Use:          "events",
}

func init() {
	GetCmd.AddCommand(GetEventsCmd)

	GetEventsCmd.Flags().StringVar(
		&eventsFor,
		"for", "", "Filter events by object, in the form <kind>/<name>. Supported kinds: machine-workload-instance.",
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
// for client_v0.GetWorkloadEventsByQueryString. An empty --for returns an
// empty query (all events).
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
		return fmt.Sprintf("machineworkloadinstanceid=%d", *mwi.ID), nil
	default:
		return "", fmt.Errorf("unsupported kind %q in --for. Supported kinds: machine-workload-instance", kind)
	}
}

// outputGetv0EventsCmd produces the tabular output for the
// 'get events' command. MESSAGE is truncated to eventMessageTableMax chars
// with newlines collapsed so the column stays one-line per event; use
// -o yaml to see the full message.
func outputGetv0EventsCmd(
	apiClient *http.Client,
	apiEndpoint string,
	events *[]v0.WorkloadEvent,
) error {
	writer := tabwriter.NewWriter(os.Stdout, 4, 4, 4, ' ', 0)
	fmt.Fprintln(writer, "AGE\t TYPE\t REASON\t OBJECT\t MESSAGE")

	// cache per-kind-id lookups so we don't hit the API once per row
	mwiNames := map[uint]string{}

	anyTruncated := false
	for _, event := range *events {
		age := util.GetAgeFormatted(event.Timestamp)
		eventType := util.DerefString(event.Type)
		reason := util.DerefString(event.Reason)

		object := resolveEventObject(apiClient, apiEndpoint, &event, mwiNames)

		rawMessage := util.DerefString(event.Message)
		message := strings.Join(strings.Fields(rawMessage), " ")
		if len(message) > eventMessageTableMax {
			message = util.TruncateString(message, eventMessageTableMax)
			anyTruncated = true
		}

		fmt.Fprintln(
			writer,
			age, "\t",
			eventType, "\t",
			reason, "\t",
			object, "\t",
			message,
		)
	}
	writer.Flush()

	if anyTruncated {
		fmt.Println("(use -o yaml to see full message)")
	}
	return nil
}

// resolveEventObject formats the owning object of an event as `<kind>/<name>`.
// Names are resolved via per-kind client lookups and cached in the caller-
// supplied map to avoid repeat API calls within a single output run.
func resolveEventObject(
	apiClient *http.Client,
	apiEndpoint string,
	event *v0.WorkloadEvent,
	mwiNames map[uint]string,
) string {
	switch {
	case event.MachineWorkloadInstanceID != nil:
		id := *event.MachineWorkloadInstanceID
		name, ok := mwiNames[id]
		if !ok {
			mwi, err := client_v0.GetMachineWorkloadInstanceByID(apiClient, apiEndpoint, id)
			if err == nil && mwi.Name != nil {
				name = *mwi.Name
			}
			mwiNames[id] = name
		}
		if name == "" {
			return fmt.Sprintf("machine-workload-instance/<id=%d>", id)
		}
		return fmt.Sprintf("machine-workload-instance/%s", name)
	case event.WorkloadInstanceID != nil:
		return fmt.Sprintf("workload-instance/<id=%d>", *event.WorkloadInstanceID)
	case event.HelmWorkloadInstanceID != nil:
		return fmt.Sprintf("helm-workload-instance/<id=%d>", *event.HelmWorkloadInstanceID)
	case event.WorkloadResourceInstanceID != nil:
		return fmt.Sprintf("workload-resource-instance/<id=%d>", *event.WorkloadResourceInstanceID)
	default:
		return ""
	}
}
