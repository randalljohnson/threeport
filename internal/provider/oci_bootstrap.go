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
	"strings"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

// OCIAPIKeyPair represents an API key pair for OCI authentication
type OCIAPIKeyPair struct {
	PrivateKeyPEM string
	PublicKeyPEM  string
	Fingerprint   string
}

// getOCIConfigSectionName returns the standardized OCI config section name for the service user.
func (i *KubernetesRuntimeInfraOKE) getOCIConfigSectionName() string {
	return fmt.Sprintf("[%s-service]", i.RuntimeInstanceName)
}

// GetServiceUserName returns the standardized service user name for this threeport instance.
func (i *KubernetesRuntimeInfraOKE) GetServiceUserName() string {
	return fmt.Sprintf("%s-user", i.RuntimeInstanceName)
}

// getServiceUserEmail returns the email for the service user.
func (i *KubernetesRuntimeInfraOKE) getServiceUserEmail() string {
	return fmt.Sprintf("%s-user@threeport.io", i.RuntimeInstanceName)
}

// getGroupName returns the standardized group name for this threeport instance.
func (i *KubernetesRuntimeInfraOKE) getGroupName() string {
	return fmt.Sprintf("%s-group", i.RuntimeInstanceName)
}

// getPolicyName returns the standardized policy name for this threeport instance.
func (i *KubernetesRuntimeInfraOKE) getPolicyName() string {
	return fmt.Sprintf("%s-policy", i.RuntimeInstanceName)
}

// OCI Compartment Operations

// createOCICompartment creates a new compartment for the threeport instance.
func (i *KubernetesRuntimeInfraOKE) createOCICompartment(client identity.IdentityClient) error {
	compartmentDescription := fmt.Sprintf("Compartment for threeport instance %s", i.RuntimeInstanceName)

	// check if compartment already exists
	listRequest := identity.ListCompartmentsRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &i.RuntimeInstanceName,
	}

	response, err := client.ListCompartments(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list compartments: %w", err)
	}

	if len(response.Items) > 0 {
		i.CompartmentOCID = *response.Items[0].Id
		fmt.Printf("Using existing compartment: %s\n", i.RuntimeInstanceName)
		return nil
	}

	// create new compartment
	fmt.Printf("Creating compartment: %s\n", i.RuntimeInstanceName)
	createRequest := identity.CreateCompartmentRequest{
		CreateCompartmentDetails: identity.CreateCompartmentDetails{
			CompartmentId: &i.TenancyOCID,
			Name:          &i.RuntimeInstanceName,
			Description:   &compartmentDescription,
		},
	}

	createResponse, err := client.CreateCompartment(context.Background(), createRequest)
	if err != nil {
		return fmt.Errorf("failed to create compartment: %w", err)
	}

	i.CompartmentOCID = *createResponse.Compartment.Id
	fmt.Printf("Successfully created compartment: %s\n", i.RuntimeInstanceName)
	return nil
}

// deleteOCICompartment deletes an OCI compartment by name.
func (i *KubernetesRuntimeInfraOKE) deleteOCICompartment(client identity.IdentityClient) error {
	// list compartments to find the one to delete
	listRequest := identity.ListCompartmentsRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &i.RuntimeInstanceName,
	}

	response, err := client.ListCompartments(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list compartments: %w", err)
	}

	if len(response.Items) == 0 {
		fmt.Printf("Compartment %s not found, skipping deletion\n", i.RuntimeInstanceName)
		return nil
	}

	// delete the compartment
	deleteRequest := identity.DeleteCompartmentRequest{
		CompartmentId: response.Items[0].Id,
	}

	_, err = client.DeleteCompartment(context.Background(), deleteRequest)
	if err != nil {
		return fmt.Errorf("failed to delete compartment: %w", err)
	}

	fmt.Printf("Compartment deletion initiated (may take time to complete)\n")
	return nil
}

// OCI User Operations

