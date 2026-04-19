package sah

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	TaskProtocolHeader           = "X-SAH-Task-Protocol"
	ClientCapabilitiesHeader     = "X-SAH-Client-Capabilities"
	SupportedTaskProtocol        = "2026-04-11"
	CapabilityAssignmentLinks    = "assignment-links"
	CapabilityScopedSubmit       = "assignment-scoped-submission"
	CapabilityScopedRelease      = "assignment-scoped-release"
	CapabilityServerAgentRequest = "server-agent-request"
)

var supportedClientCapabilities = []string{
	CapabilityAssignmentLinks,
	CapabilityScopedSubmit,
	CapabilityScopedRelease,
	// Advertised by sah-cli v0.9.x, which can consume server-owned
	// `agent_request` prompts directly. The server should not require this until
	// additive rollout is complete for the v0.8.x line.
	CapabilityServerAgentRequest,
}

type WorkerContractViolation struct {
	RequiredTaskProtocolVersion   string
	AdvertisedTaskProtocolVersion string
	RequiredClientCapabilities    []string
	MissingClientCapabilities     []string
	ReleaseNotesURL               string
	ResolutionCommand             string
}

func (err *WorkerContractViolation) Error() string {
	if err == nil {
		return ""
	}

	parts := []string{"SCIENCE@home worker contract mismatch"}
	if strings.TrimSpace(err.RequiredTaskProtocolVersion) != "" &&
		strings.TrimSpace(err.RequiredTaskProtocolVersion) != strings.TrimSpace(err.AdvertisedTaskProtocolVersion) {
		current := strings.TrimSpace(err.AdvertisedTaskProtocolVersion)
		if current == "" {
			current = "unspecified"
		}
		parts = append(
			parts,
			fmt.Sprintf(
				"task protocol %s is required but this CLI advertises %s",
				err.RequiredTaskProtocolVersion,
				current,
			),
		)
	}
	if len(err.MissingClientCapabilities) > 0 {
		parts = append(
			parts,
			fmt.Sprintf(
				"missing client capabilities: %s",
				strings.Join(err.MissingClientCapabilities, ", "),
			),
		)
	}

	command := strings.TrimSpace(err.ResolutionCommand)
	if command == "" {
		command = "install a newer `sah` release"
		if strings.TrimSpace(err.ReleaseNotesURL) != "" {
			return strings.Join(append(parts, err.ReleaseNotesURL), "; ")
		}
		return strings.Join(append(parts, command), "; ")
	}
	resolution := fmt.Sprintf("run `%s`", command)
	if strings.TrimSpace(err.ReleaseNotesURL) != "" {
		resolution += fmt.Sprintf(" (%s)", err.ReleaseNotesURL)
	}
	parts = append(parts, resolution)
	return strings.Join(parts, "; ")
}

func SupportedClientCapabilities() []string {
	return append([]string(nil), supportedClientCapabilities...)
}

func SupportedClientCapabilitiesHeaderValue() string {
	return strings.Join(SupportedClientCapabilities(), ", ")
}

func ResolveWorkerContractViolation(release *ClientReleaseResponse) *WorkerContractViolation {
	if release == nil {
		return nil
	}

	normalized := normalizeClientRelease(*release)
	requiredProtocol := strings.TrimSpace(normalized.RequiredTaskProtocolVersion)
	missing := missingCapabilities(
		SupportedClientCapabilities(),
		normalized.RequiredClientCapabilities,
	)
	if requiredProtocol == "" && len(missing) == 0 {
		return nil
	}
	if requiredProtocol == SupportedTaskProtocol && len(missing) == 0 {
		return nil
	}

	return &WorkerContractViolation{
		RequiredTaskProtocolVersion:   requiredProtocol,
		AdvertisedTaskProtocolVersion: SupportedTaskProtocol,
		RequiredClientCapabilities:    append([]string(nil), normalized.RequiredClientCapabilities...),
		MissingClientCapabilities:     missing,
		ReleaseNotesURL:               strings.TrimSpace(normalized.Links.ReleaseNotes.Href),
	}
}

func ValidateAssignmentContract(assignment Assignment) error {
	requiredProtocol := strings.TrimSpace(assignment.ProtocolVersion)
	if requiredProtocol == "" || requiredProtocol == SupportedTaskProtocol {
		return nil
	}

	return &WorkerContractViolation{
		RequiredTaskProtocolVersion:   requiredProtocol,
		AdvertisedTaskProtocolVersion: SupportedTaskProtocol,
	}
}

func IsWorkerContractError(err error) bool {
	if err == nil {
		return false
	}

	var violation *WorkerContractViolation
	if errors.As(err, &violation) {
		return true
	}

	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	if statusErr.StatusCode != http.StatusConflict && statusErr.StatusCode != http.StatusUpgradeRequired {
		return false
	}

	message := strings.ToLower(strings.TrimSpace(statusErr.Message + " " + statusErr.Body))
	return strings.Contains(message, "worker contract")
}

func normalizeCapability(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeCapabilities(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		capability := normalizeCapability(value)
		if capability == "" {
			continue
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		normalized = append(normalized, capability)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func missingCapabilities(supported []string, required []string) []string {
	if len(required) == 0 {
		return nil
	}

	supportedSet := map[string]struct{}{}
	for _, capability := range supported {
		normalized := normalizeCapability(capability)
		if normalized == "" {
			continue
		}
		supportedSet[normalized] = struct{}{}
	}

	missing := make([]string, 0, len(required))
	seen := map[string]struct{}{}
	for _, capability := range required {
		normalized := normalizeCapability(capability)
		if normalized == "" {
			continue
		}
		if _, exists := supportedSet[normalized]; exists {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		missing = append(missing, normalized)
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}
