package provider

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	ocicontainerengine "github.com/oracle/oci-go-sdk/v65/containerengine"
	ocicore "github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/pulumi/pulumi-oci/sdk/v3/go/oci"
	"github.com/pulumi/pulumi-oci/sdk/v3/go/oci/containerengine"
	"github.com/pulumi/pulumi-oci/sdk/v3/go/oci/core"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	kube "github.com/threeport/threeport/pkg/kube/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
	"gopkg.in/yaml.v2"
	"gorm.io/datatypes"
)

// KubernetesRuntimeInfraOKE represents the infrastructure for a threeport-managed OKE
// (Oracle Kubernetes Engine) cluster.
type KubernetesRuntimeInfraOKE struct {
	// The unique name of the kubernetes runtime instance managed by threeport.
	RuntimeInstanceName string

	// Version of the OKE cluster.
	Version string

	// The Oracle Cloud tenancy ID where the cluster infra is provisioned.
	TenancyOCID string

	// The Oracle Cloud compartment ID where resources will be created.
	CompartmentOCID string

	// The Oracle Cloud config provider.
	ConfigProvider common.ConfigurationProvider

	// The Oracle Cloud region where resources will be created.
	Region string

	// The Oracle Cloud shape used for the worker nodes.
	WorkerNodeShape string

	// The number of nodes initially created for the worker node pool.
	WorkerNodeInitialCount int32

	// The path to the Pulumi state directory
	stateDir string

	// Service user credentials for OCI operations
	ServiceUserOCID string
	PrivateKeyPEM   string
	PublicKeyPEM    string
	Fingerprint     string
}

