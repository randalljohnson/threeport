package cmd

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	strcase "github.com/iancoleman/strcase"
	cobra "github.com/spf13/cobra"

	apilib "github.com/threeport/threeport/pkg/api/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	cli "github.com/threeport/threeport/pkg/cli/v0"
	client_v0 "github.com/threeport/threeport/pkg/client/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

var (
	eventsFor        string
	eventsObjectKind string
	eventsApiGroup   string
	eventsName       string
	eventsReason     string
	eventsOutput     string
	eventsSort       string
	eventsLimit      int
	eventsTopLevel   bool
	eventsVerbose    bool
	eventsWide       bool
	eventsReverse    bool
	eventsSince      time.Duration
	eventsType       string
)

const (
	eventShortAlias = "ev"
)

// bootNoiseReasons lists Reason values considered low-signal boot-time
// noise. Rows carrying these Reasons are hidden by default and revealed
// with --verbose; Warning-type events are always shown regardless.
var bootNoiseReasons = map[string]bool{
	"HostKeyCaptured":                 true,
	"SSHReachable":                    true,
	"ProviderResourcesProvisioning":   true,
	"ProviderResourcesDeprovisioning": true,
	"ScriptSucceeded":                 true,
}

// topLevelObjectKinds lists the core API type names considered
// top-level for the --top-level filter. Sub-object types
// (GcpGceMachineRuntimeInstance, KubernetesWorkloadResourceInstance,
// AwsEksKubernetesRuntimeInstance, etc.) stay off the list.
//
// TODO: promote to an SDK-generated manifest driven by a per-type
// top_level: true field in sdk-config.yaml so modules (Router,
// RouterFleetInstance) can register their own top-level kinds via
// IsTopLevel() instead of extending this hardcoded set.
var topLevelObjectKinds = map[string]bool{
	"KubernetesRuntimeDefinition":  true,
	"KubernetesRuntimeInstance":    true,
	"KubernetesWorkloadDefinition": true,
	"KubernetesWorkloadInstance":   true,
	"HelmWorkloadDefinition":       true,
	"HelmWorkloadInstance":         true,
	"ControlPlaneDefinition":       true,
	"ControlPlaneInstance":         true,
	"GatewayDefinition":            true,
	"GatewayInstance":              true,
	"DomainNameDefinition":         true,
	"DomainNameInstance":           true,
	"MachineRuntimeDefinition":     true,
	"MachineRuntimeInstance":       true,
	"MachineWorkloadDefinition":    true,
	"MachineWorkloadInstance":      true,
	"ObservabilityStackDefinition": true,
	"ObservabilityStackInstance":   true,
	"SecretDefinition":             true,
	"SecretInstance":               true,
	"TerraformDefinition":          true,
	"TerraformInstance":            true,
}

