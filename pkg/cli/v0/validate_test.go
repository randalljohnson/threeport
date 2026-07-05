package v0

import (
	"strings"
	"testing"
)

// TestValidateConfigNameFlags covers the mutual-exclusion check between
// a config file path and an object name flag.
func TestValidateConfigNameFlags(t *testing.T) {
	tests := []struct {
		name             string
		objectConfigPath string
		objectName       string
		objectOutputName string
		wantErr          bool
		wantErrSubstr    string
	}{
		{
			name:             "both empty is accepted",
			objectConfigPath: "",
			objectName:       "",
			objectOutputName: "workload",
			wantErr:          false,
		},
		{
			name:             "only config path is accepted",
			objectConfigPath: "/path/to/config.yaml",
			objectName:       "",
			objectOutputName: "workload",
			wantErr:          false,
		},
		{
			name:             "only object name is accepted",
			objectConfigPath: "",
			objectName:       "my-workload",
			objectOutputName: "workload",
			wantErr:          false,
		},
		{
			name:             "both provided is rejected with object name in message",
			objectConfigPath: "/path/to/config.yaml",
			objectName:       "my-workload",
			objectOutputName: "workload",
			wantErr:          true,
			wantErrSubstr:    "workload name and path to config file provided",
		},
		{
			name:             "both provided uses the supplied output name",
			objectConfigPath: "/path/to/config.yaml",
			objectName:       "some-name",
			objectOutputName: "kubernetes runtime instance",
			wantErr:          true,
			wantErrSubstr:    "kubernetes runtime instance name and path to config file provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// call the validator with the case's flag values
			err := ValidateConfigNameFlags(tt.objectConfigPath, tt.objectName, tt.objectOutputName)

			// assert error state matches expectation
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// assert error message names the object when both flags are given
			if tt.wantErr && !strings.Contains(err.Error(), tt.wantErrSubstr) {
				t.Fatalf("error %q missing substring %q", err.Error(), tt.wantErrSubstr)
			}
		})
	}
}

// TestValidateDescribeOutputFlag covers the allowed output-format set for
// describe commands.
func TestValidateDescribeOutputFlag(t *testing.T) {
	tests := []struct {
		name         string
		outputFormat string
		wantErr      bool
	}{
		{name: "plain is accepted", outputFormat: "plain", wantErr: false},
		{name: "json is accepted", outputFormat: "json", wantErr: false},
		{name: "yaml is accepted", outputFormat: "yaml", wantErr: false},
		{name: "empty is rejected", outputFormat: "", wantErr: true},
		{name: "unknown format is rejected", outputFormat: "xml", wantErr: true},
		{name: "uppercase JSON is accepted case-insensitively", outputFormat: "JSON", wantErr: false},
		{name: "mixed case Yaml is accepted case-insensitively", outputFormat: "Yaml", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// call the validator with the case's format string
			err := ValidateDescribeOutputFlag(tt.outputFormat, "workload")

			// assert error state matches expectation
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for output %q, got nil", tt.outputFormat)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for output %q: %v", tt.outputFormat, err)
			}

			// assert rejection message lists the valid formats so callers see them
			if tt.wantErr && !strings.Contains(err.Error(), "valid formats") {
				t.Fatalf("error %q missing valid-formats hint", err.Error())
			}
		})
	}
}
