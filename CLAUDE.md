./bin/tptctl down --name oke-test --infra-only

# Build Commands
**IMPORTANT**: Use mage for building tptctl, NOT go build
```bash
# Build tptctl binary
mage build:tptctl

# NOT: go build ./...
```

# OKE Testing Command (from launch.json)
go run cmd/tptctl/main.go up --provider=oke --name=oke-test-9 --oci-region=us-ashburn-1 --oci-config-profile=DEFAULT --force-overwrite-config -r rj9317 -t add-oci-install-guide

# Threeport Code Conventions

## Function Naming
- Use PascalCase for exported functions: `ThreeportWorkloadName`, `GetConnection`, `CreateOCIUserAndCredentials`
- Use camelCase for private/unexported functions: `createOCICompartment`, `validateThreeportState`, `getAvailabilityDomainName`
- Function names should be descriptive and use complete words rather than abbreviations
- **CRITICAL**: Use "verb+action" naming pattern for all functions:
  - `GetClusterOCID`, `CreateOCIUser`, `ValidateOCIUserPropagation`, `DeployComplete`
  - NOT: `ClusterOCID`, `OCIUser`, `UserPropagation`
  - Start with action verbs: `Get`, `Create`, `Validate`, `Deploy`, `Delete`, `Generate`, `Write`, etc.

## Function Documentation (Docstrings)
- ALL functions (both exported and unexported) require documentation comments
- Comments must begin with the function name followed by a verb describing what it does
- Use present tense verbs: "creates", "returns", "validates", "performs"
- Format: `// FunctionName verbs what the function does.`

### Examples of correct docstring format:
```go
// ThreeportWorkloadName returns a standardized name for a ThreeportWorkload
// Kubernetes custom resource based on the workload instance ID.
func ThreeportWorkloadName(...)

// MergeHelmValuesGo merges two helm values documents and
// returns the result as a map[string]interface{}.
func MergeHelmValuesGo(...)

// createOCICompartment creates a new compartment for the threeport instance.
func (b *OCIBootstrapSDK) createOCICompartment(...)

// getAvailabilityDomainName returns the full name of the first availability domain in the region.
func (i *KubernetesRuntimeInfraOKE) getAvailabilityDomainName() (string, error)

// CreateOCIUserAndCredentials creates user, groups, policies, and API key using OCI SDK directly.
func (b *OCIBootstrapSDK) CreateOCIUserAndCredentials() error

// ValidateOCIUserPropagation validates that the service user credentials are propagated across all OCI services.
func (b *OCIBootstrapSDK) ValidateOCIUserPropagation() error
```

## Key Patterns:
- Exported functions get detailed multi-line descriptions when complex
- Unexported functions get concise single-line descriptions
- Always start with function name + verb
- End with period
- For functions that span multiple lines in description, maintain consistent formatting

## Logging Conventions

### Structured Logging (Controllers/Reconcilers)
- Use `log.Info()`, `log.Error()`, `log.V(1).Info()` for internal operations
- Include contextual fields: `log.Error(err, "failed to create resource", "resourceID", id)`
- Use verbosity levels: `log.V(1).Info()` for debug-level information

### Console Output (User-facing Operations)
- **NO EMOJIS** - Use plain text only
- Use `fmt.Printf()` for standard output operations
- Use `fmt.Fprintf(os.Stderr, ...)` for errors
- **Message formats:**
  - Success: `fmt.Printf("Successfully created %s\n", name)`
  - Info/Status: `fmt.Printf("Using existing %s: %s\n", type, name)`
  - Progress: `fmt.Printf("Creating %s...\n", resource)`
  - Errors: `fmt.Fprintf(os.Stderr, "Error: %s\n", err)`
  - Usage: `fmt.Fprintf(os.Stderr, "Usage: %s <args>\n", program)`

### CLI Output Functions (pkg/cli/v0/output.go)
Use the standardized functions when available:
- `cli.Info("message")` → "Info: message"
- `cli.Error("message", err)` → "Error: message" (in red)
- `cli.Warning("message")` → "Warning: message" (in yellow)
- `cli.Complete("message")` → "Complete: message" (in green)

### Formatting Standards:
- **Always end with newline**: `\n`
- **Simple descriptive text**: "Creating compartment", "Successfully authenticated"
- **Include relevant details**: resource names, IDs, accounts when helpful
- **Consistent capitalization**: Start messages with capital letters

## Error Message Conventions

### Error Wrapping and Formatting
- **Use `%w` for error wrapping**: `fmt.Errorf("failed to create resource: %w", err)`
- **NOT `%v`**: Avoid `fmt.Errorf("failed to create resource: %v", err)`
- **Error messages start lowercase**: "failed to create", not "Failed to create"
- **Use consistent patterns**: "failed to [action]" for most error messages