// Create installs a Kubernetes cluster using Oracle Cloud OKE for threeport workloads.
func (i *KubernetesRuntimeInfraOKE) Create() (*kube.KubeConnectionInfo, error) {
	// create compartment for this threeport instance
	if err := i.createOCIUserAndCredentials(); err != nil {
		return nil, fmt.Errorf("failed to create compartment: %w", err)
	}

	// set up Pulumi workspace and get stack
	stack, err := i.setupPulumiWorkspace(func(ctx *pulumi.Context) error {

		// get the availability domain name
		availabilityDomain, err := i.getAvailabilityDomainName()
		if err != nil {
			return fmt.Errorf("failed to get availability domain: %w", err)
		}

		// create OCI provider with explicit configuration using service user credentials
		ociProvider, err := oci.NewProvider(ctx, "oci-provider", &oci.ProviderArgs{
			Region:      pulumi.String(i.Region),
			TenancyOcid: pulumi.String(i.TenancyOCID),
			UserOcid:    pulumi.String(i.ServiceUserOCID),
			Fingerprint: pulumi.String(i.Fingerprint),
			PrivateKey:  pulumi.String(i.PrivateKeyPEM),
		})
		if err != nil {
			return fmt.Errorf("failed to create OCI provider: %w", err)
		}

		// create VCN for the cluster
		vcn, err := core.NewVcn(ctx, fmt.Sprintf("%s-vcn", i.RuntimeInstanceName), &core.VcnArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			CidrBlock:     pulumi.String("10.0.0.0/16"),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-vcn", i.RuntimeInstanceName)),
			DnsLabel:      pulumi.String(createDNSLabel(i.RuntimeInstanceName)),
			IsIpv6enabled: pulumi.Bool(false),
		}, pulumi.Provider(ociProvider),
			pulumi.DeleteBeforeReplace(true),
			pulumi.Protect(false))
		if err != nil {
			return fmt.Errorf("failed to create VCN: %w", err)
		}

		// create Internet Gateway
		internetGateway, err := core.NewInternetGateway(ctx, fmt.Sprintf("%s-ig", i.RuntimeInstanceName), &core.InternetGatewayArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			VcnId:         vcn.ID(),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-ig", i.RuntimeInstanceName)),
			Enabled:       pulumi.Bool(true),
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn}))
		if err != nil {
			return fmt.Errorf("failed to create Internet Gateway: %w", err)
		}

		// create NAT Gateway
		natGateway, err := core.NewNatGateway(ctx, fmt.Sprintf("%s-ng", i.RuntimeInstanceName), &core.NatGatewayArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			VcnId:         vcn.ID(),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-ng", i.RuntimeInstanceName)),
			BlockTraffic:  pulumi.Bool(false),
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn}))
		if err != nil {
			return fmt.Errorf("failed to create NAT Gateway: %w", err)
		}

		// get the service gateway service ID and cidr block
		serviceID, serviceCidrBlock, err := i.getServiceGatewayServiceIdAndCidrBlock()
		if err != nil {
			return fmt.Errorf("failed to get service gateway service ID: %w", err)
		}

		// create Service Gateway
		serviceGateway, err := core.NewServiceGateway(ctx, fmt.Sprintf("%s-sg", i.RuntimeInstanceName), &core.ServiceGatewayArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			VcnId:         vcn.ID(),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-sg", i.RuntimeInstanceName)),
			Services: core.ServiceGatewayServiceArray{
				&core.ServiceGatewayServiceArgs{
					ServiceId: pulumi.String(serviceID),
				},
			},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn}))
		if err != nil {
			return fmt.Errorf("failed to create Service Gateway: %w", err)
		}

		// create route table for public subnet
		publicRouteTable, err := core.NewRouteTable(ctx, fmt.Sprintf("%s-public-rt", i.RuntimeInstanceName), &core.RouteTableArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			VcnId:         vcn.ID(),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-public-rt", i.RuntimeInstanceName)),
			RouteRules: core.RouteTableRouteRuleArray{
				&core.RouteTableRouteRuleArgs{
					Destination:     pulumi.String("0.0.0.0/0"),
					DestinationType: pulumi.String("CIDR_BLOCK"),
					NetworkEntityId: internetGateway.ID(),
				},
			},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{internetGateway}))
		if err != nil {
			return fmt.Errorf("failed to create public route table: %w", err)
		}

		// create route table for private subnet
		privateRouteTable, err := core.NewRouteTable(ctx, fmt.Sprintf("%s-private-rt", i.RuntimeInstanceName), &core.RouteTableArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			VcnId:         vcn.ID(),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-private-rt", i.RuntimeInstanceName)),
			RouteRules: core.RouteTableRouteRuleArray{
				&core.RouteTableRouteRuleArgs{
					Destination:     pulumi.String("0.0.0.0/0"),
					DestinationType: pulumi.String("CIDR_BLOCK"),
					NetworkEntityId: natGateway.ID(),
				},
				&core.RouteTableRouteRuleArgs{
					Destination:     pulumi.String(serviceCidrBlock),
					DestinationType: pulumi.String("SERVICE_CIDR_BLOCK"),
					NetworkEntityId: serviceGateway.ID(),
				},
			},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{natGateway, serviceGateway}))
		if err != nil {
			return fmt.Errorf("failed to create private route table: %w", err)
		}

		// define subnets to be used by security lists, cluster, and load balancer
		publicSubnetCidrBlock := "10.0.0.0/28"
		privateSubnetCidrBlock := "10.0.10.0/24"
		loadBalancerSubnetCidrBlock := "10.0.20.0/24"

		// create security list for public subnet
		publicSecList, err := core.NewSecurityList(ctx, fmt.Sprintf("%s-public-seclist", i.RuntimeInstanceName), &core.SecurityListArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			VcnId:         vcn.ID(),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-public-seclist", i.RuntimeInstanceName)),
			IngressSecurityRules: core.SecurityListIngressSecurityRuleArray{
				// allow Kubernetes API server traffic from anywhere
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol: pulumi.String("6"), // TCP
					Source:   pulumi.String("0.0.0.0/0"),
					TcpOptions: &core.SecurityListIngressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(6443),
						Min: pulumi.Int(6443),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow Kubernetes API server traffic from private subnet
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol: pulumi.String("6"), // TCP
					Source:   pulumi.String(privateSubnetCidrBlock),
					TcpOptions: &core.SecurityListIngressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(6443),
						Min: pulumi.Int(6443),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow port 12250 from private subnet
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol: pulumi.String("6"), // TCP
					Source:   pulumi.String(privateSubnetCidrBlock),
					TcpOptions: &core.SecurityListIngressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(12250),
						Min: pulumi.Int(12250),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow ICMP type 3 code 4 from private subnet
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol: pulumi.String("1"), // ICMP
					Source:   pulumi.String(privateSubnetCidrBlock),
					IcmpOptions: &core.SecurityListIngressSecurityRuleIcmpOptionsArgs{
						Type: pulumi.Int(3),
						Code: pulumi.Int(4),
					},
					Stateless: pulumi.Bool(false),
				},
			},
			EgressSecurityRules: core.SecurityListEgressSecurityRuleArray{
				// allow traffic to Oracle Services Network
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:        pulumi.String("6"), // TCP
					Destination:     pulumi.String(serviceCidrBlock),
					DestinationType: pulumi.String("SERVICE_CIDR_BLOCK"),
					TcpOptions: &core.SecurityListEgressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(443),
						Min: pulumi.Int(443),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow all TCP traffic to private subnet
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("6"), // TCP
					Destination: pulumi.String(privateSubnetCidrBlock),
					Stateless:   pulumi.Bool(false),
				},
				// allow ICMP type 3 code 4 to private subnet
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("1"), // ICMP
					Destination: pulumi.String(privateSubnetCidrBlock),
					IcmpOptions: &core.SecurityListEgressSecurityRuleIcmpOptionsArgs{
						Type: pulumi.Int(3),
						Code: pulumi.Int(4),
					},
					Stateless: pulumi.Bool(false),
				},
			},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn}))
		if err != nil {
			return fmt.Errorf("failed to create public security list: %w", err)
		}

		// create security list for worker nodes subnet (private)
		workerNodesSecList, err := core.NewSecurityList(ctx, fmt.Sprintf("%s-worker-nodes-seclist", i.RuntimeInstanceName), &core.SecurityListArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			VcnId:         vcn.ID(),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-worker-nodes-seclist", i.RuntimeInstanceName)),
			IngressSecurityRules: core.SecurityListIngressSecurityRuleArray{
				// allow all traffic from private subnet
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol:  pulumi.String("all"),
					Source:    pulumi.String(privateSubnetCidrBlock),
					Stateless: pulumi.Bool(false),
				},
				// allow ICMP type 3 code 4 from public subnet
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol: pulumi.String("1"), // ICMP
					Source:   pulumi.String(publicSubnetCidrBlock),
					IcmpOptions: &core.SecurityListIngressSecurityRuleIcmpOptionsArgs{
						Type: pulumi.Int(3),
						Code: pulumi.Int(4),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow all TCP traffic from public subnet
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol:  pulumi.String("6"), // TCP
					Source:    pulumi.String(publicSubnetCidrBlock),
					Stateless: pulumi.Bool(false),
				},
				// allow SSH from anywhere
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol: pulumi.String("6"), // TCP
					Source:   pulumi.String("0.0.0.0/0"),
					TcpOptions: &core.SecurityListIngressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(22),
						Min: pulumi.Int(22),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow all traffic from load balancer subnet
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol:  pulumi.String("all"),
					Source:    pulumi.String(loadBalancerSubnetCidrBlock),
					Stateless: pulumi.Bool(false),
				},
			},
			EgressSecurityRules: core.SecurityListEgressSecurityRuleArray{
				// allow all traffic to private subnet
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("all"),
					Destination: pulumi.String(privateSubnetCidrBlock),
					Stateless:   pulumi.Bool(false),
				},
				// allow Kubernetes API server traffic to public subnet
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("6"), // TCP
					Destination: pulumi.String(publicSubnetCidrBlock),
					TcpOptions: &core.SecurityListEgressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(6443),
						Min: pulumi.Int(6443),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow TCP port 12250 to public subnet
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("6"), // TCP
					Destination: pulumi.String(publicSubnetCidrBlock),
					TcpOptions: &core.SecurityListEgressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(12250),
						Min: pulumi.Int(12250),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow ICMP type 3 code 4 to public subnet
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("1"), // ICMP
					Destination: pulumi.String(publicSubnetCidrBlock),
					IcmpOptions: &core.SecurityListEgressSecurityRuleIcmpOptionsArgs{
						Type: pulumi.Int(3),
						Code: pulumi.Int(4),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow TCP port 443 to Oracle Services Network
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:        pulumi.String("6"), // TCP
					Destination:     pulumi.String(serviceCidrBlock),
					DestinationType: pulumi.String("SERVICE_CIDR_BLOCK"),
					TcpOptions: &core.SecurityListEgressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(443),
						Min: pulumi.Int(443),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow ICMP type 3 code 4 to anywhere
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("1"), // ICMP
					Destination: pulumi.String("0.0.0.0/0"),
					IcmpOptions: &core.SecurityListEgressSecurityRuleIcmpOptionsArgs{
						Type: pulumi.Int(3),
						Code: pulumi.Int(4),
					},
					Stateless: pulumi.Bool(false),
				},
				// allow all traffic to anywhere
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("all"),
					Destination: pulumi.String("0.0.0.0/0"),
					Stateless:   pulumi.Bool(false),
				},
			},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn}))
		if err != nil {
			return fmt.Errorf("failed to create worker nodes security list: %w", err)
		}

		// create load balancer security list
		loadBalancerSecList, err := core.NewSecurityList(ctx, fmt.Sprintf("%s-load-balancer-seclist", i.RuntimeInstanceName), &core.SecurityListArgs{
			CompartmentId: pulumi.String(i.CompartmentOCID),
			VcnId:         vcn.ID(),
			DisplayName:   pulumi.String(fmt.Sprintf("%s-load-balancer-seclist", i.RuntimeInstanceName)),
			IngressSecurityRules: core.SecurityListIngressSecurityRuleArray{
				// allow 443 from anywhere
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol:  pulumi.String("6"), // TCP
					Source:    pulumi.String("0.0.0.0/0"),
					Stateless: pulumi.Bool(false),
					TcpOptions: &core.SecurityListIngressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(443),
						Min: pulumi.Int(443),
					},
				},
				// allow 80 from anywhere
				&core.SecurityListIngressSecurityRuleArgs{
					Protocol:  pulumi.String("6"), // TCP
					Source:    pulumi.String("0.0.0.0/0"),
					Stateless: pulumi.Bool(false),
					TcpOptions: &core.SecurityListIngressSecurityRuleTcpOptionsArgs{
						Max: pulumi.Int(80),
						Min: pulumi.Int(80),
					},
				},
			},
			EgressSecurityRules: core.SecurityListEgressSecurityRuleArray{
				// allow all traffic to private subnet
				&core.SecurityListEgressSecurityRuleArgs{
					Protocol:    pulumi.String("all"),
					Destination: pulumi.String(privateSubnetCidrBlock),
					Stateless:   pulumi.Bool(false),
				},
			},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn}))
		if err != nil {
			return fmt.Errorf("failed to create load balancer security list: %w", err)
		}

		// create public subnet
		publicSubnet, err := core.NewSubnet(ctx, fmt.Sprintf("%s-public-subnet", i.RuntimeInstanceName), &core.SubnetArgs{
			CidrBlock:              pulumi.String(publicSubnetCidrBlock),
			CompartmentId:          pulumi.String(i.CompartmentOCID),
			VcnId:                  vcn.ID(),
			DisplayName:            pulumi.String(fmt.Sprintf("%s-public-subnet", i.RuntimeInstanceName)),
			DnsLabel:               pulumi.String(createDNSLabel(fmt.Sprintf("%s-public", i.RuntimeInstanceName))),
			ProhibitPublicIpOnVnic: pulumi.Bool(false),
			RouteTableId:           publicRouteTable.ID(),
			SecurityListIds:        pulumi.StringArray{publicSecList.ID()},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn, publicRouteTable, publicSecList}))
		if err != nil {
			return fmt.Errorf("failed to create public subnet: %w", err)
		}

		// create private subnet
		privateSubnet, err := core.NewSubnet(ctx, fmt.Sprintf("%s-private-subnet", i.RuntimeInstanceName), &core.SubnetArgs{
			CidrBlock:              pulumi.String(privateSubnetCidrBlock),
			CompartmentId:          pulumi.String(i.CompartmentOCID),
			VcnId:                  vcn.ID(),
			DisplayName:            pulumi.String(fmt.Sprintf("%s-private-subnet", i.RuntimeInstanceName)),
			DnsLabel:               pulumi.String(createDNSLabel(fmt.Sprintf("%s-private", i.RuntimeInstanceName))),
			ProhibitPublicIpOnVnic: pulumi.Bool(true),
			RouteTableId:           privateRouteTable.ID(),
			SecurityListIds:        pulumi.StringArray{workerNodesSecList.ID()},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn, privateRouteTable, workerNodesSecList}))
		if err != nil {
			return fmt.Errorf("failed to create private subnet: %w", err)
		}

		// create load balancer subnet
		loadBalancerSubnet, err := core.NewSubnet(ctx, fmt.Sprintf("%s-lb-subnet", i.RuntimeInstanceName), &core.SubnetArgs{
			CidrBlock:              pulumi.String(loadBalancerSubnetCidrBlock),
			CompartmentId:          pulumi.String(i.CompartmentOCID),
			VcnId:                  vcn.ID(),
			DisplayName:            pulumi.String(fmt.Sprintf("%s-lb-subnet", i.RuntimeInstanceName)),
			DnsLabel:               pulumi.String(createDNSLabel(fmt.Sprintf("%s-lb", i.RuntimeInstanceName))),
			ProhibitPublicIpOnVnic: pulumi.Bool(false),
			RouteTableId:           publicRouteTable.ID(),
			SecurityListIds:        pulumi.StringArray{loadBalancerSecList.ID()},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{vcn, publicRouteTable, loadBalancerSecList}))
		if err != nil {
			return fmt.Errorf("failed to create load balancer subnet: %w", err)
		}

		// create OKE Cluster with explicit dependency on networking components
		cluster, err := containerengine.NewCluster(ctx, i.RuntimeInstanceName, &containerengine.ClusterArgs{
			CompartmentId:     pulumi.String(i.CompartmentOCID),
			Name:              pulumi.String(i.RuntimeInstanceName),
			VcnId:             vcn.ID(),
			KubernetesVersion: pulumi.String(i.Version),
			EndpointConfig: &containerengine.ClusterEndpointConfigArgs{
				IsPublicIpEnabled: pulumi.Bool(true),
				SubnetId:          publicSubnet.ID(),
				NsgIds:            pulumi.StringArray{}, // optional: Add network security group IDs if needed
			},
			Options: &containerengine.ClusterOptionsArgs{
				KubernetesNetworkConfig: &containerengine.ClusterOptionsKubernetesNetworkConfigArgs{
					PodsCidr:     pulumi.String("10.244.0.0/16"),
					ServicesCidr: pulumi.String("10.96.0.0/16"),
				},
				ServiceLbSubnetIds: pulumi.StringArray{loadBalancerSubnet.ID()},
			},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{
				vcn,
				internetGateway,
				natGateway,
				serviceGateway,
				publicSubnet,
				privateSubnet,
				publicRouteTable,
				privateRouteTable,
			}))
		if err != nil {
			return fmt.Errorf("failed to create OKE cluster: %w", err)
		}

		// get the OKE worker node image OCID
		imageOCID, err := i.getOKEWorkerNodeImageOCID()
		if err != nil {
			return fmt.Errorf("failed to get OKE worker node image OCID: %w", err)
		}

		// create node pool
		_, err = containerengine.NewNodePool(ctx, fmt.Sprintf("%s-nodepool", i.RuntimeInstanceName), &containerengine.NodePoolArgs{
			ClusterId:         cluster.ID(),
			CompartmentId:     pulumi.String(i.CompartmentOCID),
			Name:              pulumi.String(fmt.Sprintf("%s-nodepool", i.RuntimeInstanceName)),
			NodeShape:         pulumi.String(i.WorkerNodeShape),
			KubernetesVersion: pulumi.String(i.Version),
			InitialNodeLabels: containerengine.NodePoolInitialNodeLabelArray{
				&containerengine.NodePoolInitialNodeLabelArgs{
					Key:   pulumi.String("threeport.io/managed"),
					Value: pulumi.String("true"),
				},
			},
			NodeConfigDetails: &containerengine.NodePoolNodeConfigDetailsArgs{
				Size: pulumi.Int(0), // Set to 0 to test infrastructure creation without nodepool
				PlacementConfigs: containerengine.NodePoolNodeConfigDetailsPlacementConfigArray{
					&containerengine.NodePoolNodeConfigDetailsPlacementConfigArgs{
						AvailabilityDomain: pulumi.String(availabilityDomain),
						SubnetId:           privateSubnet.ID(),
					},
				},
			},
			NodeSourceDetails: &containerengine.NodePoolNodeSourceDetailsArgs{
				ImageId:    pulumi.String(imageOCID),
				SourceType: pulumi.String("IMAGE"),
			},
			NodeShapeConfig: &containerengine.NodePoolNodeShapeConfigArgs{
				Ocpus:       pulumi.Float64(2.0),
				MemoryInGbs: pulumi.Float64(12.0),
			},
		}, pulumi.Provider(ociProvider),
			pulumi.DependsOn([]pulumi.Resource{cluster}))
		if err != nil {
			fmt.Printf("Failed to create node pool: %v\n", err)
			return fmt.Errorf("failed to create node pool: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to set up Pulumi workspace: %w", err)
	}

	// create a context for the automation API
	ctx := context.Background()

	// deploy the stack
	_, err = stack.Up(ctx, optup.ProgressStreams(os.Stdout))
	if err != nil {
		return nil, fmt.Errorf("failed to deploy stack: %w", err)
	}

	return i.GetConnection()
}

