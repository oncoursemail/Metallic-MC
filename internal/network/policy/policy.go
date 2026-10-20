// Package policy compiles a bounded, immutable authorization policy and evaluates
// it without I/O. Its inputs must come from a trusted caller: this package does
// not authenticate subjects, contexts, clocks, or policy publishers. Matching a
// Version identifies a snapshot; it does not prove that snapshot is current.
package policy

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	SchemaVersion    = 1
	MaxDocumentBytes = 4 << 20
	MaxRoles         = 256
	MaxSubjects      = 4096
	MaxRules         = 4096
	MaxParents       = 16
	MaxMemberships   = 32
	MaxDepth         = 32
	MaxContextPairs  = 16
	MaxIdentifier    = 128
	MaxContextValue  = 256
)

var (
	ErrInvalidDocument = errors.New("invalid policy document")
	ErrInvalidRequest  = errors.New("invalid policy request")
)

type Effect string

const (
	Allow Effect = "allow"
	Deny  Effect = "deny"
)

// Version binds the epoch, publisher's revision, and all canonical document
// contents. A publisher must independently prevent rollback and revision reuse.
type Version struct {
	Epoch    string            `json:"epoch"`
	Revision uint64            `json:"revision"`
	Digest   [sha256.Size]byte `json:"digest"`
}

type Document struct {
	Schema   uint8     `json:"schema"`
	Epoch    string    `json:"epoch"`
	Revision uint64    `json:"revision"`
	TenantID string    `json:"tenantId"`
	Roles    []Role    `json:"roles"`
	Subjects []Subject `json:"subjects"`
	Rules    []Rule    `json:"rules"`
}

type Role struct {
	ID      string   `json:"id"`
	Parents []string `json:"parents"`
}

type Subject struct {
	ID          string       `json:"id"`
	Memberships []Membership `json:"memberships"`
}

type Membership struct {
	RoleID    string            `json:"roleId"`
	Context   map[string]string `json:"context"`
	NotBefore time.Time         `json:"notBefore"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

// Rule selects exactly one subject or role, one action, and one resource. All
// context pairs must match. Identifiers and context values are case-sensitive.
// There are no wildcards, implicit defaults, priorities, or operator bypasses.
type Rule struct {
	ID           string            `json:"id"`
	SubjectID    string            `json:"subjectId"`
	RoleID       string            `json:"roleId"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resourceType"`
	ResourceID   string            `json:"resourceId"`
	Effect       Effect            `json:"effect"`
	Context      map[string]string `json:"context"`
	NotBefore    time.Time         `json:"notBefore"`
	ExpiresAt    time.Time         `json:"expiresAt"`
}

// Request contains caller-authenticated facts, not an HTTP request schema. Now
// must come from the enforcing service's trusted clock. The caller must not
// concurrently mutate Context while Evaluate reads it.
type Request struct {
	Expected     Version
	TenantID     string
	SubjectID    string
	Action       string
	ResourceType string
	ResourceID   string
	Context      map[string]string
	Now          time.Time
}

// ValidateRequest checks identifier grammar, all supplied context pairs and the
// clock's supported range. It does not authenticate facts, check required context
// completeness, compare Expected with a snapshot, or establish freshness. Callers
// must not concurrently mutate Context while validation reads it.
func ValidateRequest(r Request) error {
	if !validID(r.TenantID) || !validID(r.SubjectID) || !validID(r.Action) ||
		!validID(r.ResourceType) || !validID(r.ResourceID) || !validContext(r.Context) ||
		!ValidRequestTime(r.Now) {
		return ErrInvalidRequest
	}
	return nil
}

// ValidRequestTime accepts nonzero instants within UTC years 1970 through 9999,
// regardless of their supplied time zone. It proves neither clock authenticity
// nor monotonicity or freshness. Policy window bounds separately permit zero.
func ValidRequestTime(t time.Time) bool { return !t.IsZero() && validTime(t) }

type Decision struct {
	Allowed         bool     `json:"allowed"`
	Code            string   `json:"code"`
	Version         Version  `json:"version"`
	RuleID          string   `json:"ruleId,omitempty"`
	InheritancePath []string `json:"inheritancePath,omitempty"`
	// ValidUntil is the next scheduled document boundary, conservatively
	// including unrelated subjects and rules. Zero means there is no future
	// scheduled boundary. Neither value establishes a security freshness lease.
	ValidUntil time.Time `json:"validUntil,omitempty"`
}

type rolePath struct {
	index int
	path  []string
}

type compiledMembership struct {
	membership Membership
	paths      []rolePath
}

type compiledRule struct {
	rule      Rule
	roleIndex int // -1 for a direct subject rule
}

