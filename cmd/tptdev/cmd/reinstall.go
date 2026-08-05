/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/threeport/threeport/internal/version"
	auth "github.com/threeport/threeport/pkg/auth/v0"
	cli "github.com/threeport/threeport/pkg/cli/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
	installer "github.com/threeport/threeport/pkg/threeport-installer/v0"
	"github.com/threeport/threeport/pkg/threeport-installer/v0/tptdev"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// reinstallApis holds the --apis flag value: a comma-separated list
// of sdk-config ApiObjectGroup names whose controllers should be
// reinstalled. When empty, the reinstall auto-detects the controller
// subset from the cluster's installer-managed deployments.
var reinstallApis string

// reinstallDropDatabase holds the --drop-database flag value: whether
// to drop the database schema before reapplying the install, so the
// migrations run from scratch.
var reinstallDropDatabase bool

// reinstallConfirm holds the --confirm flag value: the control plane
// name the caller typed back to authorize dropping the database.
var reinstallConfirm string

// reinstallRestoreBootstrap holds the --restore-bootstrap flag value:
// whether to recreate the threeport API records the control plane needs
// in order to accept work. Implied by --drop-database, which destroys
// them; on its own it covers a database emptied some other way.
var reinstallRestoreBootstrap bool

// reinstallCmd represents the reinstall command. Dev-only: sweeps
// installer-managed stateless deployments and reapplies the install
// path so devs pick up source changes without losing database state
// or rotating the control plane's external endpoint.
var reinstallCmd = &cobra.Command{
	Use:   "reinstall",
	Short: "Sweep and reapply stateless control plane resources",
	Long: `Reinstall the stateless side of a dev threeport control plane.

Sweeps every installer-managed Deployment in the control plane
namespace, then reapplies the install path so the pods come back with
the current images and specs from source.

Preserved across reinstall: cockroachdb data, nats data, the
certificate authority and signed certs, and the rest-api's external
service ip. Everything else (controller and api-server pods, their
configmaps, rbac) is recreated.

Pass --drop-database to reset the database as well, taking the whole
control plane from running to running with an empty schema in one
command: the control plane is scaled down, its schema is dropped by a
statement issued against the running database, the install reapplies,
and the migrations run from scratch. That data is not recoverable, so
the flag also requires --confirm with the control plane name, and the
target cluster must record itself as a development installation, which
a cloud-hosted control plane does by being installed with 'tptctl up
--tier development'.

The database's data volume, its certificates and the certificate
authority all survive a drop, so no volume is reprovisioned and no
credentials need to be re-issued or re-downloaded afterward. The
kubernetes runtime and control plane records the API needs in order to
accept work go out with the schema and are recreated from the local
threeport config once the API is back up.

Pass --restore-bootstrap to recreate those same records without
dropping anything. Use it when the database was emptied by something
other than this command, such as a database dropped by hand so that
renumbered migrations reapply from scratch. It creates only the records
that are missing, so running it against a control plane that still has
them changes nothing.

Intended for dev environments only. The reinstall command does not
build images; run 'tptdev build --push' first if the image needs to
change.`,
	Run: func(cmd *cobra.Command, args []string) {
		cliArgs.GetControlPlaneEnvVars()

		// require the control plane name typed back before dropping the
		// database. The installer refuses the drop outright on anything
		// but a development control plane; this catches the case where
		// the target legitimately is one, but not the one the caller had
		// in mind.
		if reinstallDropDatabase && reinstallConfirm != cliArgs.ControlPlaneName {
			cli.Error(fmt.Sprintf(
				"--drop-database destroys the database of control plane %q and its data cannot be recovered; re-run with --confirm %s to proceed",
				cliArgs.ControlPlaneName, cliArgs.ControlPlaneName,
			), nil)
			os.Exit(1)
		}

		// default the tag to the sha-suffixed dev tag the image build resolves
		// so reinstall picks up images built by 'tptdev build' without requiring
		// --tag every invocation; matches the build command's default.
		if cliArgs.ControlPlaneImageTag == "" {
			tag, err := util.ResolveImageTag(version.GetVersion())
			if err != nil {
				cli.Error(fmt.Sprintf("failed to resolve default image tag: %s\nspecify a tag explicitly with --tag/-t", err), nil)
				os.Exit(1)
			}
			cliArgs.ControlPlaneImageTag = tag
		}

		cpi, err := cliArgs.CreateInstaller()
		if err != nil {
			cli.Error("failed to create threeport control plane installer", err)
			os.Exit(1)
		}
		cpi.Opts.Namespace = installer.ControlPlaneNamespace
		cpi.Opts.Debug = cliArgs.Debug

		kubeClient, mapper, err := client_lib.GetKubeDynamicClientAndMapper(cliArgs.KubeconfigPath)
		if err != nil {
			cli.Error("failed to create kube client", err)
			os.Exit(1)
		}

		// narrow the controller list to the subset that should be
		// reinstalled. With --apis set the user picks the subset
		// explicitly; otherwise auto-detect from the cluster's
		// installer-managed deployments so the reinstall mirrors the
		// current install rather than expanding it.
		selected, selectedNames, autoDetected, err := installer.SelectControllersForReinstall(
			kubeClient,
			cpi.Opts.Namespace,
			installer.ParseApis(reinstallApis),
			cpi.Opts.ControllerList,
		)
		if err != nil {
			cli.Error("failed to select controllers for reinstall", err)
			os.Exit(1)
		}
		source := "specified via --apis"
		if autoDetected {
			source = "auto-detected from cluster"
		}
		cli.Info(fmt.Sprintf(
			"reinstalling %d controller(s) (%s): %s",
			len(selected), source, strings.Join(selectedNames, ", "),
		))
		cpi.Opts.ControllerList = selected

		// detect whether the running api was installed with auth so the
		// reapply path configures the new pods the same way
		cpi.Opts.AuthEnabled = detectAuthEnabled(kubeClient)

		// load the existing CA from the cluster when auth is on so
		// controllers added since the original install get fresh
		// certs signed by the persistent CA. existing controllers'
		// cert secrets survive the sweep and are reused as-is.
		var authConfig *auth.AuthConfig
		if cpi.Opts.AuthEnabled {
			authConfig, err = cpi.LoadAuthConfigFromCluster(kubeClient, &mapper)
			if err != nil {
				cli.Error("failed to load existing CA from cluster", err)
				os.Exit(1)
			}
		}

		// drop the database before the reapply so the install that
		// follows recreates it empty and the migrations run from scratch
		if reinstallDropDatabase {
			if err := cpi.DropDatabase(kubeClient, &mapper); err != nil {
				cli.Error("failed to drop control plane database", err)
				os.Exit(1)
			}
		}

		if err := cpi.Reinstall(kubeClient, &mapper, authConfig); err != nil {
			cli.Error("failed to reinstall threeport control plane", err)
			os.Exit(1)
		}

		// the reinstall replaces kubernetes resources only. The records
		// the genesis install wrote through the threeport API went out
		// with the dropped database, so restore them; without them the
		// control plane comes back with no default kubernetes runtime
		// to place workloads on and no record of itself. Restoring is
		// also available on its own, for a database emptied by
		// something other than this command.
		if reinstallDropDatabase || reinstallRestoreBootstrap {
			if err := cli.EnsureBootstrapObjects(cpi); err != nil {
				cli.Error("failed to restore control plane bootstrap objects", err)
				os.Exit(1)
			}
		}

		cli.Complete("threeport control plane reinstalled")
	},
}