// Delete deletes an Oracle Cloud OKE cluster.
func (i *KubernetesRuntimeInfraOKE) Delete() error {
	// set up Pulumi workspace and get stack
	stack, err := i.setupPulumiWorkspace(func(ctx *pulumi.Context) error {
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to set up Pulumi workspace: %w", err)
	}

	// create a context for the automation API
	ctx := context.Background()

	// destroy the stack
	_, err = stack.Destroy(ctx, optdestroy.ProgressStreams(os.Stdout))
	if err != nil {
		return fmt.Errorf("failed to destroy stack: %w", err)
	}

	// remove the state directory after successful destruction
	if err := os.RemoveAll(i.stateDir); err != nil {
		return fmt.Errorf("failed to remove state directory: %w", err)
	}

	return nil
}

// GetClusterOCID gets the OCID of the OKE cluster.
func (i *KubernetesRuntimeInfraOKE) GetClusterOCID(okeClusterName string) (string, error) {

	containerClient, err := ocicontainerengine.NewContainerEngineClientWithConfigurationProvider(i.ConfigProvider)
	if err != nil {
		return "", fmt.Errorf("failed to create container engine client: %w", err)
	}

	// set the region for the client
	containerClient.SetRegion(i.Region)

	// list clusters to find the one with matching name
	request := ocicontainerengine.ListClustersRequest{
		CompartmentId: &i.CompartmentOCID,
		Name:          &i.RuntimeInstanceName,
		LifecycleState: []ocicontainerengine.ClusterLifecycleStateEnum{
			ocicontainerengine.ClusterLifecycleStateActive,
		},
	}

	response, err := containerClient.ListClusters(context.Background(), request)
	if err != nil {
		return "", fmt.Errorf("failed to list clusters: %w", err)
	}

	// find the cluster with the matching name
	for _, cluster := range response.Items {
		if cluster.Name != nil && *cluster.Name == okeClusterName {
			return *cluster.Id, nil
		}
	}

	return "", fmt.Errorf("no active cluster found with name %s", okeClusterName)
}

// GetConnection gets the latest connection info for authentication to an OKE cluster.
func (i *KubernetesRuntimeInfraOKE) GetConnection() (*kube.KubeConnectionInfo, error) {
	// create a new container engine client
	clusterOCID, err := i.GetClusterOCID(i.RuntimeInstanceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster OCID: %w", err)
	}

	containerClient, err := ocicontainerengine.NewContainerEngineClientWithConfigurationProvider(i.ConfigProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create container engine client: %w", err)
	}

	// set the region for the client
	containerClient.SetRegion(i.Region)

	// get cluster details to get the API endpoint
	getClusterRequest := ocicontainerengine.GetClusterRequest{
		ClusterId: &clusterOCID,
	}

	clusterDetails, err := containerClient.GetCluster(context.Background(), getClusterRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster details: %w", err)
	}

	if clusterDetails.Endpoints == nil || clusterDetails.Endpoints.PublicEndpoint == nil {
		return nil, fmt.Errorf("cluster endpoints not found")
	}

	// get the kubeconfig which contains the CA certificate
	kubeconfigRequest := ocicontainerengine.CreateKubeconfigRequest{
		ClusterId: &clusterOCID,
		CreateClusterKubeconfigContentDetails: ocicontainerengine.CreateClusterKubeconfigContentDetails{
			TokenVersion: common.String("2.0.0"),
			Expiration:   common.Int(86400),
		},
	}

	kubeconfigResponse, err := containerClient.CreateKubeconfig(context.Background(), kubeconfigRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	defer kubeconfigResponse.Content.Close()

	// read the kubeconfig content
	kubeconfigBytes, err := io.ReadAll(kubeconfigResponse.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to read kubeconfig content: %w", err)
	}

	// parse the kubeconfig using the KubeConfig struct
	var kubeconfig KubeConfig
	if err := yaml.Unmarshal(kubeconfigBytes, &kubeconfig); err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// validate and extract required fields
	if len(kubeconfig.Clusters) == 0 {
		return nil, fmt.Errorf("no clusters found in kubeconfig")
	}

	token, tokenExpirationTime, err := util.GenerateOkeToken(clusterOCID, i.ConfigProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	caCert, err := base64.StdEncoding.DecodeString(kubeconfig.Clusters[0].Cluster.CertificateAuthorityData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA certificate: %w", err)
	}

	// create connection info
	kubeConnInfo := &kube.KubeConnectionInfo{
		APIEndpoint:     *clusterDetails.Endpoints.PublicEndpoint,
		CACertificate:   string(caCert),
		Token:           token,
		TokenExpiration: tokenExpirationTime,
	}

	return kubeConnInfo, nil
}

// KubeConfig represents the structure of the kubeconfig file
type KubeConfig struct {
	Clusters []struct {
		Cluster struct {
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
}

// loadOCIConfig reads the OCI configuration using the OCI SDK and
// populates KubernetesRuntimeInfraOKE struct fields
func (i *KubernetesRuntimeInfraOKE) LoadOCIConfig(
	region,
	ociConfigProfile,
	compartmentOCID string,
) error {
	// get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// path to OCI config file
	ociConfigPath := filepath.Join(homeDir, ".oci", "config")

	// check if config file exists
	if _, err := os.Stat(ociConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("OCI config file not found at %s", ociConfigPath)
	}

	// load the configuration using the OCI SDK
	configProvider, err := common.ConfigurationProviderFromFileWithProfile(
		ociConfigPath,
		ociConfigProfile,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to load OCI configuration: %w", err)
	}
	i.ConfigProvider = configProvider

	// get the tenancy OCID
	i.TenancyOCID, err = configProvider.TenancyOCID()
	if err != nil {
		return fmt.Errorf("failed to get tenancy OCID: %w", err)
	} else if i.TenancyOCID == "" {
		return fmt.Errorf("tenancy OCID not found in OCI config")
	}

	// set the region if it is provided via cli flag,
	// otherwise get the region from the OCI config
	if region != "" {
		i.Region = region
	} else {
		i.Region, err = configProvider.Region()
		if err != nil {
			return fmt.Errorf("failed to get region: %w", err)
		} else if i.Region == "" {
			return fmt.Errorf("region not found in OCI config")
		}
	}

	// set the compartment OCID if it is provided via cli flag,
	// otherwise use the tenancy OCID as the root compartment
	if compartmentOCID != "" {
		i.CompartmentOCID = compartmentOCID
	} else {
		i.CompartmentOCID = i.TenancyOCID
	}

	return nil
}

// createDNSLabel creates a valid DNS label that meets OCI requirements:
// - Must be 15 characters or less
// - Must contain only lowercase alphanumeric characters
// - Maintains uniqueness by using parts of the original name
func createDNSLabel(name string) string {
	// convert to lowercase
	dnsLabel := strings.ToLower(name)

	// If longer than 15 chars, take first 7 and last 7 with 'x' in middle
	if len(dnsLabel) > 15 {
		dnsLabel = dnsLabel[:7] + "x" + dnsLabel[len(dnsLabel)-7:]
	}

	// Replace any non-alphanumeric chars with 'x'
	dnsLabel = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return 'x'
	}, dnsLabel)

	return dnsLabel
}

// getAvailabilityDomainName returns the full name of the first availability domain in the region
func (i *KubernetesRuntimeInfraOKE) getAvailabilityDomainName() (string, error) {
	// create a new identity client
	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(i.ConfigProvider)
	if err != nil {
		return "", fmt.Errorf("failed to create identity client: %w", err)
	}

	// set the region for the client
	identityClient.SetRegion(i.Region)

	// create a request to list availability domains
	request := identity.ListAvailabilityDomainsRequest{
		CompartmentId: common.String(i.CompartmentOCID),
	}

	// call the API to get availability domains
	response, err := identityClient.ListAvailabilityDomains(context.Background(), request)
	if err != nil {
		return "", fmt.Errorf("failed to list availability domains: %w", err)
	}

	// check if we have any availability domains
	if len(response.Items) == 0 {
		return "", fmt.Errorf("no availability domains found in region %s", i.Region)
	}

	// return the name of the first availability domain
	return *response.Items[0].Name, nil
}

// getServiceGatewayServiceIdAndCidrBlock returns the OCI service ID for the service gateway in a given region.
// This ID is used to identify the Oracle Services Network in the service gateway.
func (i *KubernetesRuntimeInfraOKE) getServiceGatewayServiceIdAndCidrBlock() (string, string, error) {
	// create a new virtual network client
	vcnClient, err := ocicore.NewVirtualNetworkClientWithConfigurationProvider(i.ConfigProvider)
	if err != nil {
		return "", "", fmt.Errorf("failed to create virtual network client: %w", err)
	}

	// set the region for the client
	vcnClient.SetRegion(i.Region)

	// create a request to list services
	request := ocicore.ListServicesRequest{}

	// call the API to get services
	response, err := vcnClient.ListServices(context.Background(), request)
	if err != nil {
		return "", "", fmt.Errorf("failed to list services: %w", err)
	}

	// find the Oracle Services Network service
	for _, service := range response.Items {
		if service.Name != nil && strings.Contains(*service.Name, "Services In Oracle Services Network") {
			return *service.Id, *service.CidrBlock, nil
		}
	}

	// If service not found, return an error
	return "", "", fmt.Errorf("Oracle Services Network service not found in region %s", i.Region)
}

// getLatestOKEVersion returns the latest Kubernetes version available in OKE
func (i *KubernetesRuntimeInfraOKE) getLatestOKEVersion() (string, error) {
	// create a new container engine client
	containerClient, err := ocicontainerengine.NewContainerEngineClientWithConfigurationProvider(i.ConfigProvider)
	if err != nil {
		return "", fmt.Errorf("failed to create container engine client: %w", err)
	}

	// set the region for the client
	containerClient.SetRegion(i.Region)

	// create a request to list node pool options
	request := ocicontainerengine.GetNodePoolOptionsRequest{
		CompartmentId:    common.String(i.CompartmentOCID),
		NodePoolOptionId: common.String("all"),
	}

	// call the API to get node pool options
	response, err := containerClient.GetNodePoolOptions(context.Background(), request)
	if err != nil {
		return "", fmt.Errorf("failed to get node pool options: %w", err)
	}

	// check if we have any images
	if len(response.Sources) == 0 {
		return "", fmt.Errorf("no OKE worker node images found")
	}

	// find the latest version by parsing version strings
	latestVersion := ""
	for _, source := range response.Sources {
		if sourceType, ok := source.(ocicontainerengine.NodeSourceViaImageOption); ok {
			name := *sourceType.SourceName
			// extract version from name (e.g., "OKE-1.30.10")
			if strings.Contains(name, "OKE-") {
				version := strings.Split(name, "OKE-")[1]
				version = strings.Split(version, "-")[0] // remove any trailing parts
				if latestVersion == "" || version > latestVersion {
					latestVersion = version
				}
			}
		}
	}

	if latestVersion == "" {
		return "", fmt.Errorf("could not determine latest OKE version")
	}

	latestVersion = "v" + latestVersion

	return latestVersion, nil
}

// getOKEWorkerNodeImageOCID returns the OCID of the OKE worker node image
// with version specified in struct
func (i *KubernetesRuntimeInfraOKE) getOKEWorkerNodeImageOCID() (string, error) {
	// create a new container engine client
	containerClient, err := ocicontainerengine.NewContainerEngineClientWithConfigurationProvider(i.ConfigProvider)
	if err != nil {
		return "", fmt.Errorf("failed to create container engine client: %w", err)
	}

	// set the region for the client
	containerClient.SetRegion(i.Region)

	// create a request to list node pool options
	request := ocicontainerengine.GetNodePoolOptionsRequest{
		CompartmentId:    common.String(i.CompartmentOCID),
		NodePoolOptionId: common.String("all"),
	}

	// call the API to get node pool options
	response, err := containerClient.GetNodePoolOptions(context.Background(), request)
	if err != nil {
		return "", fmt.Errorf("failed to get node pool options: %w", err)
	}

	// check if we have any images
	if len(response.Sources) == 0 {
		return "", fmt.Errorf("no OKE worker node images found")
	}

	// find an image with the specified Kubernetes version
	for _, source := range response.Sources {
		// try to get the concrete type
		if sourceType, ok := source.(ocicontainerengine.NodeSourceViaImageOption); ok {
			name := *sourceType.SourceName
			// remove leading 'v' from version for image search
			versionWithoutV := strings.TrimPrefix(i.Version, "v")
			if strings.Contains(name, fmt.Sprintf("OKE-%s", versionWithoutV)) &&
				strings.Contains(name, "aarch64") {
				return *sourceType.ImageId, nil
			}
		}
	}

	return "", fmt.Errorf("no suitable OKE worker node images found with aarch64 architecture and Kubernetes version %s", i.Version)
}

// setupPulumiWorkspace sets up the Pulumi workspace and environment for OKE operations
func (i *KubernetesRuntimeInfraOKE) setupPulumiWorkspace(program pulumi.RunFunc) (auto.Stack, error) {

	// set up state directory
	if err := i.setStateDir(); err != nil {
		return auto.Stack{}, fmt.Errorf("failed to set state directory: %w", err)
	}

	// set environment variables for Pulumi configuration
	if err := i.setPulumiEnvVars(); err != nil {
		return auto.Stack{}, fmt.Errorf("failed to set Pulumi environment variables: %w", err)
	}

	// create Pulumi.yaml project file
	pulumiYaml := `name: oke
runtime: go
description: Oracle Kubernetes Engine (OKE) cluster for Threeport
`
	pulumiYamlPath := filepath.Join(i.stateDir, "Pulumi.yaml")
	if err := os.WriteFile(pulumiYamlPath, []byte(pulumiYaml), 0644); err != nil {
		return auto.Stack{}, fmt.Errorf("failed to create Pulumi.yaml: %w", err)
	}

	ctx := context.Background()

	// create a new workspace with local state backend
	workspace, err := auto.NewLocalWorkspace(
		ctx,
		auto.Program(program),
		auto.WorkDir(i.stateDir),
	)
	if err != nil {
		return auto.Stack{}, fmt.Errorf("failed to create workspace: %w", err)
	}

	// create or select a stack with fully qualified name
	stack, err := auto.UpsertStack(ctx, i.getStackName(), workspace)
	if err != nil {
		return auto.Stack{}, fmt.Errorf("failed to create/select stack: %w", err)
	}

	// set up stack configuration
	err = stack.SetConfig(ctx, "oci:region", auto.ConfigValue{Value: i.Region})
	if err != nil {
		return auto.Stack{}, fmt.Errorf("failed to set region config: %w", err)
	}

	return stack, nil
}

// GetStackState returns the state of the OKE stack as a JSON object
func (i *KubernetesRuntimeInfraOKE) GetStackState() (*datatypes.JSON, error) {

	// set up state directory
	if err := i.setStateDir(); err != nil {
		return nil, fmt.Errorf("failed to set state directory: %w", err)
	}

	// set environment variables for Pulumi configuration
	if err := i.setPulumiEnvVars(); err != nil {
		return nil, fmt.Errorf("failed to set Pulumi environment variables: %w", err)
	}

	ctx := context.Background()

	// create a new workspace with local state backend
	workspace, err := auto.NewLocalWorkspace(
		ctx,
		auto.WorkDir(i.stateDir),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	// load stack from workspace
	stack, err := auto.SelectStack(ctx, i.getStackName(), workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to select stack: %w", err)
	}

	// get the stack's state
	state, err := stack.Export(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to export stack state: %w", err)
	}

	// convert state to JSON
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state to JSON: %w", err)
	}

	jsonState := datatypes.JSON(stateJSON)
	return &jsonState, nil
}

// SetStackState sets the state of the OKE stack from a JSON object
func (i *KubernetesRuntimeInfraOKE) SetStackState(state *datatypes.JSON) error {

	// set up state directory
	if err := i.setStateDir(); err != nil {
		return fmt.Errorf("failed to set state directory: %w", err)
	}

	// set environment variables for Pulumi configuration
	if err := i.setPulumiEnvVars(); err != nil {
		return fmt.Errorf("failed to set Pulumi environment variables: %w", err)
	}

	ctx := context.Background()

	// create a new workspace with local state backend
	workspace, err := auto.NewLocalWorkspace(
		ctx,
		auto.WorkDir(i.stateDir),
	)
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}

	// create/select stack
	stack, err := auto.UpsertStack(ctx, i.getStackName(), workspace)
	if err != nil {
		return fmt.Errorf("failed to create/select stack: %w", err)
	}

	// unmarshal state
	var pulumiState apitype.UntypedDeployment
	err = json.Unmarshal(*state, &pulumiState)
	if err != nil {
		return fmt.Errorf("failed to unmarshal state from JSON: %w", err)
	}

	// set the stack's state and persist to disk
	err = stack.Import(ctx, pulumiState)
	if err != nil {
		return fmt.Errorf("failed to import stack state: %w", err)
	}

	return nil
}

// setPulumiEnvVars sets the environment variables for Pulumi
func (i *KubernetesRuntimeInfraOKE) setPulumiEnvVars() error {
	os.Setenv("PULUMI_BACKEND_URL", "file://"+i.stateDir)
	os.Setenv("PULUMI_HOME", i.stateDir)
	os.Setenv("PULUMI_ORGANIZATION", "organization") // TODO: update these?
	os.Setenv("PULUMI_PROJECT", "oke")
	os.Setenv("PULUMI_CONFIG_PASSPHRASE", "threeport")

	// set plugin path to the default location
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	defaultPluginPath := filepath.Join(userHomeDir, ".pulumi", "plugins")
	os.Setenv("PULUMI_PLUGIN_PATH", defaultPluginPath)

	return nil
}

// setStateDir sets the state directory for the OKE stack
func (i *KubernetesRuntimeInfraOKE) setStateDir() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	i.stateDir = filepath.Join(homeDir, ".threeport", "pulumi-state", i.RuntimeInstanceName)

	// ensure state directory exists
	if err := os.MkdirAll(i.stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	return nil
}

// getStackName returns the name of the OKE stack
func (i *KubernetesRuntimeInfraOKE) getStackName() string {
	return fmt.Sprintf("organization/oke/%s", i.RuntimeInstanceName)
}

// createOCIUserAndCredentials creates a new compartment and sets up complete OCI user/group infrastructure for the threeport instance.
func (i *KubernetesRuntimeInfraOKE) createOCIUserAndCredentials() error {
	fmt.Printf("Creating OCI user and credentials using SDK\n")

	// create a new identity client
	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(i.ConfigProvider)
	if err != nil {
		return fmt.Errorf("failed to create identity client: %w", err)
	}

	// get the default region from the config provider for user/credential operations
	defaultRegion, err := i.ConfigProvider.Region()
	if err != nil {
		return fmt.Errorf("failed to get default region from config provider: %w", err)
	}

	// set the region for the client to use default region (home region for compartment creation)
	identityClient.SetRegion(defaultRegion)

	// Create compartment first
	if err := i.createOCICompartment(identityClient); err != nil {
		return fmt.Errorf("failed to create compartment: %w", err)
	}

	// Create service user
	if err := i.createOCIServiceUser(identityClient); err != nil {
		return fmt.Errorf("failed to create service user: %w", err)
	}

	// Generate API key pair
	if err := i.generateOCIAPIKeyPair(); err != nil {
		return fmt.Errorf("failed to generate API key pair: %w", err)
	}

	// Create API key for service user
	if err := i.createOCIAPIKey(identityClient); err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}

	// Create groups
	if err := i.createOCIGroup(identityClient); err != nil {
		return fmt.Errorf("failed to create groups: %w", err)
	}

	// Create policies
	if err := i.createOCIPolicy(identityClient); err != nil {
		return fmt.Errorf("failed to create policies: %w", err)
	}

	// Add user to groups
	if err := i.addOCIUserToGroup(identityClient); err != nil {
		return fmt.Errorf("failed to add user to groups: %w", err)
	}

	// Write OCI configuration files
	if err := i.writeOCIConfiguration(); err != nil {
		return fmt.Errorf("failed to write OCI configuration: %w", err)
	}

	// Validate user propagation across all OCI services
	if err := i.validateOCIUserPropagation(); err != nil {
		return fmt.Errorf("failed to validate user propagation: %w", err)
	}

	fmt.Printf("Successfully created OCI user and credentials\n")
	return nil
}

// createOCICompartment creates a new compartment for the threeport instance.
func (i *KubernetesRuntimeInfraOKE) createOCICompartment(client identity.IdentityClient) error {
	compartmentName := fmt.Sprintf("threeport-%s", i.RuntimeInstanceName)

	// Check if compartment already exists
	listRequest := identity.ListCompartmentsRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &compartmentName,
	}

	listResponse, err := client.ListCompartments(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list compartments: %w", err)
	}

	// If compartment exists, use it
	for _, compartment := range listResponse.Items {
		if *compartment.Name == compartmentName {
			i.CompartmentOCID = *compartment.Id
			fmt.Printf("Using existing compartment: %s (%s)\n", compartmentName, i.CompartmentOCID)
			return nil
		}
	}

	// Create new compartment
	createRequest := identity.CreateCompartmentRequest{
		CreateCompartmentDetails: identity.CreateCompartmentDetails{
			CompartmentId: &i.TenancyOCID,
			Name:          &compartmentName,
			Description:   common.String(fmt.Sprintf("Threeport compartment for %s - all workload clusters will be deployed here", i.RuntimeInstanceName)),
		},
	}

	createResponse, err := client.CreateCompartment(context.Background(), createRequest)
	if err != nil {
		return fmt.Errorf("failed to create compartment: %w", err)
	}

	i.CompartmentOCID = *createResponse.Compartment.Id
	fmt.Printf("Successfully created compartment: %s\n", compartmentName)
	fmt.Printf("All future workload clusters will be deployed in this compartment\n")
	return nil
}

