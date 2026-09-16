package gcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"

	apiserver_lib "github.com/threeport/threeport/pkg/api-server/lib/v0"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

const (
	gkeCredsInstanceID   uint = 11
	gkeCredsDefinitionID uint = 21
	gkeCredsProviderID   uint = 31
)

func gkeCredsWrite(t *testing.T, w http.ResponseWriter, data []apiserver_lib.Object) {
	t.Helper()
	body, err := json.Marshal(apiserver_lib.Response{Data: data})
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func gkeCredsServer(t *testing.T, provider *v0.GcpProvider) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	inst := &v0.GcpGkeKubernetesRuntimeInstance{
		Common:                              v0.Common{ID: util.Ptr(gkeCredsInstanceID)},
		GcpGkeKubernetesRuntimeDefinitionID: util.Ptr(gkeCredsDefinitionID),
		GcpProviderID:                       util.Ptr(gkeCredsProviderID),
	}
	def := &v0.GcpGkeKubernetesRuntimeDefinition{
		Common: v0.Common{ID: util.Ptr(gkeCredsDefinitionID)},
	}
	mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeInstances, gkeCredsInstanceID), func(w http.ResponseWriter, r *http.Request) {
		gkeCredsWrite(t, w, []apiserver_lib.Object{inst})
	})
	mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathGcpGkeKubernetesRuntimeDefinitions, gkeCredsDefinitionID), func(w http.ResponseWriter, r *http.Request) {
		gkeCredsWrite(t, w, []apiserver_lib.Object{def})
	})
	mux.HandleFunc(fmt.Sprintf("%s/%d", v0.PathGcpProviders, gkeCredsProviderID), func(w http.ResponseWriter, r *http.Request) {
		gkeCredsWrite(t, w, []apiserver_lib.Object{provider})
	})
	return httptest.NewServer(mux)
}

func TestGkeLifecycleBuildInfraRejectsEmptyServiceAccountCredentials(t *testing.T) {
	empty := ""
	for _, creds := range []*string{nil, &empty} {
		srv := gkeCredsServer(t, &v0.GcpProvider{
			Common:                    v0.Common{ID: util.Ptr(gkeCredsProviderID)},
			ServiceAccountCredentials: creds,
		})
		log := logr.Discard()
		g := &gkeLifecycle{
			r: &controller.Reconciler{
				APIClient: srv.Client(),
				APIServer: strings.TrimPrefix(srv.URL, "http://"),
			},
			instanceID: gkeCredsInstanceID,
			log:        &log,
		}
		_, err := g.BuildInfra()
		require.Error(t, err)
		require.Contains(t, err.Error(), "no service account credentials")
		srv.Close()
	}
}