func init() {
	rootCmd.AddCommand(reinstallCmd)

	reinstallCmd.Flags().StringVarP(
		&cliArgs.ControlPlaneName,
		"name", "n", tptdev.DefaultInstanceName, "Name of dev genesis control plane.",
	)
	reinstallCmd.Flags().StringVarP(
		&cliArgs.KubeconfigPath,
		"kubeconfig", "k", "", "Path to kubeconfig (default is $KUBECONFIG, then ~/.kube/config).",
	)
	reinstallCmd.Flags().StringVarP(
		&cliArgs.ControlPlaneImageRepo,
		"control-plane-image-namespace", "r", "", "Image namespace to pull threeport control plane images from.",
	)
	reinstallCmd.Flags().StringVarP(
		&cliArgs.ControlPlaneImageTag,
		"control-plane-image-tag", "t", "", "Image tag for threeport control plane images. Defaults to the current git branch name.",
	)
	reinstallCmd.Flags().BoolVar(
		&cliArgs.Debug,
		"debug", false, "If true, pod imagePullPolicy is set to Always so each rollout re-pulls the tag.",
	)
	reinstallCmd.Flags().StringVar(
		&reinstallApis,
		"apis", "", "Optional. Comma-separated list of sdk-config api object group names (e.g. kubernetes_workload,gateway) to limit the reinstall to those apis' controllers. Defaults to empty, which auto-detects the controller subset from the cluster's installer-managed deployments.",
	)
	reinstallCmd.Flags().BoolVar(
		&reinstallDropDatabase,
		"drop-database", false, "Drop the database schema before reapplying, so the migrations run from scratch. Requires --confirm and a control plane installed at the development tier. The data cannot be recovered.",
	)
	reinstallCmd.Flags().StringVar(
		&reinstallConfirm,
		"confirm", "", "Name of the control plane whose database is being dropped. Must match --name. Required with --drop-database.",
	)
	reinstallCmd.Flags().BoolVar(
		&reinstallRestoreBootstrap,
		"restore-bootstrap", false, "Recreate the kubernetes runtime and control plane records the API needs in order to accept work, for a database emptied outside this command. Creates only the records that are missing. Implied by --drop-database.",
	)
}
