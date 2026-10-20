// Package syncplan validates a declared inventory for a future local-to-managed
// world import. It neither reads files nor proves that an inventory is complete,
// authentic, coherent, or safe to execute.
package syncplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	MaxEntries    = 8192
	MaxPathBytes  = 512
	MaxDepth      = 32
	MaxFileBytes  = int64(64 << 30)
	MaxTotalBytes = int64(512 << 30)
)

type Mode string

const (
	ManagedPrivate Mode = "managed-private"
	ManagedPublic  Mode = "managed-public"
)

type Layout string

const (
	JavaPre26 Layout = "java-pre26"
	Java26    Layout = "java-26"
)

type Kind string

const (
	File      Kind = "file"
	Directory Kind = "directory"
)

// Entry describes an untrusted inventory claim. SHA256 must be lowercase hex
// for a file and empty for a directory. Directory sizes must be zero. No link
// or special-file kind is accepted, even when its path would be excluded.
type Entry struct {
	Path   string `json:"path"`
	Kind   Kind   `json:"kind"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Request deliberately contains no policy acceptance, source checkpoint, or
// runtime-security assertion. Layout is a declared profile, not format proof.
// V1 permits only conventional world, world_nether, and world_the_end roots.
type Request struct {
	TargetMode           Mode     `json:"target_mode"`
	SourceLayout         Layout   `json:"source_layout"`
	BaseRemoteGeneration uint64   `json:"base_remote_generation"`
	WorldRoots           []string `json:"world_roots"`
	Entries              []Entry  `json:"entries"`
}

// TrustedContext is supplied by the authenticated controller from its own
// policy-acceptance, generation, and checkpoint records. Do not populate this
// from client JSON. JSON decoding cannot populate these fields. The checkpoint
// fields are attestations by the caller; this package cannot verify them.
type TrustedContext struct {
	RequiredPolicyRevision string `json:"-"`
	AcceptedPolicyRevision string `json:"-"`
	RemoteGeneration       uint64 `json:"-"`
	CheckpointID           string `json:"-"`
	SourceStopped          bool   `json:"-"`
	CoherentCheckpoint     bool   `json:"-"`
}

type ExcludedEntry struct {
	Entry
	Reason string `json:"reason"`
}

// Result is review data, never an executor capability. Digest binds this
// canonical result with the digest field empty; it is not a signature, file
// integrity check, authorization token, or a durable reservation.
type Result struct {
	Schema               string          `json:"schema"`
	TargetMode           Mode            `json:"target_mode"`
	SourceLayout         Layout          `json:"source_layout"`
	BaseRemoteGeneration uint64          `json:"base_remote_generation"`
	PolicyRevision       string          `json:"policy_revision"`
	CheckpointID         string          `json:"checkpoint_id"`
	WorldRoots           []string        `json:"world_roots"`
	Entries              []Entry         `json:"entries"`
	Excluded             []ExcludedEntry `json:"excluded"`
	TotalBytes           int64           `json:"total_bytes"`
	ExcludedBytes        int64           `json:"excluded_bytes"`
	Blockers             []string        `json:"blockers"`
	Digest               string          `json:"digest"`
	Executable           bool            `json:"executable"`
}

var (
	ErrInvalidRequest   = errors.New("invalid sync request")
	ErrInvalidInventory = errors.New("invalid sync inventory")
	ErrInvalidContext   = errors.New("invalid trusted sync context")
	ErrConflict         = errors.New("remote generation conflict")
)

// Plan validates and canonicalizes bounded review data. It does not modify req.
// Every successful result has executable:false and unresolved execution gates,
// even if every caller-supplied attestation is positive. Generation is only
// compared here; an executor must compare-and-swap it again during promotion.
func Plan(req Request, trusted TrustedContext) (Result, error) {
	if req.TargetMode != ManagedPrivate && req.TargetMode != ManagedPublic {
		return Result{}, fmt.Errorf("%w: unsupported target_mode", ErrInvalidRequest)
	}
	if req.SourceLayout != JavaPre26 && req.SourceLayout != Java26 {
		return Result{}, fmt.Errorf("%w: unsupported source_layout", ErrInvalidRequest)
	}
	if !validID(trusted.RequiredPolicyRevision) ||
		(trusted.AcceptedPolicyRevision != "" && !validID(trusted.AcceptedPolicyRevision)) ||
		(trusted.CheckpointID != "" && !validID(trusted.CheckpointID)) {
		return Result{}, fmt.Errorf("%w: invalid revision or checkpoint identifier", ErrInvalidContext)
	}
	if req.BaseRemoteGeneration != trusted.RemoteGeneration {
		return Result{}, ErrConflict
	}
	if len(req.WorldRoots) < 1 || len(req.WorldRoots) > 3 {
		return Result{}, fmt.Errorf("%w: provide one to three conventional world roots", ErrInvalidRequest)
	}
	rootSet := make(map[string]bool, len(req.WorldRoots))
	for _, root := range req.WorldRoots {
		if (root != "world" && root != "world_nether" && root != "world_the_end") || rootSet[root] {
			return Result{}, fmt.Errorf("%w: unsupported or duplicate world root", ErrInvalidRequest)
		}
		rootSet[root] = true
	}
	if len(req.Entries) < 1 || len(req.Entries) > MaxEntries {
		return Result{}, fmt.Errorf("%w: inventory entry count out of bounds", ErrInvalidInventory)
	}

	// Prefix spellings include implicit directories: world/Region/a and
	// world/region/b collide even without explicit directory entries.
	spellings := make(map[string]string)
	explicit := make(map[string]Kind, len(req.Entries))
	parents := make(map[string]bool)
	var inventoryBytes int64
	for _, entry := range req.Entries {
		parts, err := validPath(entry.Path)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %s", ErrInvalidInventory, err)
		}
		if entry.Kind != File && entry.Kind != Directory {
			return Result{}, fmt.Errorf("%w: links and special files are unsupported", ErrInvalidInventory)
		}
		if entry.Kind == Directory {
			if entry.Size != 0 || entry.SHA256 != "" {
				return Result{}, fmt.Errorf("%w: directory metadata must have zero size and no hash", ErrInvalidInventory)
			}
		} else {
			if entry.Size < 0 || entry.Size > MaxFileBytes || !validDigest(entry.SHA256) {
				return Result{}, fmt.Errorf("%w: invalid file size or SHA256", ErrInvalidInventory)
			}
			if inventoryBytes > MaxTotalBytes-entry.Size {
				return Result{}, fmt.Errorf("%w: inventory byte limit exceeded", ErrInvalidInventory)
			}
			inventoryBytes += entry.Size
			if rootSet[entry.Path] {
				return Result{}, fmt.Errorf("%w: a world root cannot be a file", ErrInvalidInventory)
			}
		}
		folded := strings.ToLower(entry.Path)
		if _, exists := explicit[folded]; exists {
			return Result{}, fmt.Errorf("%w: duplicate or case-colliding path", ErrInvalidInventory)
		}
		if entry.Kind == File && parents[folded] {
			return Result{}, fmt.Errorf("%w: file path is also a directory prefix", ErrInvalidInventory)
		}
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			key := strings.ToLower(prefix)
			if previous, exists := spellings[key]; exists && previous != prefix {
				return Result{}, fmt.Errorf("%w: case-colliding directory or file", ErrInvalidInventory)
			}
			spellings[key] = prefix
			if i < len(parts)-1 {
				if explicit[key] == File {
					return Result{}, fmt.Errorf("%w: file path is also a directory prefix", ErrInvalidInventory)
				}
				parents[key] = true
			}
		}
		explicit[folded] = entry.Kind
	}

	result := Result{
		Schema: "metallic.sync-plan/v1", TargetMode: req.TargetMode,
		SourceLayout: req.SourceLayout, BaseRemoteGeneration: req.BaseRemoteGeneration,
		PolicyRevision: trusted.RequiredPolicyRevision, CheckpointID: trusted.CheckpointID,
		WorldRoots: append([]string(nil), req.WorldRoots...),
		Entries:    []Entry{}, Excluded: []ExcludedEntry{}, Blockers: []string{},
		Executable: false,
	}
	sort.Strings(result.WorldRoots)
	fileCount := 0
	for _, entry := range req.Entries {
		reason := exclusionReason(entry.Path, rootSet)
		if reason != "" {
			result.Excluded = append(result.Excluded, ExcludedEntry{Entry: entry, Reason: reason})
			result.ExcludedBytes += entry.Size
			continue
		}
		result.Entries = append(result.Entries, entry)
		result.TotalBytes += entry.Size
		if entry.Kind == File {
			fileCount++
		}
	}
	if fileCount == 0 {
		return Result{}, fmt.Errorf("%w: no eligible world files", ErrInvalidInventory)
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	sort.Slice(result.Excluded, func(i, j int) bool { return result.Excluded[i].Path < result.Excluded[j].Path })
	if trusted.AcceptedPolicyRevision != trusted.RequiredPolicyRevision {
		result.Blockers = append(result.Blockers, "current-policy-acceptance-required")
	}
	if trusted.CheckpointID == "" || !trusted.SourceStopped || !trusted.CoherentCheckpoint {
		result.Blockers = append(result.Blockers, "stopped-coherent-checkpoint-required")
	}
	result.Blockers = append(result.Blockers,
		"upload-content-verification-required",
		"world-snapshot-verification-required",
		"managed-compatibility-profile-required",
		"isolated-runtime-admission-required",
		"atomic-promotion-required",
	)
	if req.TargetMode == ManagedPublic {
		result.Blockers = append(result.Blockers, "public-discovery-review-required")
	}
	canonical, err := json.Marshal(result)
	if err != nil {
		return Result{}, fmt.Errorf("canonicalize sync plan: %w", err)
	}
	digest := sha256.Sum256(canonical)
	result.Digest = hex.EncodeToString(digest[:])
	return result, nil
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 || !alphaNumeric(value[0]) {
		return false
	}
	for i := range value {
		c := value[i]
		if !alphaNumeric(c) && c != '.' && c != '_' && c != '-' && c != ':' {
			return false
		}
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		if !((value[i] >= '0' && value[i] <= '9') || (value[i] >= 'a' && value[i] <= 'f')) {
			return false
		}
	}
	return true
}

func alphaNumeric(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

func validPath(value string) ([]string, error) {
	if len(value) < 1 || len(value) > MaxPathBytes {
		return nil, errors.New("path length out of bounds")
	}
	for i := range value {
		c := value[i]
		if !alphaNumeric(c) && c != '.' && c != '_' && c != '-' && c != '/' {
			return nil, errors.New("path contains unsupported characters")
		}
	}
	parts := strings.Split(value, "/")
	if len(parts) > MaxDepth {
		return nil, errors.New("path depth out of bounds")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 128 || strings.HasSuffix(part, ".") {
			return nil, errors.New("path contains a nonportable component")
		}
		stem := strings.ToLower(strings.SplitN(part, ".", 2)[0])
		if stem == "con" || stem == "prn" || stem == "aux" || stem == "nul" ||
			(len(stem) == 4 && (strings.HasPrefix(stem, "com") || strings.HasPrefix(stem, "lpt")) && stem[3] >= '0' && stem[3] <= '9') {
			return nil, errors.New("path contains a reserved device name")
		}
	}
	return parts, nil
}

func exclusionReason(path string, roots map[string]bool) string {
	parts := strings.Split(path, "/")
	for _, part := range parts {
		name := strings.ToLower(part)
		switch name {
		case ".git", ".ssh", ".aws", ".azure", ".kube", ".gnupg", ".minecraft", ".metallic":
			return "private-or-control-data"
		case "plugins", "mods", "libraries", "versions", "runtime", "runtimes", "managed", "metallic-managed", "bin":
			return "runtime-or-plugin-data"
		case "logs", "crash-reports", "backups", "cache", "caches":
			return "local-or-transient-data"
		case "server.properties", "eula.txt", "bukkit.yml", "spigot.yml", "paper.yml", "paper-global.yml", "paper-world-defaults.yml", "velocity.toml", "session.lock":
			return "regenerated-managed-configuration"
		case "accounts.json", "launcher_accounts.json", "launcher_profiles.json", "credentials", "credentials.json", "credentials.yml", "secrets", "secrets.json", "secrets.yml", "tokens", "tokens.json", "token", "secret", "forwarding.secret":
			return "sensitive-name"
		}
		if name == ".env" || strings.HasPrefix(name, ".env.") {
			return "sensitive-name"
		}
		for _, suffix := range []string{".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".kdbx"} {
			if strings.HasSuffix(name, suffix) {
				return "sensitive-extension"
			}
		}
		for _, suffix := range []string{".jar", ".class", ".dll", ".so", ".dylib", ".exe", ".bat", ".cmd", ".ps1", ".sh"} {
			if strings.HasSuffix(name, suffix) {
				return "runtime-or-executable-artifact"
			}
		}
	}
	if !roots[parts[0]] {
		return "outside-declared-world-roots"
	}
	return ""
}