// createOCIServiceUser creates the threeport service user.
func (i *KubernetesRuntimeInfraOKE) createOCIServiceUser(client identity.IdentityClient) error {
	userName := fmt.Sprintf("threeport-service-%s", i.RuntimeInstanceName)
	userEmail := fmt.Sprintf("threeport-service-%s@example.com", i.RuntimeInstanceName)

	// Check if user already exists
	listRequest := identity.ListUsersRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &userName,
	}

	listResponse, err := client.ListUsers(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	// If user exists, use it
	for _, user := range listResponse.Items {
		if *user.Name == userName {
			i.ServiceUserOCID = *user.Id
			fmt.Printf("Using existing service user: %s (%s)\n", userName, i.ServiceUserOCID)
			return nil
		}
	}

	// Create new user
	createRequest := identity.CreateUserRequest{
		CreateUserDetails: identity.CreateUserDetails{
			CompartmentId: &i.TenancyOCID,
			Name:          &userName,
			Description:   common.String(fmt.Sprintf("Threeport service user for %s", i.RuntimeInstanceName)),
			Email:         &userEmail,
		},
	}

	createResponse, err := client.CreateUser(context.Background(), createRequest)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	i.ServiceUserOCID = *createResponse.User.Id
	fmt.Printf("Successfully created service user: %s\n", userName)
	return nil
}

