package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

var hangarFixture = `{"id":421,"name":"1.2.3","createdAt":"2025-01-01T00:00:00Z","channel":{"name":"Release"},"platformDependencies":{"PAPER":["1.21.4"]},"pluginDependencies":{"PAPER":[]},"downloads":{"PAPER":{"downloadUrl":"https://hangarcdn.papermc.io/plugins/Author/TinyPlugin/versions/1.2.3/PAPER/TinyPlugin-1.2.3.jar","externalUrl":null,"fileInfo":{"name":"TinyPlugin-1.2.3.jar","sha256Hash":"` + strings.Repeat("a", 64) + `","sizeBytes":123456}}}}`
var modrinthFixture = `{"id":"version42","project_id":"project42","name":"Tiny Plugin 1.2.3","version_number":"1.2.3","version_type":"release","environment":"server_only","game_versions":["1.21.4"],"loaders":["paper"],"dependencies":[],"files":[{"url":"https://cdn.modrinth.com/data/project42/versions/version42/TinyPlugin-1.2.3.jar","filename":"TinyPlugin-1.2.3.jar","primary":true,"size":123456,"hashes":{"sha512":"` + strings.Repeat("b", 128) + `","sha1":"` + strings.Repeat("c", 40) + `"}}]}`

const modrinthProjectFixture = `{"id":"project42","slug":"tiny-plugin","project_type":"mod","environment":["server_only"],"client_side":"unsupported","server_side":"required","title":"Tiny Plugin"}`

