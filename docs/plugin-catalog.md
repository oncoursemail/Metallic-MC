# Plugin catalog inspection

`internal/catalog` searches Hangar and Modrinth and inspects exact releases for a specified Minecraft version and server platform. It uses only the Go standard library and never downloads JARs, writes plugin files, executes plugins, installs dependencies or mutates providers.

## Contract

Create a client using `NewClient(Config{UserAgent: "MetallicArchitect/0.2 (+https://github.com/oncoursemail/metallic-mc)"})`. The identifying product/version and contact URL/email are mandatory.

`Search(ctx, SearchRequest)` returns candidate projects. `Resolve(ctx, ResolveRequest)` checks an exact Hangar version name/ID or Modrinth version ID. `Target` carries `minecraft_version` and `platform` (paper or folia). JSON uses snake_case.

A plan contains provider, project/version, target, at most one artifact, dependency records, blockers and `installable`. An artifact has its URL, plain JAR filename, byte size and SHA-256 (Hangar) or SHA-512 (Modrinth).

**`installable: true` means supported metadata checks passed.** It does not establish verified downloaded bytes, trusted publisher/code, installed-plugin compatibility or completed installation. Invalid artifact metadata is omitted from the artifact field. A future installer must validate the actual bytes and current state.

## Requests and compatibility

| Provider | Search | Exact release |
| --- | --- | --- |
| Hangar | GET https://hangar.papermc.io/api/v1/projects with query, PAPER, exact game version, bounded limit | GET /api/v1/projects/{slugOrId}/versions/{nameOrId} |
| Modrinth | GET https://api.modrinth.com/v2/search with AND facets for all_project_types:plugin, exact platform category and game version | GET /v2/version/{id}, then GET /v2/project/{project_id} |

Identifiers are validated and escaped as separate path segments. Search text is query encoded. No API origin override or metadata-supplied request URL is accepted.

Current Modrinth v2 serializes plugin projects as `mod`, while its search index exposes the plugin kind through `all_project_types:plugin`. Filtering the search index by `project_type:mod` can omit valid plugins. This distinction was verified against live API results. A Paper/Folia search category identifies candidates; the exact version must also name that loader. Other loaders do not establish Paper compatibility, and Paper does not establish Folia compatibility.

Minecraft compatibility requires exact membership in the release's supported versions. Hangar PAPER metadata cannot establish Folia support, so Folia inspection produces a blocker.

Modrinth compatibility uses the selected version's `environment` string. Project environment arrays aggregate many versions and are not proof for an exact release. Recognized server-capable values are `server_only`, `dedicated_server_only`, `server_only_client_optional`, `client_and_server`, `client_or_server` and `client_or_server_prefers_both`. Unknown, missing, new, client-only and single-player-only values block. Legacy project side flags cannot override that result.

Required dependencies always produce `required_dependency_unresolved`. They are not recursively installed or assumed present. Incompatible Modrinth dependencies produce `incompatible_dependency_unchecked` until a real installed inventory exists. Optional/embedded dependencies are listed; absent dependency metadata or unknown dependency kinds block.

## Limits and supply-chain boundaries

- HTTPS metadata requests use fixed provider origins and an identifying User-Agent.
- Redirects and retries are disabled; no unbounded pagination.
- Each upstream operation has an eight-second deadline, including its concurrency wait; caller cancellation applies.
- At most four upstream operations run concurrently per client; transport connections are bounded.
- Responses are capped at 2 MiB, response headers at 32 KiB and search results at 25 (default 10).
- Approved artifact URLs are only `https://hangarcdn.papermc.io/plugins/…` and `https://cdn.modrinth.com/data/…`. Userinfo, explicit ports, fragments and traversal segments fail. Hangar external downloads fail.
- Plain JAR filenames and positive sizes up to 128 MiB are required, together with valid strong provider digests.
- Modrinth must identify one primary file or supply a single file; ambiguous sets fail.

Upstream failures return bounded errors without upstream bodies. The catalog has no malware detector, signature verifier, archive extractor, installed-inventory reconciliation or filesystem executor. A checksum alone is not a sandbox.

The current UI requires an exact release name/ID for inspection; automatic version selection and dependency resolution are later work. Provider rate limits and metadata can change. A public multi-user rollout additionally needs shared caching, request scheduling and observed backoff policy.

## Verification and references

Run `GOMAXPROCS=2 go test -p 2 -race ./internal/catalog`. Deterministic fixtures cover paths/queries, origins, headers, redirects, malformed/oversized responses, concurrency/cancellation, exact identity, Minecraft/loader/environment compatibility, dependencies, ambiguous files, unsafe names/URLs, sizes and digests. Unexpected artifact requests fail the fixture transport.

- [Hangar public OpenAPI](https://hangar.papermc.io/v3/api-docs/public)
- [Modrinth search](https://docs.modrinth.com/api/operations/searchprojects/)
- [Modrinth exact version](https://docs.modrinth.com/api/operations/getversion/)
- [Modrinth project](https://docs.modrinth.com/api/operations/getproject/)