// generateOCIAPIKeyPair generates or loads existing API key pair.
func (i *KubernetesRuntimeInfraOKE) generateOCIAPIKeyPair() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	privateKeyPath := filepath.Join(homeDir, ".oci", fmt.Sprintf("threeport-service-%s.pem", i.RuntimeInstanceName))

	// Check if private key already exists
	if _, err := os.Stat(privateKeyPath); os.IsNotExist(err) {
		// Generate new key pair
		keyPair, err := generateOCIAPIKeyPair()
		if err != nil {
			return fmt.Errorf("failed to generate API key pair: %w", err)
		}

		i.PrivateKeyPEM = keyPair.PrivateKeyPEM
		i.PublicKeyPEM = keyPair.PublicKeyPEM
		i.Fingerprint = keyPair.Fingerprint

		fmt.Printf("Generated new API key pair\n")
	} else {
		// Load existing private key
		privateKeyPEM, err := os.ReadFile(privateKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read existing private key: %w", err)
		}

		// Generate public key from existing private key
		keyPair, err := getAPIKeyPairFromPrivateKey(string(privateKeyPEM))
		if err != nil {
			return fmt.Errorf("failed to generate public key from existing private key: %w", err)
		}

		i.PrivateKeyPEM = keyPair.PrivateKeyPEM
		i.PublicKeyPEM = keyPair.PublicKeyPEM
		i.Fingerprint = keyPair.Fingerprint

		fmt.Printf("Using existing private key: %s\n", privateKeyPath)
	}

	return nil
}

