package catalog

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

type modrinthFile struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Size     int64             `json:"size"`
	Primary  bool              `json:"primary"`
	Hashes   map[string]string `json:"hashes"`
}
type modrinthVersion struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"`
	Name          string         `json:"name"`
	VersionNumber string         `json:"version_number"`
	Environment   string         `json:"environment"`
	GameVersions  []string       `json:"game_versions"`
	Loaders       []string       `json:"loaders"`
	Files         []modrinthFile `json:"files"`
	Dependencies  []struct {
		ProjectID string `json:"project_id"`
		VersionID string `json:"version_id"`
		Type      string `json:"dependency_type"`
	} `json:"dependencies"`
}
type modrinthProject struct {
	ID          string `json:"id"`
	ProjectType string `json:"project_type"`
}

func (c *Client) searchModrinth(ctx context.Context, req SearchRequest) (SearchResult, error) {
	out := SearchResult{Provider: Modrinth, Hits: []SearchHit{}}
	// Modrinth v2 serializes plugin projects as mod, but its search index
	// exposes plugin through all_project_types. Resolve verifies exact loaders.
	facets, err := json.Marshal([][]string{{"all_project_types:plugin"}, {"categories:" + req.Target.Platform}, {"versions:" + req.Target.MinecraftVersion}})
	if err != nil {
		return out, err
	}
	q := url.Values{"query": {req.Query}, "facets": {string(facets)}, "limit": {strconv.Itoa(req.Limit)}, "offset": {"0"}}
	var raw struct {
		Hits []struct {
			ProjectID   string   `json:"project_id"`
			Slug        string   `json:"slug"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Author      string   `json:"author"`
			ProjectType string   `json:"project_type"`
			Categories  []string `json:"categories"`
		} `json:"hits"`
	}
	if err := c.get(ctx, Modrinth, "/v2/search", q, &raw); err != nil {
		return out, err
	}
	if raw.Hits == nil {
		return out, &UpstreamError{Provider: Modrinth, Reason: "search response is missing its hits array"}
	}
	for i, hit := range raw.Hits {
		if i >= req.Limit {
			break
		}
		if !validID(hit.ProjectID) || !validID(hit.Slug) || hit.Title == "" || hit.ProjectType != "mod" || !has(hit.Categories, req.Target.Platform) {
			return out, &UpstreamError{Provider: Modrinth, Reason: "search returned invalid plugin identity or loader category"}
		}
		out.Hits = append(out.Hits, SearchHit{Provider: Modrinth, ProjectID: hit.ProjectID, Slug: hit.Slug, Name: hit.Title, Description: hit.Description, Author: hit.Author, URL: "https://modrinth.com/plugin/" + url.PathEscape(hit.Slug)})
	}
	return out, nil
}
func serverEnvironment(environment string) bool {
	switch environment {
	case "server_only", "dedicated_server_only", "server_only_client_optional", "client_and_server", "client_or_server", "client_or_server_prefers_both":
		return true
	default:
		return false
	}
}
func (c *Client) resolveModrinth(ctx context.Context, req ResolveRequest, p Plan) (Plan, error) {
	var v modrinthVersion
	if err := c.get(ctx, Modrinth, "/v2/version/"+url.PathEscape(req.VersionID), nil, &v); err != nil {
		return p, err
	}
	if !validID(v.ID) || !validID(v.ProjectID) || v.VersionNumber == "" {
		return p, &UpstreamError{Provider: Modrinth, Reason: "version response is missing its identity"}
	}
	p.ProjectID, p.VersionID, p.VersionName = v.ProjectID, v.ID, v.VersionNumber
	if req.VersionID != v.ID {
		p.block("version_mismatch", "The upstream version does not match the exact requested version ID.")
	}
	if req.ProjectID != "" && req.ProjectID != v.ProjectID {
		p.block("project_mismatch", "The version belongs to a different project than requested.")
	}
	if !has(v.GameVersions, req.Target.MinecraftVersion) {
		p.block("minecraft_version_mismatch", "This release does not explicitly support the target Minecraft version.")
	}
	if !has(v.Loaders, req.Target.Platform) {
		p.block("platform_mismatch", "This release does not explicitly support the target server platform; Paper does not imply Folia.")
	}
	if !serverEnvironment(v.Environment) {
		p.block("server_environment_unverified", "The exact version does not declare a supported server environment; project-level aggregates cannot establish compatibility.")
	}
	var project modrinthProject
	if err := c.get(ctx, Modrinth, "/v2/project/"+url.PathEscape(v.ProjectID), nil, &project); err != nil {
		return p, err
	}
	if project.ID != v.ProjectID {
		p.block("project_mismatch", "Project metadata does not match this version's project ID.")
	}
	if project.ProjectType != "mod" {
		p.block("project_type_mismatch", "The project is not a Modrinth mod/plugin project.")
	}
	if v.Dependencies == nil {
		p.block("dependency_metadata_missing", "The release did not provide dependency metadata.")
	}
	for _, dep := range v.Dependencies {
		p.Dependencies = append(p.Dependencies, Dependency{ProjectID: dep.ProjectID, VersionID: dep.VersionID, Type: dep.Type})
		switch dep.Type {
		case "required":
			p.block("required_dependency_unresolved", "A required Modrinth dependency must be resolved to a compatible exact artifact before installation: "+dep.ProjectID+" "+dep.VersionID)
		case "incompatible":
			p.block("incompatible_dependency_unchecked", "An incompatible dependency must be checked against the server's installed plugins before installation.")
		case "optional", "embedded":
		default:
			p.block("dependency_type_unknown", "A dependency has an unrecognized dependency type.")
		}
	}
	selected, primaryCount := -1, 0
	for i, file := range v.Files {
		if file.Primary {
			selected = i
			primaryCount++
		}
	}
	if primaryCount == 0 && len(v.Files) == 1 {
		selected = 0
	}
	if selected < 0 || primaryCount > 1 {
		p.block("artifact_ambiguous", "The release must identify exactly one primary artifact, or provide exactly one file.")
	} else {
		f := v.Files[selected]
		p.setArtifact(Artifact{URL: f.URL, Filename: f.Filename, Size: f.Size, SHA256: f.Hashes["sha256"], SHA512: f.Hashes["sha512"]})
	}
	p.finish()
	return p, nil
}
