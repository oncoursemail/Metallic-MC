package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
)

type hangarNamespace struct {
	Owner string `json:"owner"`
	Slug  string `json:"slug"`
}
type hangarProject struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Namespace   hangarNamespace `json:"namespace"`
}
type hangarDependency struct {
	Name      string           `json:"name"`
	Required  *bool            `json:"required"`
	Namespace *hangarNamespace `json:"namespace"`
}
type hangarVersion struct {
	ID                   json.RawMessage               `json:"id"`
	Name                 string                        `json:"name"`
	PlatformDependencies map[string][]string           `json:"platformDependencies"`
	PluginDependencies   map[string][]hangarDependency `json:"pluginDependencies"`
	Downloads            map[string]struct {
		DownloadURL string `json:"downloadUrl"`
		ExternalURL string `json:"externalUrl"`
		FileInfo    *struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256Hash"`
			Size   int64  `json:"sizeBytes"`
		} `json:"fileInfo"`
	} `json:"downloads"`
}

func (c *Client) searchHangar(ctx context.Context, req SearchRequest) (SearchResult, error) {
	out := SearchResult{Provider: Hangar, Hits: []SearchHit{}}
	if req.Target.Platform != "paper" {
		return out, errors.New("Hangar PAPER metadata cannot establish Folia compatibility")
	}
	q := url.Values{"query": {req.Query}, "platform": {"PAPER"}, "version": {req.Target.MinecraftVersion}, "limit": {strconv.Itoa(req.Limit)}, "offset": {"0"}}
	var raw struct {
		Result []hangarProject `json:"result"`
	}
	if err := c.get(ctx, Hangar, "/api/v1/projects", q, &raw); err != nil {
		return out, err
	}
	if raw.Result == nil {
		return out, &UpstreamError{Provider: Hangar, Reason: "search response is missing its result array"}
	}
	for i, hit := range raw.Result {
		if i >= req.Limit {
			break
		}
		if !validID(hit.Namespace.Slug) || !validID(hit.Namespace.Owner) || hit.Name == "" {
			return out, &UpstreamError{Provider: Hangar, Reason: "search result has invalid project identity"}
		}
		out.Hits = append(out.Hits, SearchHit{Provider: Hangar, ProjectID: hit.Namespace.Slug, Slug: hit.Namespace.Slug, Name: hit.Name, Description: hit.Description, Author: hit.Namespace.Owner, URL: "https://hangar.papermc.io/" + url.PathEscape(hit.Namespace.Owner) + "/" + url.PathEscape(hit.Namespace.Slug)})
	}
	return out, nil
}
func hangarID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return strconv.FormatInt(n, 10)
	}
	return ""
}
func (c *Client) resolveHangar(ctx context.Context, req ResolveRequest, p Plan) (Plan, error) {
	if req.Target.Platform != "paper" {
		p.block("platform_unverified", "Hangar PAPER metadata does not prove Folia support; use explicit Folia metadata.")
		return p, nil
	}
	var v hangarVersion
	apiPath := "/api/v1/projects/" + url.PathEscape(req.ProjectID) + "/versions/" + url.PathEscape(req.VersionID)
	if err := c.get(ctx, Hangar, apiPath, nil, &v); err != nil {
		return p, err
	}
	if !validID(v.Name) {
		return p, &UpstreamError{Provider: Hangar, Reason: "version response is missing a valid exact version name"}
	}
	id := hangarID(v.ID)
	p.VersionName = v.Name
	p.VersionID = v.Name
	if id != "" {
		p.VersionID = id
	}
	if req.VersionID != v.Name && req.VersionID != id {
		p.block("version_mismatch", "The upstream version does not match the exact requested version.")
	}
	if !has(v.PlatformDependencies["PAPER"], req.Target.MinecraftVersion) {
		p.block("minecraft_version_mismatch", "This release does not explicitly support the target Minecraft version on Paper.")
	}
	if v.PluginDependencies == nil {
		p.block("dependency_metadata_missing", "The release did not provide dependency metadata.")
	}
	for _, dep := range v.PluginDependencies["PAPER"] {
		typ := "optional"
		if dep.Required == nil {
			p.block("dependency_metadata_invalid", "A dependency does not declare whether it is required.")
		} else if *dep.Required {
			typ = "required"
		}
		project := dep.Name
		if dep.Namespace != nil && dep.Namespace.Slug != "" {
			project = dep.Namespace.Slug
		}
		p.Dependencies = append(p.Dependencies, Dependency{ProjectID: project, Type: typ})
		if typ == "required" {
			p.block("required_dependency_unresolved", "A required Hangar dependency must be resolved to a compatible exact artifact before installation: "+project)
		}
	}
	download, ok := v.Downloads["PAPER"]
	if !ok {
		p.block("artifact_missing", "This release has no Paper artifact.")
	} else if download.ExternalURL != "" {
		p.block("external_download", "External Hangar downloads are not supported by this inspection plan.")
	} else if download.FileInfo == nil {
		p.block("artifact_metadata_missing", "The release has no file metadata for its Paper artifact.")
	} else {
		p.setArtifact(Artifact{URL: download.DownloadURL, Filename: download.FileInfo.Name, Size: download.FileInfo.Size, SHA256: download.FileInfo.SHA256})
	}
	p.finish()
	return p, nil
}