// GetEventsCmd represents the command 'tptctl get events'
var GetEventsCmd = &cobra.Command{
	Aliases: []string{"event", eventShortAlias},
	Example: `  # get all events
  tptctl get events

  # filter to a specific object (broad: any namespace/version)
  tptctl get events --for helm-workload-instance/my-app

  # narrow to one version
  tptctl get events --for v0.helm-workload-instance/my-app

  # narrow to one api namespace + version
  tptctl get events --for threeport.io/v0.helm-workload-instance/my-app

  # filter by kind alone across every object of that kind
  tptctl get events --object-kind helm-workload-instance

  # filter by API group / namespace alone
  tptctl get events --api-group threeport.io

  # filter by object name alone
  tptctl get events --name my-app

  # combine narrow filters (kind + name, group + kind, etc.)
  tptctl get events --object-kind helm-workload-instance --name my-app

  # filter by Reason (case-sensitive CamelCase)
  tptctl get events --reason SuccessfulCreate

  # show only events on top-level object kinds
  tptctl get events --top-level

  # filter to a subject by --for shape
  tptctl get events --for router-machine-set/demo1-router-set

  # only events within the last 5 minutes
  tptctl get events --since=5m

  # only Warning-type events
  tptctl get events --type=Warning

  # widen the MESSAGE column to the terminal width
  tptctl get events --wide

  # newest events first (equivalent to --sort=newest)
  tptctl get events -r

  # include the boot-noise reasons hidden by default
  tptctl get events --verbose`,
	Long: `Get events from the system.

Use --for [<namespace>/][<version>.]<kind>/<name> to filter events to a specific object. <namespace> and <version> are optional; <kind> and <name> are required. The kind is the kebab-case form of the API type name; the name is the object's Name field. Both core and module types are supported.

Use --object-kind <kebab-kind> to filter events to a specific kind across every object of that kind. Mutually exclusive with --for; combinable with --api-group and --name.

Use --api-group <namespace> to filter events by API group / namespace alone (e.g. threeport.io). Mutually exclusive with --for; combinable with --object-kind and --name.

Use --name <name> to filter events by object name alone. Mutually exclusive with --for; combinable with --object-kind and --api-group.

Use --reason <reason> to filter events by Reason (case-sensitive CamelCase, e.g. SuccessfulCreate). Applied client-side after fetch.

Use --top-level to drop events on sub-object kinds (e.g. GcpGceMachineRuntimeInstance, KubernetesWorkloadResourceInstance) and keep only events on top-level user-facing kinds.

Use --sort to control row order: oldest (default) puts the oldest activity at the top so the causal sequence reads down; newest is reverse. -r / --reverse is equivalent to --sort=newest.

Use --limit N to cap the number of rows shown (after sort). The default of 0 means no cap.

Use --since=<duration> to filter events by recency (e.g. --since=10m). Zero disables the filter.

Use --type Normal|Warning to filter events by type. Empty disables the filter.

Use --wide to widen the MESSAGE column to the terminal width so long notes render inline.

Use --verbose to include the boot-noise reasons that are hidden by default: HostKeyCaptured, SSHReachable, ProviderResourcesProvisioning, ProviderResourcesDeprovisioning, ScriptSucceeded. Warning-type events are always shown regardless of --verbose.

Full event notes (including captured script stdout/stderr) can be viewed with -o yaml.`,
	PreRun: CommandPreRunFunc,
	Run: func(cmd *cobra.Command, args []string) {
		apiClient, _, apiEndpoint, requestedControlPlane := GetClientContext(cmd)

		// reject mutually exclusive filters up front. --for encodes every
		// narrow filter's information in one shape, so it cannot combine
		// with any of the narrow flags
		if eventsFor != "" {
			switch {
			case eventsObjectKind != "":
				cli.Error("", fmt.Errorf("--for and --object-kind are mutually exclusive"))
				os.Exit(1)
			case eventsApiGroup != "":
				cli.Error("", fmt.Errorf("--for and --api-group are mutually exclusive"))
				os.Exit(1)
			case eventsName != "":
				cli.Error("", fmt.Errorf("--for and --name are mutually exclusive"))
				os.Exit(1)
			}
		}

		// build query string from the requested filter
		queryString, err := buildEventsQueryString(eventsFor, eventsObjectKind, eventsApiGroup, eventsName)
		if err != nil {
			cli.Error("failed to build events query", err)
			os.Exit(1)
		}

		// fetch events; the client walks pagination internally, then the
		// caller-supplied limit caps the returned slice
		events, err := client_v0.GetEventsJoinAttachedObjectReferenceByQueryString(apiClient, apiEndpoint, queryString, eventsLimit)
		if err != nil {
			cli.Error("failed to retrieve events", err)
			os.Exit(1)
		}

		// drop events on sub-object kinds when --top-level is set
		if eventsTopLevel {
			filtered := make([]v0.Event, 0, len(*events))
			for _, e := range *events {
				if isTopLevelEvent(&e) {
					filtered = append(filtered, e)
				}
			}
			events = &filtered
		}

		// hide boot-noise Reasons by default; --verbose reveals them.
		// Warning-type events are always shown; matches pkg/event/v0.TypeWarning.
		// hiddenBootNoise counts how many rows were suppressed so the
		// footer hint can offer --verbose as the escape hatch.
		hiddenBootNoise := 0
		if !eventsVerbose {
			filtered := make([]v0.Event, 0, len(*events))
			for _, e := range *events {
				if util.DerefString(e.Type) != "Warning" && bootNoiseReasons[util.DerefString(e.Reason)] {
					hiddenBootNoise++
					continue
				}
				filtered = append(filtered, e)
			}
			events = &filtered
		}

		// drop events older than --since ago when the flag is set
		if eventsSince > 0 {
			cutoff := time.Now().Add(-eventsSince)
			filtered := make([]v0.Event, 0, len(*events))
			for _, e := range *events {
				if e.EventTime != nil && e.EventTime.After(cutoff) {
					filtered = append(filtered, e)
				}
			}
			events = &filtered
		}

		// validate --type up front, then drop non-matching rows when set
		switch eventsType {
		case "", "Normal", "Warning":
		default:
			cli.Error("", fmt.Errorf("unrecognized type: %s (expected Normal or Warning)", eventsType))
			os.Exit(1)
		}
		if eventsType != "" {
			filtered := make([]v0.Event, 0, len(*events))
			for _, e := range *events {
				if util.DerefString(e.Type) == eventsType {
					filtered = append(filtered, e)
				}
			}
			events = &filtered
		}

		// drop rows whose Reason does not match --reason. Case-sensitive
		// CamelCase match by design; server-side reason index is a followup.
		if eventsReason != "" {
			filtered := make([]v0.Event, 0, len(*events))
			for _, e := range *events {
				if util.DerefString(e.Reason) == eventsReason {
					filtered = append(filtered, e)
				}
			}
			events = &filtered
		}

		if len(*events) == 0 {
			// still surface the boot-noise escape hatch when everything
			// was filtered away so a user who over-narrowed can back off
			if !eventsVerbose && hiddenBootNoise > 0 {
				fmt.Printf("%d event(s) hidden by boot-noise filter; use --verbose to include\n", hiddenBootNoise)
			}
			cli.Info(fmt.Sprintf(
				"no events found that are currently managed by %s threeport control plane",
				requestedControlPlane,
			))
			os.Exit(0)
		}

		// -r / --reverse folds into --sort=newest. When both are set
		// explicitly and they conflict, reject rather than silently pick
		// one. Combining --reverse with the default sort just flips to
		// newest without complaint.
		if eventsReverse {
			if cmd.Flags().Changed("sort") && eventsSort == "oldest" {
				cli.Error("", fmt.Errorf("--reverse and --sort=oldest are mutually exclusive"))
				os.Exit(1)
			}
			eventsSort = "newest"
		}

		// sort by event time per --sort
		newestFirst := false
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

		// write output. For tabular the verbose hint prints BEFORE the
		// table so the terminal read order is verbose-hint, table,
		// truncation-hint (the last printed inside outputEventsTable
		// after the tabwriter flush). yaml and json outputs never mix
		// hints with the payload.
		switch eventsOutput {
		case "tabular":
			if !eventsVerbose && hiddenBootNoise > 0 {
				fmt.Printf("%d event(s) hidden by boot-noise filter; use --verbose to include\n", hiddenBootNoise)
			}
			if err := outputEventsTable(events, eventsWide); err != nil {
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
		"for", "", "Filter events by object, in the form [<namespace>/][<version>.]<kind>/<name>. Kind is the kebab-case form of the API type name (e.g. machine-runtime-instance, router-definition). Mutually exclusive with --object-kind.",
	)
	GetEventsCmd.Flags().StringVar(
		&eventsObjectKind,
		"object-kind", "", "Filter events by object kind alone (kebab-case form of the API type name, e.g. helm-workload-instance). Mutually exclusive with --for; combinable with --api-group and --name.",
	)
	GetEventsCmd.Flags().StringVar(
		&eventsApiGroup,
		"api-group", "", "Filter events by API group / namespace (e.g. threeport.io). Mutually exclusive with --for; combinable with --object-kind and --name.",
	)
	GetEventsCmd.Flags().StringVar(
		&eventsName,
		"name", "", "Filter events by object name alone. Mutually exclusive with --for; combinable with --object-kind and --api-group.",
	)
	GetEventsCmd.Flags().StringVar(
		&eventsReason,
		"reason", "", "Filter events by Reason (case-sensitive CamelCase, e.g. SuccessfulCreate).",
	)
	GetEventsCmd.Flags().BoolVar(
		&eventsTopLevel,
		"top-level", false, "Only show events on top-level object kinds (drops sub-object kinds like GcpGceMachineRuntimeInstance and KubernetesWorkloadResourceInstance).",
	)
	GetEventsCmd.Flags().StringVarP(
		&eventsOutput,
		"output", "o", "tabular", "Output format for events. One of: [tabular, yaml, json]",
	)
	GetEventsCmd.Flags().StringVar(
		&eventsSort,
		"sort", "oldest", "Sort order. One of: [oldest, newest]. Default oldest places the earliest event at the top so the causal sequence reads down.",
	)
	GetEventsCmd.Flags().IntVar(
		&eventsLimit,
		"limit", 0, "Maximum number of events to display after sort. 0 means no cap.",
	)
	GetEventsCmd.Flags().BoolVar(
		&eventsVerbose,
		"verbose", false, "Show low-value boot-noise events (HostKeyCaptured, SSHReachable, ProviderResourcesProvisioning, ProviderResourcesDeprovisioning, ScriptSucceeded) that are hidden by default.",
	)
	GetEventsCmd.Flags().DurationVar(
		&eventsSince,
		"since", 0, "Only show events with EventTime newer than the given duration ago (e.g. --since=10m, --since=1h). Zero means no time filter.",
	)
	GetEventsCmd.Flags().StringVar(
		&eventsType,
		"type", "", "Filter events by Type. One of: [Normal, Warning]. Empty means no filter.",
	)
	GetEventsCmd.Flags().BoolVar(
		&eventsWide,
		"wide", false, "Widen MESSAGE column to the terminal width.",
	)
	GetEventsCmd.Flags().BoolVarP(
		&eventsReverse,
		"reverse", "r", false, "Reverse sort order. Equivalent to --sort=newest.",
	)
	GetEventsCmd.Flags().StringVarP(
		&cliArgs.ControlPlaneName,
		"control-plane-name", "i", "", "Optional. Name of control plane. Will default to current control plane if not provided.",
	)
}

// buildEventsQueryString turns the --for / --object-kind / --api-group /
// --name flags into the events query string. Callers must ensure --for is
// not combined with any of the narrow flags (the caller guards this
// mutex before invoking).
//
// --for accepts three input shapes, narrowing the query as more parts are
// supplied:
//
//	<kebab-kind>/<name>                                 - broad, any namespace/version
//	<version>.<kebab-kind>/<name>                       - narrow to one version
//	<namespace>/<version>.<kebab-kind>/<name>           - exact fully qualified type match
//
// The kind segment carries the optional version inline as
// "<version>.<kind>", mirroring the fully qualified type form.
//
// --object-kind, --api-group, and --name each set exactly one query key.
// They combine freely so a caller can narrow by any subset (kind + name,
// group + kind, group + name, or all three).
//
// Empty flags return an empty string so the caller queries every event.
func buildEventsQueryString(forFlag, objectKindFlag, apiGroupFlag, nameFlag string) (string, error) {
	// no filter requested - return empty so the caller queries every event
	if forFlag == "" && objectKindFlag == "" && apiGroupFlag == "" && nameFlag == "" {
		return "", nil
	}

	q := url.Values{}

	// narrow flags: each maps to one query key. Any subset may be set;
	// each additional key AND-narrows the server-side match.
	if forFlag == "" {
		if objectKindFlag != "" {
			q.Set("objecttypename", strcase.ToCamel(objectKindFlag))
		}
		if apiGroupFlag != "" {
			q.Set("objectnamespace", apiGroupFlag)
		}
		if nameFlag != "" {
			q.Set("objectname", nameFlag)
		}
		return q.Encode(), nil
	}

	// --for: split slash-delimited segments. parse right-to-left so the
	// optional namespace lands in the right slot
	parts := strings.Split(forFlag, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return "", fmt.Errorf(
			"invalid --for value %q: expected [<namespace>/][<version>.]<kind>/<name>",
			forFlag,
		)
	}

	// last segment is always the object name
	name := parts[len(parts)-1]
	if name == "" {
		return "", fmt.Errorf("invalid --for value %q: empty name", forFlag)
	}
	q.Set("objectname", name)

	// second-to-last is the kind, optionally prefixed by "<version>."
	// e.g. "kubernetes-workload-instance" or "v0.kubernetes-workload-instance"
	kindPart := parts[len(parts)-2]
	if kindPart == "" {
		return "", fmt.Errorf("invalid --for value %q: empty kind", forFlag)
	}
	kind := kindPart
	if dotIdx := strings.Index(kindPart, "."); dotIdx >= 0 {
		// split version off the front
		version := kindPart[:dotIdx]
		kind = kindPart[dotIdx+1:]
		if version == "" || kind == "" {
			return "", fmt.Errorf("invalid --for value %q: empty version or kind around dot", forFlag)
		}
		q.Set("objectversion", version)
	}

	// kebab-case kind -> CamelCase TypeName segment of fully qualified type
	// ("kubernetes-workload-instance" -> "KubernetesWorkloadInstance")
	q.Set("objecttypename", strcase.ToCamel(kind))

	// third-to-last (if present) is the api namespace
	if len(parts) == 3 {
		namespace := parts[0]
		if namespace == "" {
			return "", fmt.Errorf("invalid --for value %q: empty namespace", forFlag)
		}
		q.Set("objectnamespace", namespace)
	}

	return q.Encode(), nil
}

// isTopLevelEvent reports whether the event's ObjectType is a top-level
// object kind per topLevelObjectKinds. Events missing or malformed
// ObjectType are treated as non-top-level.
func isTopLevelEvent(e *v0.Event) bool {
	rawType := util.DerefString(e.ObjectType)
	if rawType == "" {
		return false
	}
	_, _, typeName, ok := apilib.ParseQualifiedType(rawType)
	if !ok {
		return false
	}
	return topLevelObjectKinds[typeName]
}