// createOCIAPIKey creates the API key for the service user.
func (i *KubernetesRuntimeInfraOKE) createOCIAPIKey(client identity.IdentityClient) error {
	// Check if API key with this fingerprint already exists
	listRequest := identity.ListApiKeysRequest{
		UserId: &i.ServiceUserOCID,
	}

	listResponse, err := client.ListApiKeys(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list API keys: %w", err)
	}

	// If key with same fingerprint exists, don't create a new one
	for _, key := range listResponse.Items {
		if *key.Fingerprint == i.Fingerprint {
			fmt.Printf("API key with fingerprint %s already exists\n", i.Fingerprint)
			return nil
		}
	}

	// Create new API key
	createRequest := identity.UploadApiKeyRequest{
		UserId: &i.ServiceUserOCID,
		CreateApiKeyDetails: identity.CreateApiKeyDetails{
			Key: &i.PublicKeyPEM,
		},
	}

	_, err = client.UploadApiKey(context.Background(), createRequest)
	if err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}

	fmt.Printf("Successfully created API key with fingerprint: %s\n", i.Fingerprint)
	return nil
}

// createOCIGroup creates the threeport bootstrap group.
func (i *KubernetesRuntimeInfraOKE) createOCIGroup(client identity.IdentityClient) error {
	groupName := fmt.Sprintf("threeport-bootstrap-%s", i.RuntimeInstanceName)
	groupDescription := fmt.Sprintf("Threeport bootstrap group for %s", i.RuntimeInstanceName)

	// Check if group already exists
	listRequest := identity.ListGroupsRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &groupName,
	}

	listResponse, err := client.ListGroups(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	// Check if group exists
	var groupExists bool
	for _, existingGroup := range listResponse.Items {
		if *existingGroup.Name == groupName {
			fmt.Printf("Using existing group: %s (%s)\n", groupName, *existingGroup.Id)
			groupExists = true
			break
		}
	}

	if !groupExists {
		// Create new group
		createRequest := identity.CreateGroupRequest{
			CreateGroupDetails: identity.CreateGroupDetails{
				CompartmentId: &i.TenancyOCID,
				Name:          &groupName,
				Description:   &groupDescription,
			},
		}

		_, err = client.CreateGroup(context.Background(), createRequest)
		if err != nil {
			return fmt.Errorf("failed to create group %s: %v", groupName, err)
		}

		fmt.Printf("Successfully created group: %s\n", groupName)
	}

	return nil
}