func changedJSON(t *testing.T, source string, change func(map[string]any)) string {
	t.Helper()
	var raw map[string]any
	if e := json.Unmarshal([]byte(source), &raw); e != nil {
		t.Fatal(e)
	}
	change(raw)
	data, e := json.Marshal(raw)
	if e != nil {
		t.Fatal(e)
	}
	return string(data)
}
func downloadInfo(m map[string]any) map[string]any {
	return m["downloads"].(map[string]any)["PAPER"].(map[string]any)
}
func modFile(m map[string]any) map[string]any { return m["files"].([]any)[0].(map[string]any) }
func fixturePlan(t *testing.T, provider Provider, version, project string, target Target) Plan {
	t.Helper()
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Error("unexpected write")
		}
		switch r.URL.Path {
		case "/api/v1/projects/TinyPlugin/versions/1.2.3", "/v2/version/version42":
			return jsonResponse(200, version), nil
		case "/v2/project/project42":
			return jsonResponse(200, project), nil
		default:
			t.Errorf("unexpected endpoint; artifact must never be fetched: %s", r.URL)
			return jsonResponse(404, `{}`), nil
		}
	})
	req := ResolveRequest{Provider: provider, ProjectID: "project42", VersionID: "version42", Target: target}
	if provider == Hangar {
		req.ProjectID, req.VersionID = "TinyPlugin", "1.2.3"
	}
	p, e := c.Resolve(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestResolveExactArtifact(t *testing.T) {
	for _, provider := range []Provider{Hangar, Modrinth} {
		t.Run(string(provider), func(t *testing.T) {
			v := modrinthFixture
			if provider == Hangar {
				v = hangarFixture
			}
			p := fixturePlan(t, provider, v, modrinthProjectFixture, paperTarget())
			if !p.Installable || len(p.Blockers) != 0 || p.Artifact == nil || p.Artifact.Filename != "TinyPlugin-1.2.3.jar" || p.Artifact.Size != 123456 {
				t.Fatalf("plan %+v", p)
			}
			if provider == Hangar && len(p.Artifact.SHA256) != 64 || provider == Modrinth && len(p.Artifact.SHA512) != 128 {
				t.Fatal("missing strong hash")
			}
		})
	}
}
func TestHangarBlockers(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(map[string]any)
	}{
		{"version", "version_mismatch", func(m map[string]any) { m["name"] = "1.2.4" }},
		{"minecraft", "minecraft_version_mismatch", func(m map[string]any) { m["platformDependencies"] = map[string]any{"PAPER": []string{"1.21.3"}} }},
		{"missing_artifact", "artifact_missing", func(m map[string]any) { m["downloads"] = map[string]any{} }},
		{"external", "external_download", func(m map[string]any) { downloadInfo(m)["externalUrl"] = "https://example.com/plugin.jar" }},
		{"hash", "artifact_hash_invalid", func(m map[string]any) { downloadInfo(m)["fileInfo"].(map[string]any)["sha256Hash"] = "bad" }},
		{"url", "artifact_url_untrusted", func(m map[string]any) { downloadInfo(m)["downloadUrl"] = "https://evil.example/plugin.jar" }},
		{"filename", "artifact_name_invalid", func(m map[string]any) { downloadInfo(m)["fileInfo"].(map[string]any)["name"] = "../plugin.jar" }},
		{"size", "artifact_size_invalid", func(m map[string]any) { downloadInfo(m)["fileInfo"].(map[string]any)["sizeBytes"] = -1 }},
		{"missing_dependencies", "dependency_metadata_missing", func(m map[string]any) { delete(m, "pluginDependencies") }},
		{"required_dependency", "required_dependency_unresolved", func(m map[string]any) {
			m["pluginDependencies"] = map[string]any{"PAPER": []any{map[string]any{"name": "Vault", "required": true, "namespace": map[string]any{"owner": "Author", "slug": "Vault"}}}}
		}},
		{"unknown_required_flag", "dependency_metadata_invalid", func(m map[string]any) {
			m["pluginDependencies"] = map[string]any{"PAPER": []any{map[string]any{"name": "Vault"}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixturePlan(t, Hangar, changedJSON(t, hangarFixture, tc.change), "", paperTarget())
			if p.Installable || !hasBlock(p, tc.code) {
				t.Fatalf("missing %s: %+v", tc.code, p)
			}
		})
	}
}
func TestModrinthBlockers(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(map[string]any)
	}{
		{"version", "version_mismatch", func(m map[string]any) { m["id"] = "otherVersion" }},
		{"minecraft", "minecraft_version_mismatch", func(m map[string]any) { m["game_versions"] = []string{"1.21.3"} }},
		{"fabric", "platform_mismatch", func(m map[string]any) { m["loaders"] = []string{"fabric"} }},
		{"hash", "artifact_hash_invalid", func(m map[string]any) { modFile(m)["hashes"] = map[string]any{"sha1": strings.Repeat("a", 40)} }},
		{"url", "artifact_url_untrusted", func(m map[string]any) { modFile(m)["url"] = "https://127.0.0.1/private.jar" }},
		{"filename", "artifact_name_invalid", func(m map[string]any) { modFile(m)["filename"] = "plugin.exe" }},
		{"size", "artifact_size_invalid", func(m map[string]any) { modFile(m)["size"] = maxArtifactBytes + 1 }},
		{"ambiguous", "artifact_ambiguous", func(m map[string]any) { f := modFile(m); m["files"] = []any{f, f} }},
		{"missing_dependencies", "dependency_metadata_missing", func(m map[string]any) { delete(m, "dependencies") }},
		{"required_dependency", "required_dependency_unresolved", func(m map[string]any) {
			m["dependencies"] = []any{map[string]any{"project_id": "depProject", "version_id": "depVersion", "dependency_type": "required"}}
		}},
		{"incompatible_dependency", "incompatible_dependency_unchecked", func(m map[string]any) {
			m["dependencies"] = []any{map[string]any{"project_id": "depProject", "dependency_type": "incompatible"}}
		}},
		{"unknown_dependency", "dependency_type_unknown", func(m map[string]any) {
			m["dependencies"] = []any{map[string]any{"project_id": "depProject", "dependency_type": "new_kind"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixturePlan(t, Modrinth, changedJSON(t, modrinthFixture, tc.change), modrinthProjectFixture, paperTarget())
			if p.Installable || !hasBlock(p, tc.code) {
				t.Fatalf("missing %s: %+v", tc.code, p)
			}
		})
	}
}
func TestExactVersionEnvironment(t *testing.T) {
	for _, tc := range []struct {
		environment string
		supported   bool
	}{{"server_only", true}, {"dedicated_server_only", true}, {"server_only_client_optional", true}, {"client_and_server", true}, {"client_or_server", true}, {"client_or_server_prefers_both", true}, {"client_only", false}, {"client_only_server_optional", false}, {"singleplayer_only", false}, {"unknown", false}, {"future_environment", false}, {"", false}} {
		t.Run(tc.environment, func(t *testing.T) {
			v := changedJSON(t, modrinthFixture, func(m map[string]any) { m["environment"] = tc.environment })
			p := fixturePlan(t, Modrinth, v, modrinthProjectFixture, paperTarget())
			if p.Installable != tc.supported {
				t.Fatalf("environment %q: %+v", tc.environment, p)
			}
			if !tc.supported && !hasBlock(p, "server_environment_unverified") {
				t.Fatal("missing environment blocker")
			}
		})
	}
}
func TestPaperDoesNotImplyFolia(t *testing.T) {
	target := Target{MinecraftVersion: "1.21.4", Platform: "folia"}
	p := fixturePlan(t, Modrinth, modrinthFixture, modrinthProjectFixture, target)
	if p.Installable || !hasBlock(p, "platform_mismatch") {
		t.Fatal("Paper-only accepted on Folia")
	}
	v := changedJSON(t, modrinthFixture, func(m map[string]any) { m["loaders"] = []string{"folia"} })
	p = fixturePlan(t, Modrinth, v, modrinthProjectFixture, target)
	if !p.Installable {
		t.Fatal("explicit Folia rejected")
	}
	p = fixturePlan(t, Hangar, hangarFixture, "", target)
	if p.Installable || !hasBlock(p, "platform_unverified") {
		t.Fatal("Hangar implies Folia")
	}
}
func TestProjectIdentityAndType(t *testing.T) {
	for _, field := range []string{"id", "project_type"} {
		project := changedJSON(t, modrinthProjectFixture, func(m map[string]any) { m[field] = "wrong" })
		p := fixturePlan(t, Modrinth, modrinthFixture, project, paperTarget())
		code := "project_mismatch"
		if field == "project_type" {
			code = "project_type_mismatch"
		}
		if p.Installable || !hasBlock(p, code) {
			t.Fatal("project mismatch accepted")
		}
	}
}
func TestOptionalAndEmbeddedDependencies(t *testing.T) {
	v := changedJSON(t, modrinthFixture, func(m map[string]any) {
		m["dependencies"] = []any{map[string]any{"project_id": "optionalProject", "dependency_type": "optional"}, map[string]any{"project_id": "embeddedProject", "dependency_type": "embedded"}}
	})
	p := fixturePlan(t, Modrinth, v, modrinthProjectFixture, paperTarget())
	if !p.Installable || len(p.Dependencies) != 2 {
		t.Fatal("optional/embedded dependencies")
	}
}
func TestHangarPathSegmentsAreEncoded(t *testing.T) {
	v := changedJSON(t, hangarFixture, func(m map[string]any) { m["name"] = "1.2+build 4" })
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.EscapedPath() != "/api/v1/projects/Tiny%20Plugin/versions/1.2+build%204" {
			t.Errorf("unencoded path %s", r.URL.EscapedPath())
		}
		return jsonResponse(200, v), nil
	})
	p, e := c.Resolve(context.Background(), ResolveRequest{Provider: Hangar, ProjectID: "Tiny Plugin", VersionID: "1.2+build 4", Target: paperTarget()})
	if e != nil || !p.Installable {
		t.Fatalf("encodedversion %+v %v", p, e)
	}
}