// Snapshot is read-only after Compile and safe for concurrent evaluations.
// Every evaluation loads one complete snapshot; returned slices are detached.
type Snapshot struct {
	version    Version
	tenantID   string
	subjects   map[string][]compiledMembership
	rules      map[string][]compiledRule
	boundaries []time.Time
}

// Version returns a value copy and never exposes internal document data.
func (s *Snapshot) Version() Version {
	if s == nil {
		return Version{}
	}
	return s.version
}

func invalid(detail string) error { return fmt.Errorf("%w: %s", ErrInvalidDocument, detail) }

// Compile validates the entire document, including unused rules and roles. The
// caller must not mutate the input during Compile; after it returns all maps
// and slices are independent of the caller's input. Rejected documents never
// produce a partially usable snapshot.
func Compile(input Document) (*Snapshot, error) {
	d, err := canonicalDocument(input)
	if err != nil {
		return nil, err
	}
	roleIndexes := make(map[string]int, len(d.Roles))
	for i, role := range d.Roles {
		if _, exists := roleIndexes[role.ID]; exists {
			return nil, invalid("duplicate role id")
		}
		roleIndexes[role.ID] = i
	}
	for _, role := range d.Roles {
		for _, parent := range role.Parents {
			if _, exists := roleIndexes[parent]; !exists {
				return nil, invalid("unknown parent role")
			}
		}
	}
	if err := validateGraph(d.Roles, roleIndexes); err != nil {
		return nil, err
	}
	paths := make([][]rolePath, len(d.Roles))
	for i := range d.Roles {
		paths[i] = inheritancePaths(i, d.Roles, roleIndexes)
	}
	s := &Snapshot{
		tenantID: d.TenantID,
		subjects: make(map[string][]compiledMembership, len(d.Subjects)),
		rules:    make(map[string][]compiledRule),
	}
	for _, subject := range d.Subjects {
		if _, exists := s.subjects[subject.ID]; exists {
			return nil, invalid("duplicate subject id")
		}
		memberships := make([]compiledMembership, 0, len(subject.Memberships))
		for _, member := range subject.Memberships {
			index, exists := roleIndexes[member.RoleID]
			if !exists {
				return nil, invalid("unknown membership role")
			}
			memberships = append(memberships, compiledMembership{member, paths[index]})
			s.addBoundaries(member.NotBefore, member.ExpiresAt)
		}
		s.subjects[subject.ID] = memberships
	}
	var previous string
	for _, rule := range d.Rules {
		if rule.ID == previous {
			return nil, invalid("duplicate rule id")
		}
		previous = rule.ID
		index := -1
		if rule.SubjectID != "" {
			if _, exists := s.subjects[rule.SubjectID]; !exists {
				return nil, invalid("unknown rule subject")
			}
		} else {
			var exists bool
			index, exists = roleIndexes[rule.RoleID]
			if !exists {
				return nil, invalid("unknown rule role")
			}
		}
		s.rules[rule.Action] = append(s.rules[rule.Action], compiledRule{rule, index})
		s.addBoundaries(rule.NotBefore, rule.ExpiresAt)
	}
	slices.SortFunc(s.boundaries, time.Time.Compare)
	s.boundaries = slices.CompactFunc(s.boundaries, time.Time.Equal)
	encoded, err := json.Marshal(d)
	if err != nil {
		return nil, invalid("document cannot be encoded")
	}
	if len(encoded) > MaxDocumentBytes {
		return nil, invalid("document size limit exceeded")
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("metallic-network-policy-v1\x00"))
	_, _ = hash.Write(encoded)
	s.version = Version{Epoch: d.Epoch, Revision: d.Revision}
	copy(s.version.Digest[:], hash.Sum(nil))
	return s, nil
}