// createOCIPolicy creates the threeport policy with compartment creation permissions.
func (i *KubernetesRuntimeInfraOKE) createOCIPolicy(client identity.IdentityClient) error {
	compartmentName := fmt.Sprintf("threeport-%s", i.RuntimeInstanceName)
	bootstrapGroupName := fmt.Sprintf("threeport-bootstrap-%s", i.RuntimeInstanceName)

	policyName := fmt.Sprintf("threeport-bootstrap-policy-%s", i.RuntimeInstanceName)
	policyDescription := fmt.Sprintf("Threeport bootstrap policy for %s", i.RuntimeInstanceName)
	policyStatements := []string{
		// fmt.Sprintf("Allow group %s to manage compartments in tenancy",
		// bootstrapGroupName),

		// fmt.Sprintf("Allow group %s to manage all-resources in tenancy", bootstrapGroupName),
		// fmt.Sprintf("Allow group %s to manage all-resources in compartment
		// %s", bootstrapGroupName, compartmentName),

		// https://docs.public.content.oci.oraclecloud.com/en-us/iaas/compute-cloud-at-customer/topics/oke/create-a-user-group-and-policies-that-authorize-members-to-use-oke.htm#create-a-user-group-and-policies-that-authorize-members-to-use-oke
		fmt.Sprintf("Allow group %s to read all-resources in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage cluster-family in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage instance-family in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage network-load-balancers in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage virtual-network-family in compartment %s", bootstrapGroupName, compartmentName),


		// additional policies
		fmt.Sprintf("Allow group %s to inspect compartments in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage volume-family in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage load-balancers in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to use vnics in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to use network-security-groups in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to use private-ips in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage public-ips in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage object-family in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage tag-namespaces in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage tag-defaults in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to use tag-namespaces in compartment %s", bootstrapGroupName, compartmentName),
		fmt.Sprintf("Allow group %s to use subnets in compartment %s", bootstrapGroupName, compartmentName),
	}

	// Check if policy already exists
	listRequest := identity.ListPoliciesRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &policyName,
	}

	listResponse, err := client.ListPolicies(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list policies: %w", err)
	}

	// Check if policy exists
	var policyExists bool
	for _, existingPolicy := range listResponse.Items {
		if *existingPolicy.Name == policyName {
			fmt.Printf("Using existing policy: %s (%s)\n", policyName, *existingPolicy.Id)
			policyExists = true
			break
		}
	}

	if !policyExists {
		// Create new policy
		createRequest := identity.CreatePolicyRequest{
			CreatePolicyDetails: identity.CreatePolicyDetails{
				CompartmentId: &i.TenancyOCID,
				Name:          &policyName,
				Description:   &policyDescription,
				Statements:    policyStatements,
			},
		}

		_, err = client.CreatePolicy(context.Background(), createRequest)
		if err != nil {
			return fmt.Errorf("failed to create policy %s: %v", policyName, err)
		}

		fmt.Printf("Successfully created policy: %s\n", policyName)
	}

	return nil
}

// addOCIUserToGroup adds the service user to the threeport bootstrap group.
func (i *KubernetesRuntimeInfraOKE) addOCIUserToGroup(client identity.IdentityClient) error {
	groupName := fmt.Sprintf("threeport-bootstrap-%s", i.RuntimeInstanceName)

	// Get group ID
	listRequest := identity.ListGroupsRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &groupName,
	}

	listResponse, err := client.ListGroups(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	if len(listResponse.Items) == 0 {
		return fmt.Errorf("group %s not found", groupName)
	}

	groupID := *listResponse.Items[0].Id

	// Check if user is already in group
	membersRequest := identity.ListUserGroupMembershipsRequest{
		CompartmentId: &i.TenancyOCID,
		UserId:        &i.ServiceUserOCID,
		GroupId:       &groupID,
	}

	membersResponse, err := client.ListUserGroupMemberships(context.Background(), membersRequest)
	if err != nil {
		return fmt.Errorf("failed to list group memberships: %w", err)
	}

	// If user is already in group, skip
	if len(membersResponse.Items) > 0 {
		fmt.Printf("User already in group: %s\n", groupName)
		return nil
	}

	// Add user to group
	addRequest := identity.AddUserToGroupRequest{
		AddUserToGroupDetails: identity.AddUserToGroupDetails{
			UserId:  &i.ServiceUserOCID,
			GroupId: &groupID,
		},
	}

	_, err = client.AddUserToGroup(context.Background(), addRequest)
	if err != nil {
		return fmt.Errorf("failed to add user to group %s: %v", groupName, err)
	}

	fmt.Printf("Successfully added user to group: %s\n", groupName)
	return nil
}

// writeOCIConfiguration writes the OCI configuration files.
func (i *KubernetesRuntimeInfraOKE) writeOCIConfiguration() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	// Write private key file
	privateKeyPath := filepath.Join(homeDir, ".oci", fmt.Sprintf("threeport-service-%s.pem", i.RuntimeInstanceName))
	if _, err := os.Stat(privateKeyPath); os.IsNotExist(err) {
		if err := os.WriteFile(privateKeyPath, []byte(i.PrivateKeyPEM), 0600); err != nil {
			return fmt.Errorf("failed to write private key file: %w", err)
		}
		fmt.Printf("Successfully created private key: %s\n", privateKeyPath)
	} else {
		fmt.Printf("Private key already exists, not overwriting: %s\n", privateKeyPath)
	}

	// Update OCI config file
	configPath := filepath.Join(homeDir, ".oci", "config")
	configContent := fmt.Sprintf(`[THREEPORT_SERVICE]
user=%s
fingerprint=%s
tenancy=%s
region=%s
key_file=%s
`,
		i.ServiceUserOCID,
		i.Fingerprint,
		i.TenancyOCID,
		i.Region,
		privateKeyPath,
	)

	// If config file doesn't exist, create it
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
			return fmt.Errorf("failed to create OCI config: %w", err)
		}
	} else {
		// File exists, update THREEPORT_SERVICE section
		existingContent, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read existing OCI config: %w", err)
		}

		// Remove existing THREEPORT_SERVICE section
		lines := strings.Split(string(existingContent), "\n")
		var newLines []string
		skipSection := false
		for _, line := range lines {
			if strings.TrimSpace(line) == "[THREEPORT_SERVICE]" {
				skipSection = true
				continue
			}
			if skipSection && strings.HasPrefix(strings.TrimSpace(line), "[") {
				skipSection = false
			}
			if !skipSection {
				newLines = append(newLines, line)
			}
		}

		// Append new section
		newContent := strings.Join(newLines, "\n") + "\n" + configContent

		// Write back to file
		if err := os.WriteFile(configPath, []byte(newContent), 0600); err != nil {
			return fmt.Errorf("failed to update OCI config: %w", err)
		}
	}

	fmt.Printf("Successfully updated OCI config: %s\n", configPath)
	return nil
}

// ServiceStatus represents the current status of a service propagation check
type ServiceStatus struct {
	Name                 string
	ConsecutiveSuccesses int
	Attempts             int
	LastError            error
	Completed            bool
	Failed               bool
}

