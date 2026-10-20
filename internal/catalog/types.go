// Package catalog inspects plugin metadata. It never downloads or executes artifacts.
package catalog

import "fmt"

type Provider string

const (
	Hangar   Provider = "hangar"
	Modrinth Provider = "modrinth"
)

type Config struct{ UserAgent string }
type Target struct {
	MinecraftVersion string `json:"minecraft_version"`
	Platform         string `json:"platform"`
}
type SearchRequest struct {
	Provider Provider `json:"provider"`
	Query    string   `json:"query"`
	Target   Target   `json:"target"`
	Limit    int      `json:"limit"`
}
type ResolveRequest struct {
	Provider  Provider `json:"provider"`
	ProjectID string   `json:"project_id"`
	VersionID string   `json:"version_id"`
	Target    Target   `json:"target"`
}
type SearchHit struct {
	Provider    Provider `json:"provider"`
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	URL         string   `json:"url"`
}
type SearchResult struct {
	Provider Provider    `json:"provider"`
	Hits     []SearchHit `json:"hits"`
}
type Artifact struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256,omitempty"`
	SHA512   string `json:"sha512,omitempty"`
}
type Dependency struct {
	ProjectID string `json:"project_id,omitempty"`
	VersionID string `json:"version_id,omitempty"`
	Type      string `json:"type"`
}
type Blocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Plan struct {
	Provider     Provider     `json:"provider"`
	ProjectID    string       `json:"project_id"`
	VersionID    string       `json:"version_id"`
	VersionName  string       `json:"version_name"`
	Target       Target       `json:"target"`
	Artifact     *Artifact    `json:"artifact"`
	Dependencies []Dependency `json:"dependencies"`
	Blockers     []Blocker    `json:"blockers"`
	// Installable means metadata checks passed, not that bytes were downloaded,
	// hashes verified against bytes, or an installation was performed.
	Installable bool `json:"installable"`
}
type UpstreamError struct {
	Provider Provider
	Status   int
	Reason   string
}

func (e *UpstreamError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s catalog: HTTP %d (%s)", e.Provider, e.Status, e.Reason)
	}
	return fmt.Sprintf("%s catalog: %s", e.Provider, e.Reason)
}
func (p *Plan) block(code, message string) {
	p.Blockers = append(p.Blockers, Blocker{Code: code, Message: message})
}
func (p *Plan) finish() { p.Installable = p.Artifact != nil && len(p.Blockers) == 0 }
