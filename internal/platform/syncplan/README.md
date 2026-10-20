# Managed world sync planning

`syncplan.Plan(request, trustedContext)` produces deterministic review data for a future local-to-managed world import. It does no file I/O, extraction, upload, hashing of file contents, Minecraft launch, container admission, or deployment. Every successful result contains `executable:false` and unresolved execution blockers.

## Caller boundary

Decode only `Request` from user JSON. Supply `TrustedContext` separately from authenticated controller records. All context fields use `json:"-"` so JSON decoding cannot populate them. An HTTP integration should also reject unknown request fields and limit its body size and concurrent planning work.

The caller supplies the required platform policy revision, the revision actually accepted by the relevant owner, the authoritative destination generation, and a stopped/coherent source-checkpoint attestation. These are not proofs supplied by this package. The Minecraft EULA is separate from the platform policy acceptance record; this planner does not ask for or infer a new EULA acceptance.

The requested base generation must exactly equal the trusted destination generation. A conflict returns `ErrConflict`. No generation is reserved, advanced, or persisted. A future promoter must recheck the generation transactionally and bind the operation to the authenticated tenant, server, source checkpoint, approved profile, and a single-use operation ID. This plan's digest is not an authorization capability and is not bound to a server identity by itself.

## Request format

| Field | Accepted values |
|---|---|
| `target_mode` | `managed-private` or `managed-public` |
| `source_layout` | `java-pre26` or `java-26` |
| `base_remote_generation` | Unsigned integer matching the trusted destination value |
| `world_roots` | One to three distinct values chosen from `world`, `world_nether`, `world_the_end` |
| `entries` | Bounded inventory of `path`, `kind`, `size`, and `sha256` |
| Entry `kind` | `file` or `directory` |
| File `sha256` | Exactly 64 lowercase hexadecimal characters |
| Directory metadata | `size:0`, `sha256:""` |

The two layout labels declare the older Java layout and the storage layout introduced by Java 26.1. This package does not read `level.dat` or determine a Minecraft version, and neither label automatically certifies every future version. Actual layout and world completeness remain an explicit verification gate. Bedrock, arbitrary custom world roots, plugin imports, local-public tunnels, and automatic layout guessing are unsupported in v1.

Within declared world roots, all eligible descendants are retained. This includes old `region`, `DIM-1`, `DIM1`, and `playerdata` paths and current `dimensions`, `players`, namespaced `data`, and embedded `resourcepacks` paths. It also retains custom dimension namespaces and datapacks. It does not copy only a historical list of world subfolders. Java 26.1 changed those storage paths; see the official [26.1 release notes](https://www.minecraft.net/en-us/article/minecraft-java-edition-26-1).

## Validation and limits

The planner accepts at most 8,192 entries, a 512-byte path, 32 path components, and 128 bytes per component. Each component uses ASCII letters, digits, `.`, `_`, or `-`. Files may claim at most 64 GiB each and the full inventory may claim at most 512 GiB. These are validation ceilings, not a hosting storage entitlement. The byte limit includes excluded files.

Paths are not cleaned or normalized into acceptance. Absolute paths, traversal, empty components, trailing separators/dots, spaces, backslashes, drive letters, alternate data stream colons, Unicode, Windows device names, and short-name alias syntax are rejected. All inventory entries are validated, including entries later excluded. Duplicate paths, case collisions in explicit or implicit directory names, and file/directory prefix collisions are rejected independent of input order. Symlinks, hardlinks, sockets, devices, FIFOs, and unknown kinds are rejected.

Known private/control directories, runtime/plugin directories, transient logs/backups, managed server configuration, source lock files, known credential basenames, key-store extensions, and executable artifacts are excluded even when nested under a world. Files outside the declared world roots are excluded. The result explains each exclusion rather than silently passing it to the executor. An inventory containing no eligible world files is rejected.

These name checks are **not secret detection or malware scanning**. A credential could be inside an innocuous JSON/NBT file, a plugin could be renamed, or a ZIP could contain unsafe paths, executable content, or a decompression bomb. No archive is opened here. Content inspection, per-file hash/length verification, quota enforcement, archive extraction safeguards, and the source's actual file types remain mandatory downstream. The planner cannot prove that the manifest includes every world file or that it came from the asserted source.

## Result and digest

The result contains canonical sorted roots, included entries, excluded entries with reasons, included/excluded byte totals, policy/checkpoint identifiers, and blockers. The SHA-256 digest hashes the canonical JSON representation of the result with the `digest` field set to an empty string. Sorting makes equivalent inventory/root permutations produce identical results. Returned slices are detached from input slices.

Every result retains these blockers:

- `upload-content-verification-required`
- `world-snapshot-verification-required`
- `managed-compatibility-profile-required`
- `isolated-runtime-admission-required`
- `atomic-promotion-required`

Missing or outdated platform acceptance adds `current-policy-acceptance-required`. Missing checkpoint identity, a running source, or an unattested coherent snapshot adds `stopped-coherent-checkpoint-required`. Public mode adds `public-discovery-review-required`. Even a trusted caller with all context fields populated cannot remove the five execution blockers through this API.

The future managed release process must create new server configuration, runtime/compatibility artifacts, and hosted secrets. It must preserve a stopped coherent world snapshot, stage the upload, independently validate it, admit an isolated runtime, and promote with a generation comparison. It must not merge two running copies of the same world. File-based persistence of a required plugin does not guarantee its hooks remain active beside arbitrary untrusted code in the same JVM.

## Verification

Tests cover modern and legacy world trees, nested exclusions, exact generation conflicts, independent trust evidence, path/link attacks, inferred directory case collisions, file-prefix collisions in both orders, canonical digest binding, detached results, byte/count bounds, concurrent read-only planning, and a bounded path/metadata fuzz target.

```sh
go test -race -count=1 -p=2 ./internal/platform/syncplan
go vet ./internal/platform/syncplan
go test -run='^$' -fuzz=FuzzInventoryPath -fuzztime=5s -parallel=2 ./internal/platform/syncplan
```

Passing these tests establishes the behavior of this metadata planner. It does not establish successful Minecraft sync, launcher sign-in, managed hosting isolation, compatible gameplay, or a restored world.