// validateOCIUserPropagation validates that the service user credentials are propagated across all OCI services.
func (i *KubernetesRuntimeInfraOKE) validateOCIUserPropagation() error {
	// Create a raw configuration provider with the service user credentials
	configProvider := common.NewRawConfigurationProvider(
		i.TenancyOCID,
		i.ServiceUserOCID,
		i.Region,
		i.Fingerprint,
		i.PrivateKeyPEM,
		nil,
	)
	const requiredConsecutiveSuccesses = 1
	const maxAttempts = 450
	const retryDelay = 2 * time.Second

	services := []struct {
		id   string
		name string
		test func() error
	}{
		{
			id:   "identity",
			name: "Identity service",
			test: func() error {
				identityClient, err := identity.NewIdentityClientWithConfigurationProvider(configProvider)
				if err != nil {
					return fmt.Errorf("failed to create identity client: %w", err)
				}
				getCompartmentRequest := identity.GetCompartmentRequest{
					CompartmentId: &i.CompartmentOCID,
				}
				_, err = identityClient.GetCompartment(context.Background(), getCompartmentRequest)
				return err
			},
		},
		{
			id:   "core",
			name: "Core service",
			test: func() error {
				coreClient, err := ocicore.NewVirtualNetworkClientWithConfigurationProvider(configProvider)
				if err != nil {
					return fmt.Errorf("failed to create core client: %w", err)
				}

				// Test VCN access
				vcnRequest := ocicore.ListVcnsRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListVcns(context.Background(), vcnRequest)
				if err != nil {
					return fmt.Errorf("VCN access failed: %w", err)
				}

				// Test Internet Gateway access
				igwRequest := ocicore.ListInternetGatewaysRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListInternetGateways(context.Background(), igwRequest)
				if err != nil {
					return fmt.Errorf("Internet Gateway access failed: %w", err)
				}

				// Test NAT Gateway access
				natRequest := ocicore.ListNatGatewaysRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListNatGateways(context.Background(), natRequest)
				if err != nil {
					return fmt.Errorf("NAT Gateway access failed: %w", err)
				}

				// Test Service Gateway access
				sgwRequest := ocicore.ListServiceGatewaysRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListServiceGateways(context.Background(), sgwRequest)
				if err != nil {
					return fmt.Errorf("Service Gateway access failed: %w", err)
				}

				// Test Security Lists access
				secListRequest := ocicore.ListSecurityListsRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListSecurityLists(context.Background(), secListRequest)
				if err != nil {
					return fmt.Errorf("Security Lists access failed: %w", err)
				}

				// Test Subnets access
				subnetRequest := ocicore.ListSubnetsRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListSubnets(context.Background(), subnetRequest)
				if err != nil {
					return fmt.Errorf("Subnets access failed: %w", err)
				}

				return nil
			},
		},
		{
			id:   "container-engine",
			name: "Container Engine service",
			test: func() error {
				ceClient, err := ocicontainerengine.NewContainerEngineClientWithConfigurationProvider(configProvider)
				if err != nil {
					return fmt.Errorf("failed to create container engine client: %w", err)
				}
				ceRequest := ocicontainerengine.ListClustersRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = ceClient.ListClusters(context.Background(), ceRequest)
				return err
			},
		},
	}

	// Initialize status map
	statusMap := make(map[string]*ServiceStatus)
	for _, service := range services {
		statusMap[service.id] = &ServiceStatus{
			Name: service.name,
		}
	}

	// Channel for status updates
	statusChan := make(chan struct {
		serviceID string
		status    ServiceStatus
	}, 100)

	// Start all services in parallel
	var wg sync.WaitGroup
	for _, service := range services {
		wg.Add(1)
		go func(svc struct {
			id   string
			name string
			test func() error
		}) {
			defer wg.Done()

			consecutiveSuccesses := 0
			attempts := 0

			for consecutiveSuccesses < requiredConsecutiveSuccesses && attempts < maxAttempts {
				attempts++

				err := svc.test()
				if err != nil {
					consecutiveSuccesses = 0 // Reset on failure
					// Simplify error display to just show HTTP status code
					var displayError error
					if strings.Contains(err.Error(), "Http Status Code:") {
						// Extract just the HTTP status code
						parts := strings.Split(err.Error(), "Http Status Code: ")
						if len(parts) > 1 {
							codePart := strings.Split(parts[1], ".")[0]
							displayError = fmt.Errorf("HTTP %s", codePart)
						} else {
							displayError = fmt.Errorf("Auth failed")
						}
					} else {
						displayError = fmt.Errorf("Auth failed: %w", err)
					}

					statusChan <- struct {
						serviceID string
						status    ServiceStatus
					}{
						serviceID: svc.id,
						status: ServiceStatus{
							Name:                 svc.name,
							ConsecutiveSuccesses: consecutiveSuccesses,
							Attempts:             attempts,
							LastError:            displayError,
							Completed:            false,
							Failed:               false,
						},
					}
					time.Sleep(retryDelay)
				} else {
					consecutiveSuccesses++
					completed := consecutiveSuccesses >= requiredConsecutiveSuccesses
					statusChan <- struct {
						serviceID string
						status    ServiceStatus
					}{
						serviceID: svc.id,
						status: ServiceStatus{
							Name:                 svc.name,
							ConsecutiveSuccesses: consecutiveSuccesses,
							Attempts:             attempts,
							LastError:            nil,
							Completed:            completed,
							Failed:               false,
						},
					}
					if !completed {
						time.Sleep(1 * time.Second) // Small delay between successful attempts
					}
				}
			}

			// Mark as failed if max attempts reached
			if consecutiveSuccesses < requiredConsecutiveSuccesses {
				statusChan <- struct {
					serviceID string
					status    ServiceStatus
				}{
					serviceID: svc.id,
					status: ServiceStatus{
						Name:                 svc.name,
						ConsecutiveSuccesses: consecutiveSuccesses,
						Attempts:             attempts,
						LastError:            fmt.Errorf("max attempts reached"),
						Completed:            false,
						Failed:               true,
					},
				}
			}
		}(service)
	}

	// Close channel when all goroutines are done
	go func() {
		wg.Wait()
		close(statusChan)
	}()

	// Display parallel status updates
	fmt.Printf("Validating service propagation\n")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Track last displayed status to avoid duplicate outputs
	lastDisplayTime := time.Now()
	lastStatuses := make(map[string]ServiceStatus)
	displayInitialized := false

	displayStatus := func() {
		// Only display if there's been a meaningful change or enough time has passed
		hasChanged := false
		for id, status := range statusMap {
			if lastStatus, exists := lastStatuses[id]; !exists ||
				lastStatus.Completed != status.Completed ||
				lastStatus.Failed != status.Failed {
				hasChanged = true
				break
			}
		}

		if !hasChanged && time.Since(lastDisplayTime) < 2*time.Second {
			return
		}

		if !displayInitialized {
			// First time - print the initial lines
			for _, service := range services {
				status := statusMap[service.id]
				if status.Completed {
					fmt.Printf("%s... synced\n", status.Name)
				} else if status.Failed {
					fmt.Printf("%s... failed\n", status.Name)
				} else {
					fmt.Printf("%s... waiting\n", status.Name)
				}
				lastStatuses[service.id] = *status
			}
			displayInitialized = true
		} else {
			// Move cursor up to overwrite previous lines
			fmt.Printf("\033[%dA", len(services))
			for _, service := range services {
				status := statusMap[service.id]
				if status.Completed {
					fmt.Printf("\033[K%s... synced\n", status.Name)
				} else if status.Failed {
					fmt.Printf("\033[K%s... failed\n", status.Name)
				} else {
					fmt.Printf("\033[K%s... waiting\n", status.Name)
				}
				lastStatuses[service.id] = *status
			}
		}
		lastDisplayTime = time.Now()
	}

	// Main status update loop
	for {
		select {
		case update, ok := <-statusChan:
			if !ok {
				// Channel closed, all done
				displayStatus() // Final display

				// Check for failures
				for _, status := range statusMap {
					if status.Failed {
						return fmt.Errorf("%s failed to propagate", status.Name)
					}
				}

				fmt.Printf("All services propagated successfully\n")
				return nil
			}
			statusMap[update.serviceID] = &update.status
			displayStatus()

		case <-ticker.C:
			displayStatus()
		}
	}
}

// OCIAPIKeyPair represents an API key pair for OCI authentication
type OCIAPIKeyPair struct {
	PrivateKeyPEM string
	PublicKeyPEM  string
	Fingerprint   string
}

// generateOCIAPIKeyPair generates a new API key pair for OCI authentication.
func generateOCIAPIKeyPair() (*OCIAPIKeyPair, error) {
	// Generate RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// Convert private key to PEM format
	privateKeyPKCS1 := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyPKCS1,
	})

	// Convert public key to PEM format
	publicKeyPKCS1, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyPKCS1,
	})

	// Calculate fingerprint
	fingerprint := calculateKeyFingerprint(string(publicKeyPEM))

	return &OCIAPIKeyPair{
		PrivateKeyPEM: string(privateKeyPEM),
		PublicKeyPEM:  string(publicKeyPEM),
		Fingerprint:   fingerprint,
	}, nil
}

// getAPIKeyPairFromPrivateKey extracts API key pair details from an existing private key PEM.
func getAPIKeyPairFromPrivateKey(privateKeyPEM string) (*OCIAPIKeyPair, error) {
	// Parse the private key
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Convert public key to PEM format
	publicKeyPKCS1, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyPKCS1,
	})

	// Calculate fingerprint
	fingerprint := calculateKeyFingerprint(string(publicKeyPEM))

	return &OCIAPIKeyPair{
		PrivateKeyPEM: privateKeyPEM,
		PublicKeyPEM:  string(publicKeyPEM),
		Fingerprint:   fingerprint,
	}, nil
}

// calculateKeyFingerprint calculates the MD5 fingerprint of a public key in PEM format.
func calculateKeyFingerprint(publicKeyPEM string) string {
	// Remove PEM headers and footers, and whitespace
	lines := strings.Split(publicKeyPEM, "\n")
	var keyData strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-----") && line != "" {
			keyData.WriteString(line)
		}
	}

	// Decode base64 key data
	keyBytes, err := base64.StdEncoding.DecodeString(keyData.String())
	if err != nil {
		// Fallback: use the raw key data if base64 decoding fails
		keyBytes = []byte(keyData.String())
	}

	// Calculate MD5 hash
	hash := md5.Sum(keyBytes)

	// Format as colon-separated hex pairs
	fingerprint := make([]string, len(hash))
	for i, b := range hash {
		fingerprint[i] = fmt.Sprintf("%02x", b)
	}

	return strings.Join(fingerprint, ":")
}
