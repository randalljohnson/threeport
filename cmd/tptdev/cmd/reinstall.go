/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	auth "github.com/threeport/threeport/pkg/auth/v0"
	cli "github.com/threeport/threeport/pkg/cli/v0"
	client_lib "github.com/threeport/threeport/pkg/client/lib/v0"
	installer "github.com/threeport/threeport/pkg/threeport-installer/v0"
	"github.com/threeport/threeport/pkg/threeport-installer/v0/tptdev"
)

// reinstallApis holds the --apis flag value: a comma-separated list
// of sdk-config ApiObjectGroup names whose controllers should be
// reinstalled. When empty, the reinstall auto-detects the controller
// subset from the cluster's installer-managed deployments.
var reinstallApis string

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

Intended for dev environments only. The reinstall command does not
build images; run 'tptdev build --push' first if the image needs to
change.`,
	Run: func(cmd *cobra.Command, args []string) {
		cliArgs.GetControlPlaneEnvVars()

		// default tag to current git branch name so reinstall picks up
		// images built by 'tptdev build' without requiring --tag every
		// invocation; matches the build command's default.
		if cliArgs.ControlPlaneImageTag == "" {
			branch, err := gitBranchName()
			if err != nil {
				cli.Error(fmt.Sprintf("failed to determine git branch for default tag: %s\nspecify a tag explicitly with --tag/-t", err), nil)
				os.Exit(1)
			}
			cliArgs.ControlPlaneImageTag = branch
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

		if err := cpi.Reinstall(kubeClient, &mapper, authConfig); err != nil {
			cli.Error("failed to reinstall threeport control plane", err)
			os.Exit(1)
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
}