// Evaluate denies on invalid input, version mismatch, or absence of an explicit
// allow. A matching deny always wins. Among deciding rules the lowest rule ID
// wins; inherited explanations use the shortest, then lexicographically first,
// active path. Context values, rule IDs, and iteration order cannot add authority.
func (s *Snapshot) Evaluate(r Request) Decision {
	if s == nil {
		return Decision{Code: "uninitialized"}
	}
	d := Decision{Version: s.version}
	if ValidateRequest(r) != nil {
		d.Code = "invalid_request"
		return d
	}
	if r.Expected != s.version {
		d.Code = "version_mismatch"
		return d
	}
	if r.TenantID != s.tenantID {
		d.Code = "tenant_mismatch"
		return d
	}
	memberships, exists := s.subjects[r.SubjectID]
	if !exists {
		d.Code = "unknown_subject"
		return d
	}
	rules, exists := s.rules[r.Action]
	if !exists {
		d.Code = "unknown_action"
		return d
	}
	if i := sort.Search(len(s.boundaries), func(i int) bool { return s.boundaries[i].After(r.Now) }); i < len(s.boundaries) {
		d.ValidUntil = s.boundaries[i]
	}
	var active [MaxRoles][]string
	for _, m := range memberships {
		if !inWindow(r.Now, m.membership.NotBefore, m.membership.ExpiresAt) || !matches(m.membership.Context, r.Context) {
			continue
		}
		for _, p := range m.paths {
			if betterPath(p.path, active[p.index]) {
				active[p.index] = p.path
			}
		}
	}
	var firstAllow *compiledRule
	for i := range rules {
		candidate := &rules[i]
		rule := &candidate.rule
		if rule.ResourceType != r.ResourceType || rule.ResourceID != r.ResourceID ||
			!inWindow(r.Now, rule.NotBefore, rule.ExpiresAt) || !matches(rule.Context, r.Context) {
			continue
		}
		if candidate.roleIndex < 0 {
			if rule.SubjectID != r.SubjectID {
				continue
			}
		} else if active[candidate.roleIndex] == nil {
			continue
		}
		if rule.Effect == Deny {
			d.Code = "explicit_deny"
			return decided(d, candidate, &active)
		}
		if firstAllow == nil {
			firstAllow = candidate
		}
	}
	if firstAllow != nil {
		d.Allowed, d.Code = true, "explicit_allow"
		return decided(d, firstAllow, &active)
	}
	d.Code = "no_matching_allow"
	return d
}

func decided(d Decision, rule *compiledRule, active *[MaxRoles][]string) Decision {
	d.RuleID = rule.rule.ID
	if rule.roleIndex >= 0 {
		d.InheritancePath = slices.Clone(active[rule.roleIndex])
	}
	return d
}

func betterPath(candidate, current []string) bool {
	return current == nil || len(candidate) < len(current) ||
		(len(candidate) == len(current) && slices.Compare(candidate, current) < 0)
}

