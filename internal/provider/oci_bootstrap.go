package provider

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

// OCI resource naming format strings. These constants ensure create/delete
// pairs always reference the same resource names.
const (
	ociConfigSectionFormat = "[%s-service]"
	serviceUserNameFormat  = "%s-user"
	serviceUserEmailFormat = "%s-user@threeport.io"
	groupNameFormat        = "%s-group"
	policyNameFormat       = "%s-policy"
	privateKeyFileFormat   = "%s-private-key.pem"
)

// OCIAPIKeyPair represents an API key pair for OCI authentication
type OCIAPIKeyPair struct {
	PrivateKeyPEM string
	PublicKeyPEM  string
	Fingerprint   string
}

// getTenancyOCID returns the tenancy OCID from the config provider.
func (i *KubernetesRuntimeInfraOKE) getTenancyOCID() (string, error) {
	tenancyOCID, err := i.ConfigProvider.TenancyOCID()
	if err != nil {
		return "", fmt.Errorf("failed to get tenancy OCID from config provider: %w", err)
	}
	return tenancyOCID, nil
}

// getOCIConfigSectionName returns the standardized OCI config section name for the service user.
func (i *KubernetesRuntimeInfraOKE) getOCIConfigSectionName() string {
	return fmt.Sprintf(ociConfigSectionFormat, i.RuntimeInstanceName)
}

// GetServiceUserName returns the standardized service user name for this threeport instance.
func (i *KubernetesRuntimeInfraOKE) GetServiceUserName() string {
	return fmt.Sprintf(serviceUserNameFormat, i.RuntimeInstanceName)
}

// getServiceUserEmail returns the email for the service user.
func (i *KubernetesRuntimeInfraOKE) getServiceUserEmail() string {
	return fmt.Sprintf(serviceUserEmailFormat, i.RuntimeInstanceName)
}

// getGroupName returns the standardized group name for this threeport instance.
func (i *KubernetesRuntimeInfraOKE) getGroupName() string {
	return fmt.Sprintf(groupNameFormat, i.RuntimeInstanceName)
}

// getPolicyName returns the standardized policy name for this threeport instance.
func (i *KubernetesRuntimeInfraOKE) getPolicyName() string {
	return fmt.Sprintf(policyNameFormat, i.RuntimeInstanceName)
}

// getPrivateKeyFilename returns the private key filename for this threeport instance.
func (i *KubernetesRuntimeInfraOKE) getPrivateKeyFilename() string {
	return fmt.Sprintf(privateKeyFileFormat, i.RuntimeInstanceName)
}

// OCI Compartment Operations

// createOCICompartment creates a new compartment for the threeport instance.
// The compartment is created as a child of the current CompartmentOCID (the parent).
// After creation, CompartmentOCID is overwritten with the child compartment OCID.
func (i *KubernetesRuntimeInfraOKE) createOCICompartment(client identity.IdentityClient) error {
	compartmentDescription := fmt.Sprintf("Compartment for threeport instance %s", i.RuntimeInstanceName)
	parentCompartmentOCID := i.CompartmentOCID

	// check if CompartmentOCID already points to the target compartment
	// (e.g., on retry after buildOkeInfra resolved it)
	getResponse, err := client.GetCompartment(context.Background(), identity.GetCompartmentRequest{
		CompartmentId: &parentCompartmentOCID,
	})
	if err == nil && getResponse.Name != nil && *getResponse.Name == i.RuntimeInstanceName {
		i.logInfo(fmt.Sprintf("using existing compartment: %s", i.RuntimeInstanceName))
		return nil
	}

	// check if compartment already exists under parent
	listRequest := identity.ListCompartmentsRequest{
		CompartmentId: &parentCompartmentOCID,
		Name:          &i.RuntimeInstanceName,
	}

	response, err := client.ListCompartments(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list compartments: %w", err)
	}

	if len(response.Items) > 0 {
		i.CompartmentOCID = *response.Items[0].Id
		i.logInfo(fmt.Sprintf("using existing compartment: %s", i.RuntimeInstanceName))
		return nil
	}

	// create new compartment under parent
	i.logInfo(fmt.Sprintf("creating compartment: %s", i.RuntimeInstanceName))
	createRequest := identity.CreateCompartmentRequest{
		CreateCompartmentDetails: identity.CreateCompartmentDetails{
			CompartmentId: &parentCompartmentOCID,
			Name:          &i.RuntimeInstanceName,
			Description:   &compartmentDescription,
		},
	}

	createResponse, err := client.CreateCompartment(context.Background(), createRequest)
	if err != nil {
		return fmt.Errorf("failed to create compartment: %w", err)
	}

	i.CompartmentOCID = *createResponse.Compartment.Id
	i.logInfo(fmt.Sprintf("successfully created compartment: %s", i.RuntimeInstanceName))
	return nil
}