// createOCIServiceUser creates the threeport service user.
func (i *KubernetesRuntimeInfraOKE) createOCIServiceUser(client identity.IdentityClient) error {
	userName := i.GetServiceUserName()
	userEmail := i.getServiceUserEmail()

	// check if user already exists
	listRequest := identity.ListUsersRequest{
		CompartmentId: &i.TenancyOCID,
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
	fmt.Printf("Successfully created user: %s\n", userName)
	return nil
}

// deleteOCIUser deletes an OCI user by name.
func (i *KubernetesRuntimeInfraOKE) deleteOCIUser(client identity.IdentityClient) error {
	userName := i.GetServiceUserName()
	// list users to find the one to delete
	listRequest := identity.ListUsersRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &userName,
	}

	response, err := client.ListUsers(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	if len(response.Items) == 0 {
		fmt.Printf("User %s not found, skipping deletion\n", userName)
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
			fmt.Fprintf(os.Stderr, "Warning: failed to delete API key: %v\n", err)
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
	groupName := i.getGroupName()
	groupDescription := fmt.Sprintf("Bootstrap group for threeport instance %s", i.RuntimeInstanceName)

	// check if group already exists
	listRequest := identity.ListGroupsRequest{
		CompartmentId: &i.TenancyOCID,
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
			CompartmentId: &i.TenancyOCID,
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
	groupName := i.getGroupName()

	// get group OCID
	listRequest := identity.ListGroupsRequest{
		CompartmentId: &i.TenancyOCID,
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
		CompartmentId: &i.TenancyOCID,
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
	groupName := i.getGroupName()
	// list groups to find the one to delete
	listRequest := identity.ListGroupsRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &groupName,
	}

	response, err := client.ListGroups(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	if len(response.Items) == 0 {
		fmt.Printf("Group %s not found, skipping deletion\n", groupName)
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
func (i *KubernetesRuntimeInfraOKE) createOCIPolicy(client identity.IdentityClient) error {
	groupName := i.getGroupName()
	policyName := i.getPolicyName()
	policyDescription := fmt.Sprintf("Threeport bootstrap policy for %s", i.RuntimeInstanceName)
	policyStatements := []string{
		// policies recommended in OCI documentation
		// https://docs.public.content.oci.oraclecloud.com/en-us/iaas/compute-cloud-at-customer/topics/oke/create-a-user-group-and-policies-that-authorize-members-to-use-oke.htm
		fmt.Sprintf("Allow group %s to read all-resources in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage cluster-family in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage instance-family in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage network-load-balancers in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage virtual-network-family in compartment %s", groupName, i.RuntimeInstanceName),
		// additional policies
		fmt.Sprintf("Allow group %s to inspect compartments in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage volume-family in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage load-balancers in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to use vnics in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to use network-security-groups in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to use private-ips in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage public-ips in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage object-family in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage tag-namespaces in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to manage tag-defaults in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to use tag-namespaces in compartment %s", groupName, i.RuntimeInstanceName),
		fmt.Sprintf("Allow group %s to use subnets in compartment %s", groupName, i.RuntimeInstanceName),
	}

	// check if policy already exists
	listRequest := identity.ListPoliciesRequest{
		CompartmentId: &i.TenancyOCID,
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

	// create new policy
	fmt.Printf("Creating policy: %s\n", policyName)
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
		return fmt.Errorf("failed to create policy: %w", err)
	}

	fmt.Printf("Successfully created policy: %s\n", policyName)
	return nil
}

// deleteOCIPolicy deletes an OCI policy by name.
func (i *KubernetesRuntimeInfraOKE) deleteOCIPolicy(client identity.IdentityClient) error {
	policyName := i.getPolicyName()
	// list policies to find the one to delete
	listRequest := identity.ListPoliciesRequest{
		CompartmentId: &i.TenancyOCID,
		Name:          &policyName,
	}

	response, err := client.ListPolicies(context.Background(), listRequest)
	if err != nil {
		return fmt.Errorf("failed to list policies: %w", err)
	}

	if len(response.Items) == 0 {
		fmt.Printf("Policy %s not found, skipping deletion\n", policyName)
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

	privateKeyPath := fmt.Sprintf("%s/.oci/%s-private-key.pem", homeDir, i.RuntimeInstanceName)

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
	fmt.Printf("Writing OCI configuration\n")

	config := fmt.Sprintf(`%s
user=%s
fingerprint=%s
tenancy=%s
region=%s
key_file=%s
`,
		i.getOCIConfigSectionName(),
		i.ServiceUserOCID,
		i.Fingerprint,
		i.TenancyOCID,
		i.Region,
		fmt.Sprintf("~/.oci/%s-private-key.pem", i.RuntimeInstanceName),
	)

	// write configuration to OCI config file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := fmt.Sprintf("%s/.oci/config", homeDir)
	privateKeyPath := fmt.Sprintf("%s/.oci/%s-private-key.pem", homeDir, i.RuntimeInstanceName)

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
	fmt.Printf("Cleaning up OCI configuration\n")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := fmt.Sprintf("%s/.oci/config", homeDir)
	privateKeyPath := fmt.Sprintf("%s/.oci/%s-private-key.pem", homeDir, i.RuntimeInstanceName)

	// remove private key file
	if err := os.Remove(privateKeyPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: failed to remove private key file: %v\n", err)
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

	fmt.Printf("Successfully cleaned up OCI configuration\n")
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