### Error Types
- **Wrapped errors**: `fmt.Errorf("failed to get user: %w", err)`
- **Simple errors**: `errors.New("user must be attached to workload instance")`
- **Context-specific errors**: Include relevant details when helpful

### Examples of correct error patterns:
```go
// Error wrapping (preferred)
return fmt.Errorf("failed to create compartment: %w", err)
return fmt.Errorf("failed to get tenancy OCID: %w", err)

// Simple errors
return errors.New("secret instance must be attached to a workload instance")
return errors.New("deletion notification received but not scheduled")

// With context
return fmt.Errorf("no active cluster found with name %s", clusterName)
```

### Common Error Message Patterns:
- `"failed to create X"`
- `"failed to get X"`
- `"failed to update X"`
- `"failed to delete X"`
- `"X not found"`
- `"X must be Y"`

## Code Formatting Conventions

### Inline Error Handling
- **Use `if err := function(); err != nil` pattern** whenever possible for concise code
- **Prefer inline assignment and check** over separate variable assignment

```go
// Preferred (concise)
if err := b.createOCICompartment(client); err != nil {
    return fmt.Errorf("failed to create compartment: %w", err)
}

// Avoid when possible (verbose)
err := b.createOCICompartment(client)
if err != nil {
    return fmt.Errorf("failed to create compartment: %w", err)
}
```

### Function Parameters and Multi-line Formatting
- **Break parameters into multiple lines** when function calls have >3 parameters or are very long
- **Align parameters vertically** for readability
- **Apply same rules to function definitions**

```go
// Multi-line function call (>3 parameters or long line)
if err := client.EnsureAttachedObjectReferenceExists(
    c.r.APIClient,
    c.r.APIServer,
    c.workloadInstanceType,
    c.workloadInstanceId,
    util.TypeName(*c.secretInstance),
    c.secretInstance.ID,
); err != nil {
    return fmt.Errorf("failed to ensure reference exists: %w", err)
}

// Multi-line function definition
func NewOCIInfrastructureRefactored(
    runtimeInstanceName,
    version,
    tenancyOCID,
    targetRegion,
    workerNodeShape string,
    workerNodeInitialCount int32,
    bootstrap *OCIBootstrapSDK,
) *OCIInfrastructureRefactored {
    // implementation
}
```

### Formatting Rules:
- **Indent parameters**: Use tabs for indentation
- **Closing parenthesis**: Place on same line as last parameter followed by function call continuation
- **Comma placement**: After each parameter except the last
- **Consistent alignment**: Parameters should align vertically

## Inline Comment Conventions

### Step Comments Within Functions
- **Start with lowercase**: All inline comments describing steps within functions must start with lowercase letters
- **Use action verbs**: Start with verbs like "create", "set", "get", "delete", "configure", "validate", etc.
- **Be concise**: Keep comments brief and focused on the action being performed
- **Maintain indentation**: Follow the same indentation level as the code they describe

### Examples of correct inline comment format:
```go
func (i *KubernetesRuntimeInfraOKE) CreateOCIResources() error {
    // set up OCI client using existing config provider
    configProvider := i.ConfigProvider

    // get tenancy OCID from the config
    tenancyOCID, err := configProvider.TenancyOCID()

    // create compartment for this threeport instance
    if err := i.createOCICompartment(client); err != nil {
        return fmt.Errorf("failed to create compartment: %w", err)
    }

    // delete all API keys for the user first
    for _, apiKey := range keysResponse.Items {
        // delete the API key
        _, err = client.DeleteApiKey(context.Background(), deleteKeyRequest)
    }
}
```

### Examples of INCORRECT inline comment format:
```go
// WRONG: Starting with uppercase
// Create compartment for this threeport instance

// WRONG: Too verbose
// This function creates a new compartment in OCI for the threeport instance

// WRONG: Missing action verb
// compartment for this threeport instance
```

### Comment Patterns:
- `// create X`
- `// delete X`
- `// get X from Y`
- `// set up X`
- `// configure X for Y`
- `// validate X`
- `// list X to find Y`
- `// update X with Y`

### Indentation Rules:
- Comments should be indented to the same level as the code they describe
- Use tabs for indentation consistency
- Place comments immediately before the code block they describe

## String Consistency and Shared Constants

### CRITICAL: Avoid String Duplication in Related Operations
When multiple functions or methods depend on the same string value (especially for resource names, identifiers, or configuration keys), **ALWAYS** extract the string into a shared constant or variable to prevent inconsistencies and bugs.