// deleteOCICompartment deletes the OCI compartment referenced by CompartmentOCID.
func (i *KubernetesRuntimeInfraOKE) deleteOCICompartment(client identity.IdentityClient) error {
	if i.CompartmentOCID == "" {
		i.logInfo("compartment OCID not set, skipping deletion")
		return nil
	}

	// verify compartment exists before deleting
	getRequest := identity.GetCompartmentRequest{
		CompartmentId: &i.CompartmentOCID,
	}
	getResponse, err := client.GetCompartment(context.Background(), getRequest)
	if err != nil {
		return fmt.Errorf("failed to get compartment: %w", err)
	}

	// skip if already deleted
	if getResponse.LifecycleState == identity.CompartmentLifecycleStateDeleted {
		i.logInfo(fmt.Sprintf("compartment %s already deleted", i.RuntimeInstanceName))
		return nil
	}

	// delete the compartment
	deleteRequest := identity.DeleteCompartmentRequest{
		CompartmentId: &i.CompartmentOCID,
	}

	_, err = client.DeleteCompartment(context.Background(), deleteRequest)
	if err != nil {
		// provide actionable guidance for non-empty compartments
		if strings.Contains(err.Error(), "BulkDeleteResource") ||
			strings.Contains(err.Error(), "409") {
			return fmt.Errorf(
				"compartment %s still contains resources — delete all resources first or wait for async cleanup: %w",
				i.RuntimeInstanceName, err,
			)
		}
		return fmt.Errorf("failed to delete compartment: %w", err)
	}

	i.logInfo("compartment deletion initiated (may take time to complete)")
	return nil
}

// OCI User Operations