func matches(required, actual map[string]string) bool {
	for key, value := range required {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func inWindow(now, start, end time.Time) bool {
	return (start.IsZero() || !now.Before(start)) && (end.IsZero() || now.Before(end))
}

func (s *Snapshot) addBoundaries(start, end time.Time) {
	if !start.IsZero() {
		s.boundaries = append(s.boundaries, start)
	}
	if !end.IsZero() {
		s.boundaries = append(s.boundaries, end)
	}
}

func validateGraph(roles []Role, indexes map[string]int) error {
	var state [MaxRoles]uint8
	var depth [MaxRoles]int
	var visit func(int) error
	visit = func(i int) error {
		if state[i] == 1 {
			return invalid("role graph contains a cycle")
		}
		if state[i] == 2 {
			return nil
		}
		state[i], depth[i] = 1, 1
		for _, parent := range roles[i].Parents {
			p := indexes[parent]
			if err := visit(p); err != nil {
				return err
			}
			depth[i] = max(depth[i], depth[p]+1)
		}
		if depth[i] > MaxDepth {
			return invalid("role graph depth limit exceeded")
		}
		state[i] = 2
		return nil
	}
	for i := range roles {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}

// Sorted breadth-first traversal records one shortest/lexicographic witness per
// reachable role, avoiding exponential enumeration of diamond-shaped graphs.
func inheritancePaths(start int, roles []Role, indexes map[string]int) []rolePath {
	paths := make([][]string, len(roles))
	paths[start] = []string{roles[start].ID}
	queue := []int{start}
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		for _, name := range roles[current].Parents {
			parent := indexes[name]
			if paths[parent] != nil {
				continue
			}
			paths[parent] = append(slices.Clone(paths[current]), name)
			queue = append(queue, parent)
		}
	}
	result := make([]rolePath, 0, len(queue))
	for i, path := range paths {
		if path != nil {
			result = append(result, rolePath{i, path})
		}
	}
	return result
}

type byteBudget int

func (b *byteBudget) add(value string) bool {
	*b += byteBudget(len(value))
	return *b <= MaxDocumentBytes
}

func canonicalDocument(input Document) (Document, error) {
	if input.Schema != SchemaVersion || input.Revision == 0 || !validID(input.Epoch) || !validID(input.TenantID) {
		return Document{}, invalid("schema, epoch, revision or tenant")
	}
	if len(input.Roles) > MaxRoles || len(input.Subjects) > MaxSubjects || len(input.Rules) > MaxRules {
		return Document{}, invalid("document item count limit exceeded")
	}
	d := Document{Schema: input.Schema, Epoch: input.Epoch, Revision: input.Revision, TenantID: input.TenantID,
		Roles: make([]Role, len(input.Roles)), Subjects: make([]Subject, len(input.Subjects)), Rules: make([]Rule, len(input.Rules))}
	var budget byteBudget
	budget.add(input.Epoch)
	budget.add(input.TenantID)
	for i, role := range input.Roles {
		if !validID(role.ID) || len(role.Parents) > MaxParents || !budget.add(role.ID) {
			return Document{}, invalid("invalid role or role bound")
		}
		parents := make([]string, len(role.Parents))
		for j, parent := range role.Parents {
			if !validID(parent) || !budget.add(parent) {
				return Document{}, invalid("invalid parent or document size")
			}
			parents[j] = parent
		}
		slices.Sort(parents)
		for j := 1; j < len(parents); j++ {
			if parents[j] == parents[j-1] {
				return Document{}, invalid("duplicate parent")
			}
		}
		d.Roles[i] = Role{ID: role.ID, Parents: parents}
	}
	for i, subject := range input.Subjects {
		if !validID(subject.ID) || len(subject.Memberships) > MaxMemberships || !budget.add(subject.ID) {
			return Document{}, invalid("invalid subject or membership bound")
		}
		type keyedMembership struct {
			key    string
			member Membership
		}
		members := make([]keyedMembership, len(subject.Memberships))
		for j, member := range subject.Memberships {
			if !validID(member.RoleID) || !validWindow(member.NotBefore, member.ExpiresAt) || !budget.add(member.RoleID) {
				return Document{}, invalid("invalid membership")
			}
			context, err := copyContext(member.Context, &budget)
			if err != nil {
				return Document{}, err
			}
			m := Membership{member.RoleID, context, canonicalTime(member.NotBefore), canonicalTime(member.ExpiresAt)}
			encoded, err := json.Marshal(m)
			if err != nil {
				return Document{}, invalid("invalid membership encoding")
			}
			members[j] = keyedMembership{string(encoded), m}
		}
		slices.SortFunc(members, func(a, b keyedMembership) int { return strings.Compare(a.key, b.key) })
		d.Subjects[i] = Subject{ID: subject.ID, Memberships: make([]Membership, len(members))}
		for j, member := range members {
			if j > 0 && member.key == members[j-1].key {
				return Document{}, invalid("duplicate membership")
			}
			d.Subjects[i].Memberships[j] = member.member
		}
	}
	for i, rule := range input.Rules {
		if !validID(rule.ID) || !validID(rule.Action) || !validID(rule.ResourceType) || !validID(rule.ResourceID) ||
			(rule.SubjectID == "") == (rule.RoleID == "") || (rule.SubjectID != "" && !validID(rule.SubjectID)) ||
			(rule.RoleID != "" && !validID(rule.RoleID)) || (rule.Effect != Allow && rule.Effect != Deny) ||
			!validWindow(rule.NotBefore, rule.ExpiresAt) {
			return Document{}, invalid("invalid rule")
		}
		for _, value := range []string{rule.ID, rule.SubjectID, rule.RoleID, rule.Action, rule.ResourceType, rule.ResourceID} {
			if !budget.add(value) {
				return Document{}, invalid("document size limit exceeded")
			}
		}
		context, err := copyContext(rule.Context, &budget)
		if err != nil {
			return Document{}, err
		}
		rule.Context = context
		rule.NotBefore, rule.ExpiresAt = canonicalTime(rule.NotBefore), canonicalTime(rule.ExpiresAt)
		d.Rules[i] = rule
	}
	slices.SortFunc(d.Roles, func(a, b Role) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(d.Subjects, func(a, b Subject) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(d.Rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	return d, nil
}

func copyContext(input map[string]string, budget *byteBudget) (map[string]string, error) {
	if !validContext(input) {
		return nil, invalid("invalid context")
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		if !budget.add(key) || !budget.add(value) {
			return nil, invalid("document size limit exceeded")
		}
		result[key] = value
	}
	return result, nil
}

func validContext(context map[string]string) bool {
	if len(context) > MaxContextPairs {
		return false
	}
	for key, value := range context {
		if !validID(key) || len(value) == 0 || len(value) > MaxContextValue || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
			return false
		}
		for _, r := range value {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return false
			}
		}
	}
	return true
}

// Identifiers use a deliberately small, case-sensitive ASCII grammar. Separators
// have no wildcard, hierarchy, filesystem, or matching semantics in this package.
func validID(value string) bool {
	if len(value) == 0 || len(value) > MaxIdentifier {
		return false
	}
	for i := range len(value) {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		if i == 0 || (c != '.' && c != '_' && c != ':' && c != '/' && c != '-') {
			return false
		}
	}
	return true
}

func canonicalTime(t time.Time) time.Time { return t.UTC().Round(0) }

func validTime(t time.Time) bool {
	if t.IsZero() {
		return true
	}
	t = canonicalTime(t)
	return t.Year() >= 1970 && t.Year() <= 9999
}

func validWindow(start, end time.Time) bool {
	return validTime(start) && validTime(end) && (start.IsZero() || end.IsZero() || start.Before(end))
}