### Common Problem Patterns:
```go
// WRONG: Duplicate strings - prone to bugs
func createUser() {
    userName := fmt.Sprintf("threeport-service-%s", instanceName)
    // create user logic
}

func deleteUser() {
    userName := fmt.Sprintf("threeport-user-%s", instanceName)  // BUG: Different format!
    // delete user logic - will fail to find user
}
```

### Correct Solution: Shared Constants
```go
// CORRECT: Shared constants ensure consistency
const (
    serviceUserNameFormat = "threeport-service-%s"
    compartmentNameFormat = "threeport-%s"
    groupNameFormat       = "threeport-bootstrap-%s"
    policyNameFormat      = "threeport-bootstrap-policy-%s"
)

func createUser() {
    userName := fmt.Sprintf(serviceUserNameFormat, instanceName)
    // create user logic
}

func deleteUser() {
    userName := fmt.Sprintf(serviceUserNameFormat, instanceName)  // Always consistent!
    // delete user logic
}
```

### When to Use Shared Constants:
- **Resource naming**: Database names, cloud resource names, API endpoints
- **Configuration keys**: Environment variables, config file keys
- **Create/Delete pairs**: Any operation that creates and later deletes the same resource
- **Validation patterns**: Error messages, validation rules
- **Multiple references**: Any string used in 2+ places

### Benefits:
- **Prevents bugs**: Impossible to have mismatched strings between related operations
- **Single source of truth**: Changes only need to be made in one place
- **Easier refactoring**: Rename patterns across entire codebase safely
- **Better maintainability**: Clear documentation of all string patterns in use

### Examples from Real Issues:
```go
// This caused a critical bug where group deletion failed:
// Create: "threeport-bootstrap-test"
// Delete: "threeport-group-test"  // WRONG! Resource not found

// Fixed with constants:
const bootstrapGroupNameFormat = "threeport-bootstrap-%s"
```

**Rule**: If you find yourself typing the same string pattern twice, immediately extract it into a constant.

# Dependency Version Management

## CRITICAL: Always Check for Latest Versions
- **NEVER assume first search result is latest** - Always verify you have the most recent version
- **ALWAYS search web for latest version** of every new dependency before adding it
- **CHECK GITHUB RELEASES**: Always check the actual GitHub releases page for the official latest version
- **UPDATE EXISTING DEPENDENCIES**: When working with libraries, check if existing ones need updates too
- **SEARCH PATTERN**: Use queries like "library-name latest version 2024 2025 github releases"

### Examples:
```bash
# WRONG: Just adding first version found
go mod add github.com/some/library v1.2.0

# RIGHT: After web searching for latest
go mod add github.com/some/library v2.1.5  # (after confirming this is latest)
```

### Version Check Workflow:
1. **Web search**: "[library] latest version 2024 2025 github releases"
2. **Visit GitHub releases**: Check actual releases page for latest tag
3. **Verify compatibility**: Check if major version changes require code updates
4. **Update go.mod**: Use the confirmed latest version
5. **Run `go mod tidy`**: Let Go update transitive dependencies

## DEBUGGING RULE: Always Check Dependencies First

**CRITICAL**: When debugging any issue involving external dependencies, ALWAYS proactively check for version updates FIRST before diving into code analysis.

### When to Check Dependency Versions:
- **Any compilation error** involving external libraries
- **Runtime errors** from external dependencies (Pulumi, OCI SDK, etc.)
- **Nil pointer errors** or marshaling issues with providers
- **Compatibility issues** between CLI tools and SDKs
- **"Missing field" or "unknown field" errors**
- **Any provider-related errors** (AWS, OCI, GCP, etc.)

### Debugging Version Check Process:
1. **Immediately check current versions**: `grep -E "(dependency)" go.mod`
2. **Search for latest versions**: Check GitHub releases for each external dependency
3. **Show version comparison**: Display "Current: vX.Y.Z → Latest: vA.B.C"
4. **Recommend updates**: Suggest updating to latest compatible versions
5. **Update and test**: Apply updates before continuing debugging

### Example Response Pattern:
```
Current dependency versions:
- Pulumi CLI: v3.193.0 → Latest: v3.198.0 (5 versions behind)
- Pulumi SDK: v3.190.0 → Latest: v3.198.0 (8 versions behind)
- OCI Provider: v3.9.0 → Latest: v3.9.0 (up to date)

Recommendation: Update Pulumi CLI and SDK first - version mismatches often cause marshaling errors.
```

**Why This Matters**: Many debugging issues (especially marshaling errors, nil pointers, and compatibility problems) are caused by version mismatches between external dependencies. Always rule out version issues before deep-diving into code analysis.

# important-instruction-reminders
Do what has been asked; nothing more, nothing less.
NEVER create files unless they're absolutely necessary for achieving your goal.
ALWAYS prefer editing an existing file to creating a new one.
NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.