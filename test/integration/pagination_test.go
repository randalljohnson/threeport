package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	cli "github.com/threeport/threeport/pkg/cli/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// paginationTestPrefix names the objects this test creates so its assertions
// can tell them apart from whatever else the control plane already holds.
const paginationTestPrefix = "pagination-filter-test"

// TestPaginatedListAppliesFilter covers the defect where a filtered list
// returned unfiltered rows once the result set outgrew the page limit. The
// count that decides whether to paginate applied the filter, and the page query
// that ran afterwards did not, so a caller filtering by name got every object
// back as soon as a second page was needed.
//
// It creates more matching and non-matching objects than the page limit, then
// lists with a limit small enough to force pagination and asserts every object
// returned matches. A unit test cannot reach this: the page query runs AS OF
// SYSTEM TIME against a snapshot, which is CockroachDB-only syntax.
func TestPaginatedListAppliesFilter(t *testing.T) {
	cli.InitConfig(nil, "")

	threeportConfig, _, err := cli.GetThreeportConfig("")
	require.Nil(t, err, "should have no error getting threeport config")
	apiClient, err := threeportConfig.GetHTTPClient(threeportConfig.CurrentControlPlane)
	require.Nil(t, err, "should have no error creating http client")
	controlPlaneConfig, err := threeportConfig.GetControlPlaneConfig(threeportConfig.CurrentControlPlane)
	require.Nil(t, err, "should not get an error looking up Threeport API endpoint")
	apiEndpoint := controlPlaneConfig.APIServer

	// arrange: enough objects on both sides of the filter that a small page
	// limit forces at least one continuation request
	const perGroup = 5
	matchName := fmt.Sprintf("%s-match", paginationTestPrefix)
	otherName := fmt.Sprintf("%s-other", paginationTestPrefix)

	var created []uint
	defer func() {
		for _, id := range created {
			retryDelete(t, fmt.Sprintf("kubernetes workload definition %d", id), func() error {
				_, err := client.DeleteKubernetesWorkloadDefinition(apiClient, apiEndpoint, id)
				return err
			})
		}
	}()

	for i := range perGroup {
		for _, name := range []string{matchName, otherName} {
			definition := v0.KubernetesWorkloadDefinition{
				Definition: v0.Definition{
					Name: util.Ptr(fmt.Sprintf("%s-%d", name, i)),
				},
				YAMLDocument: util.Ptr(paginationTestManifest),
			}
			result, err := client.CreateKubernetesWorkloadDefinition(apiClient, apiEndpoint, &definition)
			require.Nil(t, err, "should have no error creating kubernetes workload definition")
			created = append(created, *result.ID)
		}
	}

	// action: filter by an exact name with a page limit below the number of
	// objects that exist, so the query has to paginate to finish
	target := fmt.Sprintf("%s-0", matchName)
	results, err := client.GetKubernetesWorkloadDefinitionsByQueryString(
		apiClient,
		apiEndpoint,
		fmt.Sprintf("name=%s&limit=2", target),
	)
	require.Nil(t, err, "should have no error listing kubernetes workload definitions")

	// assert: every object returned matches the filter. Before the fix the
	// page query dropped the filter and returned the whole table
	require.NotNil(t, results)
	assert.Len(t, *results, 1, "exactly one definition carries this name")
	for _, definition := range *results {
		require.NotNil(t, definition.Name)
		assert.Equal(t, target, *definition.Name, "a paginated page must not return rows the filter excludes")
	}
}

// paginationTestManifest is the smallest valid workload document, since this
// test exercises the list query rather than anything the workload deploys.
const paginationTestManifest = `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: pagination-filter-test
data:
  key: value
`