// createOCIServiceUser creates the threeport service user.
func (i *KubernetesRuntimeInfraOKE) createOCIServiceUser(client identity.IdentityClient) error {
	tenancyOCID, err := i.getTenancyOCID()
	if err != nil {
		return err
	}

	userName := i.GetServiceUserName()
	userEmail := i.getServiceUserEmail()

	// check if user already exists
	listRequest := identity.ListUsersRequest{
		CompartmentId: &tenancyOCID,
		Name:          &userName,
	}

	response, err := client.ListUsers(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	if len(response.Items) > 0 {
		i.ServiceUserOCID = *response.Items[0].Id
		fmt.Printf("Using existing user: %s\n", userName)
		return nil
	}

	// create new user
	fmt.Printf("Creating user: %s\n", userName)
	createRequest := identity.CreateUserRequest{
		CreateUserDetails: identity.CreateUserDetails{
			CompartmentId: &tenancyOCID,
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
	fmt.Printf("Successfully created user: %s\n", userName)
	return nil
}

// deleteOCIUser deletes an OCI user by name.
func (i *KubernetesRuntimeInfraOKE) deleteOCIUser(client identity.IdentityClient) error {
	tenancyOCID, err := i.getTenancyOCID()
	if err != nil {
		return err
	}

	userName := i.GetServiceUserName()
	// list users to find the one to delete
	listRequest := identity.ListUsersRequest{
		CompartmentId: &tenancyOCID,
		Name:          &userName,
	}

	response, err := client.ListUsers(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	if len(response.Items) == 0 {
		i.logInfo(fmt.Sprintf("user %s not found, skipping deletion", userName))
		return nil
	}

	userOCID := *response.Items[0].Id

	// delete all API keys for the user first
	listKeysRequest := identity.ListApiKeysRequest{
		UserId: &userOCID,
	}

	keysResponse, err := client.ListApiKeys(context.Background(), listKeysRequest)
	if err != nil {
		return fmt.Errorf("failed to list API keys: %w", err)
	}

	for _, apiKey := range keysResponse.Items {
		deleteKeyRequest := identity.DeleteApiKeyRequest{
			UserId:      &userOCID,
			Fingerprint: apiKey.Fingerprint,
		}

		_, err = client.DeleteApiKey(context.Background(), deleteKeyRequest)
		if err != nil {
			i.logError(err, "failed to delete API key")
		}
	}

	// delete the user
	deleteRequest := identity.DeleteUserRequest{
		UserId: &userOCID,
	}

	_, err = client.DeleteUser(context.Background(), deleteRequest)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// OCI Group Operations

// createOCIGroup creates a new OCI group for threeport operations.
func (i *KubernetesRuntimeInfraOKE) createOCIGroup(client identity.IdentityClient) error {
	tenancyOCID, err := i.getTenancyOCID()
	if err != nil {
		return err
	}

	groupName := i.getGroupName()
	groupDescription := fmt.Sprintf("Bootstrap group for threeport instance %s", i.RuntimeInstanceName)

	// check if group already exists
	listRequest := identity.ListGroupsRequest{
		CompartmentId: &tenancyOCID,
		Name:          &groupName,
	}

	response, err := client.ListGroups(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	if len(response.Items) > 0 {
		fmt.Printf("Using existing group: %s\n", groupName)
		return nil
	}

	// create new group
	fmt.Printf("Creating group: %s\n", groupName)
	createRequest := identity.CreateGroupRequest{
		CreateGroupDetails: identity.CreateGroupDetails{
			CompartmentId: &tenancyOCID,
			Name:          &groupName,
			Description:   &groupDescription,
		},
	}

	_, err = client.CreateGroup(context.Background(), createRequest)
	if err != nil {
		return fmt.Errorf("failed to create group: %w", err)
	}

	fmt.Printf("Successfully created group: %s\n", groupName)
	return nil
}

// addOCIUserToGroup adds a user to an OCI group.
func (i *KubernetesRuntimeInfraOKE) addOCIUserToGroup(client identity.IdentityClient) error {
	tenancyOCID, err := i.getTenancyOCID()
	if err != nil {
		return err
	}

	groupName := i.getGroupName()

	// get group OCID
	listRequest := identity.ListGroupsRequest{
		CompartmentId: &tenancyOCID,
		Name:          &groupName,
	}

	response, err := client.ListGroups(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	if len(response.Items) == 0 {
		return fmt.Errorf("group %s not found", groupName)
	}

	groupOCID := *response.Items[0].Id

	fmt.Printf("Adding user to group\n")

	// check if user is already in the group
	listMembershipsRequest := identity.ListUserGroupMembershipsRequest{
		CompartmentId: &tenancyOCID,
		UserId:        &i.ServiceUserOCID,
		GroupId:       &groupOCID,
	}

	membershipsResponse, err := client.ListUserGroupMemberships(context.Background(), listMembershipsRequest)
	if err != nil {
		return fmt.Errorf("failed to list user group memberships: %w", err)
	}

	if len(membershipsResponse.Items) > 0 {
		fmt.Printf("User already in group\n")
		return nil
	}

	// add user to group
	addRequest := identity.AddUserToGroupRequest{
		AddUserToGroupDetails: identity.AddUserToGroupDetails{
			UserId:  &i.ServiceUserOCID,
			GroupId: &groupOCID,
		},
	}

	_, err = client.AddUserToGroup(context.Background(), addRequest)
	if err != nil {
		return fmt.Errorf("failed to add user to group: %w", err)
	}

	fmt.Printf("Successfully added user to group\n")
	return nil
}

// deleteOCIGroup deletes an OCI group by name.
func (i *KubernetesRuntimeInfraOKE) deleteOCIGroup(client identity.IdentityClient) error {
	tenancyOCID, err := i.getTenancyOCID()
	if err != nil {
		return err
	}

	groupName := i.getGroupName()
	// list groups to find the one to delete
	listRequest := identity.ListGroupsRequest{
		CompartmentId: &tenancyOCID,
		Name:          &groupName,
	}

	response, err := client.ListGroups(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	if len(response.Items) == 0 {
		i.logInfo(fmt.Sprintf("group %s not found, skipping deletion", groupName))
		return nil
	}

	// delete the group
	deleteRequest := identity.DeleteGroupRequest{
		GroupId: response.Items[0].Id,
	}

	_, err = client.DeleteGroup(context.Background(), deleteRequest)
	if err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}

	return nil
}

// OCI Policy Operations

// createOCIPolicy creates a new OCI policy for threeport operations.
// Policies are scoped to the genesis compartment so the service user can manage
// child compartments and their resources without tenancy-wide access.
func (i *KubernetesRuntimeInfraOKE) createOCIPolicy(client identity.IdentityClient) error {
	tenancyOCID, err := i.getTenancyOCID()
	if err != nil {
		return err
	}

	groupName := i.getGroupName()
	policyName := i.getPolicyName()
	policyDescription := fmt.Sprintf("Threeport bootstrap policy for %s", i.RuntimeInstanceName)
	compartmentName := i.RuntimeInstanceName
	policyStatements := []string{
		// scope all permissions to the genesis compartment — child compartments
		// automatically inherit these policies
		fmt.Sprintf("Allow group %s to manage compartments in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to read all-resources in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage cluster-family in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage instance-family in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage network-load-balancers in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage virtual-network-family in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage volume-family in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage load-balancers in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to use vnics in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to use network-security-groups in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to use private-ips in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage public-ips in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage object-family in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage tag-namespaces in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to manage tag-defaults in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to use tag-namespaces in compartment %s", groupName, compartmentName),
		fmt.Sprintf("Allow group %s to use subnets in compartment %s", groupName, compartmentName),
	}

	// check if policy already exists
	listRequest := identity.ListPoliciesRequest{
		CompartmentId: &tenancyOCID,
		Name:          &policyName,
	}

	response, err := client.ListPolicies(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list policies: %w", err)
	}

	if len(response.Items) > 0 {
		fmt.Printf("Using existing policy: %s\n", policyName)
		return nil
	}

	// create new policy at tenancy level (policies must be created in tenancy)
	fmt.Printf("Creating policy: %s\n", policyName)
	createRequest := identity.CreatePolicyRequest{
		CreatePolicyDetails: identity.CreatePolicyDetails{
			CompartmentId: &tenancyOCID,
			Name:          &policyName,
			Description:   &policyDescription,
			Statements:    policyStatements,
		},
	}

	_, err = client.CreatePolicy(context.Background(), createRequest)
	if err != nil {
		return fmt.Errorf("failed to create policy: %w", err)
	}

	fmt.Printf("Successfully created policy: %s\n", policyName)
	return nil
}

// deleteOCIPolicy deletes an OCI policy by name.
func (i *KubernetesRuntimeInfraOKE) deleteOCIPolicy(client identity.IdentityClient) error {
	tenancyOCID, err := i.getTenancyOCID()
	if err != nil {
		return err
	}

	policyName := i.getPolicyName()
	// list policies to find the one to delete
	listRequest := identity.ListPoliciesRequest{
		CompartmentId: &tenancyOCID,
		Name:          &policyName,
	}

	response, err := client.ListPolicies(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list policies: %w", err)
	}

	if len(response.Items) == 0 {
		i.logInfo(fmt.Sprintf("policy %s not found, skipping deletion", policyName))
		return nil
	}

	// delete the policy
	deleteRequest := identity.DeletePolicyRequest{
		PolicyId: response.Items[0].Id,
	}

	_, err = client.DeletePolicy(context.Background(), deleteRequest)
	if err != nil {
		return fmt.Errorf("failed to delete policy: %w", err)
	}

	return nil
}

// OCI API Key Operations

// generateOCIAPIKeyPair generates or loads existing API key pair for API authentication.
func (i *KubernetesRuntimeInfraOKE) generateOCIAPIKeyPair() error {
	fmt.Printf("Setting up API key pair\n")

	// check if private key already exists on disk
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	privateKeyPath := filepath.Join(homeDir, ".oci", i.getPrivateKeyFilename())

	// try to load existing key from disk
	if _, err := os.Stat(privateKeyPath); err == nil {
		fmt.Printf("Loading existing API key from disk\n")
		privateKeyBytes, err := os.ReadFile(privateKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read existing private key: %w", err)
		}

		// extract key pair details from existing private key
		keyPair, err := getAPIKeyPairFromPrivateKey(string(privateKeyBytes))
		if err != nil {
			fmt.Printf("Warning: failed to parse existing key, generating new one: %v\n", err)
			// fall through to generate new key
		} else {
			i.PrivateKeyPEM = keyPair.PrivateKeyPEM
			i.PublicKeyPEM = keyPair.PublicKeyPEM
			i.Fingerprint = keyPair.Fingerprint
			fmt.Printf("Successfully loaded existing API key pair\n")
			return nil
		}
	}

	// generate new key pair if none exists or existing key is invalid
	fmt.Printf("Generating new API key pair\n")
	keyPair, err := generateOCIAPIKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate RSA key pair: %w", err)
	}

	i.PrivateKeyPEM = keyPair.PrivateKeyPEM
	i.PublicKeyPEM = keyPair.PublicKeyPEM
	i.Fingerprint = keyPair.Fingerprint

	fmt.Printf("Successfully generated new API key pair\n")
	return nil
}

// createOCIAPIKey creates a new API key for the service user.
func (i *KubernetesRuntimeInfraOKE) createOCIAPIKey(client identity.IdentityClient) error {
	fmt.Printf("Creating API key for user\n")

	// check if API key already exists
	listRequest := identity.ListApiKeysRequest{
		UserId: &i.ServiceUserOCID,
	}

	response, err := client.ListApiKeys(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list API keys: %w", err)
	}

	// check if our key already exists
	for _, apiKey := range response.Items {
		if *apiKey.Fingerprint == i.Fingerprint {
			fmt.Printf("API key already exists\n")
			return nil
		}
	}

	// create new API key
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

	fmt.Printf("Successfully created API key\n")
	return nil
}

// OCI Configuration Operations

// writeOCIConfiguration writes the OCI configuration file for the service user.
func (i *KubernetesRuntimeInfraOKE) writeOCIConfiguration() error {
	tenancyOCID, err := i.getTenancyOCID()
	if err != nil {
		return err
	}

	fmt.Printf("Writing OCI configuration\n")

	config := fmt.Sprintf(`
%s
user=%s
fingerprint=%s
tenancy=%s
region=%s
key_file=%s
`,
		i.getOCIConfigSectionName(),
		i.ServiceUserOCID,
		i.Fingerprint,
		tenancyOCID,
		i.Region,
		filepath.Join("~/.oci", i.getPrivateKeyFilename()),
	)

	// write configuration to OCI config file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := fmt.Sprintf("%s/.oci/config", homeDir)
	privateKeyPath := filepath.Join(homeDir, ".oci", i.getPrivateKeyFilename())

	// ensure .oci directory exists
	ociDir := fmt.Sprintf("%s/.oci", homeDir)
	if err := os.MkdirAll(ociDir, 0700); err != nil {
		return fmt.Errorf("failed to create .oci directory: %w", err)
	}

	// check if configuration section already exists
	sectionName := i.getOCIConfigSectionName()
	if _, err := os.Stat(configPath); err == nil {
		// config file exists, check if our section is already there
		configBytes, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("failed to read existing config: %w", err)
		}

		if strings.Contains(string(configBytes), sectionName) {
			fmt.Printf("Configuration section already exists\n")
			// still write the private key file in case it's missing
			if err := os.WriteFile(privateKeyPath, []byte(i.PrivateKeyPEM), 0600); err != nil {
				return fmt.Errorf("failed to write private key: %w", err)
			}
			return nil
		}
	}

	// append to config file
	configFile, err := os.OpenFile(configPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer configFile.Close()

	if _, err := configFile.WriteString(config); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// write private key file
	if err := os.WriteFile(privateKeyPath, []byte(i.PrivateKeyPEM), 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	fmt.Printf("Successfully wrote OCI configuration\n")
	return nil
}

// deleteOCIConfiguration removes the OCI configuration section and private key file for the service user.
func (i *KubernetesRuntimeInfraOKE) deleteOCIConfiguration() error {
	i.logInfo("cleaning up OCI configuration")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := fmt.Sprintf("%s/.oci/config", homeDir)
	privateKeyPath := filepath.Join(homeDir, ".oci", i.getPrivateKeyFilename())

	// remove private key file
	if err := os.Remove(privateKeyPath); err != nil && !os.IsNotExist(err) {
		i.logError(err, "failed to remove private key file")
	}

	// remove configuration section from config file
	sectionName := i.getOCIConfigSectionName()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// config file doesn't exist, nothing to clean up
		return nil
	}

	// read existing config file
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	configContent := string(configBytes)
	if !strings.Contains(configContent, sectionName) {
		// section doesn't exist, nothing to clean up
		return nil
	}

	// remove the section and its content
	lines := strings.Split(configContent, "\n")
	var filteredLines []string
	inTargetSection := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// check if we're entering our target section
		if trimmedLine == sectionName {
			inTargetSection = true
			continue
		}

		// check if we're entering a different section (exit our target section)
		if strings.HasPrefix(trimmedLine, "[") && strings.HasSuffix(trimmedLine, "]") && trimmedLine != sectionName {
			inTargetSection = false
		}

		// skip lines that belong to our target section
		if inTargetSection {
			continue
		}

		filteredLines = append(filteredLines, line)
	}

	// write the filtered content back to the file
	filteredContent := strings.Join(filteredLines, "\n")
	if err := os.WriteFile(configPath, []byte(filteredContent), 0600); err != nil {
		return fmt.Errorf("failed to write updated config file: %w", err)
	}

	i.logInfo("successfully cleaned up OCI configuration")
	return nil
}

// Crypto Helper Functions

// generateOCIAPIKeyPair generates a new RSA key pair for OCI API authentication.
func generateOCIAPIKeyPair() (*OCIAPIKeyPair, error) {
	// generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// encode private key to PEM format
	privateKeyDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyDER,
	})

	// extract public key and encode to PEM format
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyDER,
	})

	// calculate fingerprint
	fingerprint := calculateKeyFingerprint(string(publicKeyPEM))

	return &OCIAPIKeyPair{
		PrivateKeyPEM: string(privateKeyPEM),
		PublicKeyPEM:  string(publicKeyPEM),
		Fingerprint:   fingerprint,
	}, nil
}

// getAPIKeyPairFromPrivateKey extracts the public key from a private key and calculates fingerprint.
func getAPIKeyPairFromPrivateKey(privateKeyPEM string) (*OCIAPIKeyPair, error) {
	// decode private key
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// extract public key and encode to PEM format
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyDER,
	})

	// calculate fingerprint
	fingerprint := calculateKeyFingerprint(string(publicKeyPEM))

	return &OCIAPIKeyPair{
		PrivateKeyPEM: privateKeyPEM,
		PublicKeyPEM:  string(publicKeyPEM),
		Fingerprint:   fingerprint,
	}, nil
}

// calculateKeyFingerprint calculates the MD5 fingerprint of a public key.
func calculateKeyFingerprint(publicKeyPEM string) string {
	// remove PEM headers and whitespace
	lines := strings.Split(publicKeyPEM, "\n")
	var keyData strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-----") && line != "" {
			keyData.WriteString(line)
		}
	}

	// decode base64 key data
	keyBytes, err := base64.StdEncoding.DecodeString(keyData.String())
	if err != nil {
		// fallback: use the raw key data if base64 decoding fails
		keyBytes = []byte(keyData.String())
	}

	// calculate MD5 hash (required by OCI for API key fingerprinting)
	hash := md5.Sum(keyBytes)

	// format as colon-separated hex pairs
	fingerprint := make([]string, len(hash))
	for i, b := range hash {
		fingerprint[i] = fmt.Sprintf("%02x", b)
	}

	return strings.Join(fingerprint, ":")
}
