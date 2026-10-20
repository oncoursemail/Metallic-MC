package syncplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

func fixtureEntry(path string) Entry {
	sum := sha256.Sum256([]byte("synthetic fixture"))
	return Entry{Path: path, Kind: File, Size: 17, SHA256: hex.EncodeToString(sum[:])}
}

func fixtureRequest() Request {
	return Request{
		TargetMode: ManagedPrivate, SourceLayout: Java26, BaseRemoteGeneration: 4,
		WorldRoots: []string{"world"},
		Entries:    []Entry{fixtureEntry("world/level.dat")},
	}
}

func fixtureContext() TrustedContext {
	return TrustedContext{
		RequiredPolicyRevision: "rules:2026-10-20", AcceptedPolicyRevision: "rules:2026-10-20",
		RemoteGeneration: 4, CheckpointID: "checkpoint:4", SourceStopped: true, CoherentCheckpoint: true,
	}
}

func mustPlan(t *testing.T, req Request, context TrustedContext) Result {
	t.Helper()
	result, err := Plan(req, context)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCurrentWorldTreeAndSensitiveExclusions(t *testing.T) {
	req := fixtureRequest()
	include := []string{
		"world/dimensions/minecraft/overworld/region/r.0.0.mca",
		"world/dimensions/minecraft/the_nether/entities/r.1.0.mca",
		"world/dimensions/metallic/arena/data/arena.dat",
		"world/players/data/1234.dat",
		"world/players/advancements/1234.json",
		"world/data/minecraft/scoreboard.dat",
		"world/datapacks/game/data/metallic/function/start.mcfunction",
		"world/resourcepacks/resources.zip",
	}
	exclude := []string{
		"server.properties", "eula.txt", "server.jar", "plugins/ViaVersion.jar",
		"world/session.lock", "world/data/credentials.json", "world/data/.env.local",
		"world/data/.ssh/id_rsa", "world/data/cert.KEY", "world/data/forwarding.secret",
		"world/plugins/Unexpected.JAR", "world/data/setup.sh",
		"world/data/logs/latest.log", "other/level.dat",
	}
	for _, path := range include {
		req.Entries = append(req.Entries, fixtureEntry(path))
	}
	for _, path := range exclude {
		req.Entries = append(req.Entries, fixtureEntry(path))
	}
	result := mustPlan(t, req, fixtureContext())
	if len(result.Entries) != 1+len(include) || len(result.Excluded) != len(exclude) {
		t.Fatalf("unexpected inclusion counts: %+v", result)
	}
	for _, entry := range result.Entries {
		if entry.Path != "world/level.dat" && !slices.Contains(include, entry.Path) {
			t.Fatalf("unexpected included entry: %s", entry.Path)
		}
	}
	for _, entry := range result.Excluded {
		if !slices.Contains(exclude, entry.Path) || entry.Reason == "" {
			t.Fatalf("unexpected exclusion: %+v", entry)
		}
	}
	if result.TotalBytes != 17*int64(len(include)+1) || result.ExcludedBytes != 17*int64(len(exclude)) {
		t.Fatalf("incorrect byte accounting: %+v", result)
	}
	if result.Executable || len(result.Blockers) != 5 {
		t.Fatalf("caller attestations must not satisfy execution gates: %+v", result)
	}
}

func TestLegacyWorldsAndExplicitDirectories(t *testing.T) {
	req := fixtureRequest()
	req.SourceLayout = JavaPre26
	req.WorldRoots = []string{"world_the_end", "world", "world_nether"}
	req.Entries = append(req.Entries,
		Entry{Path: "world", Kind: Directory},
		Entry{Path: "world/region", Kind: Directory},
		fixtureEntry("world/region/r.0.0.mca"),
		fixtureEntry("world/playerdata/1234.dat"),
		fixtureEntry("world/dimensions/custom/forest/region/r.0.0.mca"),
		fixtureEntry("world_nether/DIM-1/region/r.0.0.mca"),
		fixtureEntry("world_the_end/DIM1/region/r.0.0.mca"),
	)
	result := mustPlan(t, req, fixtureContext())
	if len(result.Entries) != len(req.Entries) || len(result.Excluded) != 0 {
		t.Fatalf("legacy tree was truncated: %+v", result)
	}
	if !reflect.DeepEqual(result.WorldRoots, []string{"world", "world_nether", "world_the_end"}) {
		t.Fatalf("roots not canonical: %v", result.WorldRoots)
	}
}

func TestCanonicalPlanAndDetachedOutput(t *testing.T) {
	req := fixtureRequest()
	req.WorldRoots = []string{"world_nether", "world"}
	req.Entries = append(req.Entries, fixtureEntry("plugins/ViaVersion.jar"), fixtureEntry("world_nether/level.dat"))
	original, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	first := mustPlan(t, req, fixtureContext())
	slices.Reverse(req.Entries)
	slices.Reverse(req.WorldRoots)
	second := mustPlan(t, req, fixtureContext())
	if first.Digest != second.Digest || !reflect.DeepEqual(first, second) {
		t.Fatal("input ordering changed the canonical result")
	}
	slices.Reverse(req.Entries)
	slices.Reverse(req.WorldRoots)
	after, err := json.Marshal(req)
	if err != nil || string(original) != string(after) {
		t.Fatal("Plan mutated its request")
	}
	digest := first.Digest
	first.Digest = ""
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("digest does not bind documented canonical result")
	}
	first.Entries[0].Path = "changed"
	first.Excluded[0].Path = "changed"
	first.WorldRoots[0] = "changed"
	if mustPlan(t, req, fixtureContext()).Digest != digest {
		t.Fatal("returned result aliases the request")
	}
	changed := fixtureContext()
	changed.CheckpointID = "checkpoint:5"
	if mustPlan(t, req, changed).Digest == digest {
		t.Fatal("checkpoint not bound to plan")
	}
	changed = fixtureContext()
	changed.AcceptedPolicyRevision = ""
	if mustPlan(t, req, changed).Digest == digest {
		t.Fatal("acceptance blockers not bound to plan")
	}
}

func TestPolicyCheckpointAndPublicGates(t *testing.T) {
	req := fixtureRequest()
	req.TargetMode = ManagedPublic
	context := fixtureContext()
	context.AcceptedPolicyRevision = "rules:old"
	context.SourceStopped = false
	result := mustPlan(t, req, context)
	for _, blocker := range []string{
		"current-policy-acceptance-required", "stopped-coherent-checkpoint-required",
		"upload-content-verification-required", "world-snapshot-verification-required",
		"managed-compatibility-profile-required", "isolated-runtime-admission-required",
		"atomic-promotion-required", "public-discovery-review-required",
	} {
		if !slices.Contains(result.Blockers, blocker) {
			t.Errorf("missing blocker %s", blocker)
		}
	}
	if result.Executable {
		t.Fatal("public planning cannot execute")
	}
	for _, mutate := range []func(*TrustedContext){
		func(c *TrustedContext) { c.CheckpointID = "" },
		func(c *TrustedContext) { c.SourceStopped = false },
		func(c *TrustedContext) { c.CoherentCheckpoint = false },
	} {
		context = fixtureContext()
		mutate(&context)
		if !slices.Contains(mustPlan(t, req, context).Blockers, "stopped-coherent-checkpoint-required") {
			t.Fatal("incomplete checkpoint attestation accepted")
		}
	}
}

func TestTrustedContextCannotBePopulatedFromJSON(t *testing.T) {
	var context TrustedContext
	payload := []byte("{\"RequiredPolicyRevision\":\"forged\",\"AcceptedPolicyRevision\":\"forged\",\"RemoteGeneration\":4,\"CheckpointID\":\"fake\",\"SourceStopped\":true,\"CoherentCheckpoint\":true,\"required_policy_revision\":\"forged\",\"accepted_policy_revision\":\"forged\",\"remote_generation\":4,\"checkpoint_id\":\"fake\",\"source_stopped\":true,\"coherent_checkpoint\":true}")
	if err := json.Unmarshal(payload, &context); err != nil {
		t.Fatal(err)
	}
	if context != (TrustedContext{}) {
		t.Fatalf("JSON populated trusted evidence: %+v", context)
	}
	context.RequiredPolicyRevision = "rules:2026-10-20"
	req := fixtureRequest()
	req.BaseRemoteGeneration = 0
	result := mustPlan(t, req, context)
	if !slices.Contains(result.Blockers, "current-policy-acceptance-required") ||
		!slices.Contains(result.Blockers, "stopped-coherent-checkpoint-required") {
		t.Fatal("untrusted JSON removed evidence gates")
	}
}

func TestRemoteGenerationMustMatch(t *testing.T) {
	req := fixtureRequest()
	for _, value := range []uint64{0, 3, 5, ^uint64(0)} {
		req.BaseRemoteGeneration = value
		if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrConflict) {
			t.Fatalf("generation %d: %v", value, err)
		}
	}
}

func TestUnsupportedModesLayoutsAndWorldRoots(t *testing.T) {
	mutations := map[string]func(*Request){
		"local":       func(r *Request) { r.TargetMode = "local-private" },
		"tunnel":      func(r *Request) { r.TargetMode = "local-public-tunnel" },
		"unknown":     func(r *Request) { r.SourceLayout = "guess-automatically" },
		"bedrock":     func(r *Request) { r.SourceLayout = "bedrock" },
		"empty-roots": func(r *Request) { r.WorldRoots = nil },
		"too-many":    func(r *Request) { r.WorldRoots = []string{"world", "world", "world", "world"} },
		"duplicate":   func(r *Request) { r.WorldRoots = []string{"world", "world"} },
		"secret-root": func(r *Request) { r.WorldRoots = []string{".ssh"} },
		"plugin-root": func(r *Request) { r.WorldRoots = []string{"plugins"} },
		"arbitrary":   func(r *Request) { r.WorldRoots = []string{"my-server"} },
		"root-alias":  func(r *Request) { r.WorldRoots = []string{"World"} },
		"nested-root": func(r *Request) { r.WorldRoots = []string{"world/data"} },
		"parent-root": func(r *Request) { r.WorldRoots = []string{"."} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			req := fixtureRequest()
			mutate(&req)
			if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected unsupported request: %v", err)
			}
		})
	}
}

func TestRejectNonportablePathsEvenOutsideWorlds(t *testing.T) {
	paths := []string{
		"", "/world/level.dat", "../world/level.dat", "world/../secret",
		"world/./level.dat", "world//level.dat", "world/level.dat/",
		"world\\level.dat", "C:/world/level.dat", "world/file:stream",
		"world/CON", "world/con.txt", "world/aux.dat", "world/NUL", "world/PRN",
		"world/COM1.txt", "world/lpt9/level.dat", "world/conin$",
		"world/name.", "world/name ", "world/has space", "world/é.dat", "world/\x00.dat",
		"world/ab~1.dat", "world/%2e%2e/secret", "world/?",
		"plugins/../../secret", "plugins\\evil.jar",
		"world/" + strings.Repeat("a", 129),
		strings.Repeat("a/", MaxDepth) + "file",
		strings.Repeat("a", MaxPathBytes+1),
	}
	for i, path := range paths {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			req := fixtureRequest()
			req.Entries = append(req.Entries, fixtureEntry(path))
			if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrInvalidInventory) {
				t.Fatalf("accepted path %q: %v", path, err)
			}
		})
	}
}

func TestRejectLinksSpecialFilesAndBadMetadata(t *testing.T) {
	entries := []Entry{
		{Path: "world/link", Kind: "symlink"},
		{Path: "plugins/link", Kind: "hardlink"},
		{Path: "world/socket", Kind: "socket"},
		{Path: "world/device", Kind: "device"},
		{Path: "world/fifo", Kind: "fifo"},
		{Path: "world/unknown", Kind: ""},
		{Path: "world/dir", Kind: Directory, Size: 1},
		{Path: "world/dir", Kind: Directory, SHA256: strings.Repeat("a", 64)},
		{Path: "world/a", Kind: File, Size: -1, SHA256: strings.Repeat("a", 64)},
		{Path: "world/a", Kind: File, Size: MaxFileBytes + 1, SHA256: strings.Repeat("a", 64)},
		{Path: "world/a", Kind: File, SHA256: strings.Repeat("a", 63)},
		{Path: "world/a", Kind: File, SHA256: strings.Repeat("a", 65)},
		{Path: "world/a", Kind: File, SHA256: strings.Repeat("A", 64)},
		{Path: "world/a", Kind: File, SHA256: strings.Repeat("z", 64)},
		fixtureEntry("world"),
	}
	for i, entry := range entries {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			req := fixtureRequest()
			req.Entries = append(req.Entries, entry)
			if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrInvalidInventory) {
				t.Fatalf("accepted unsupported entry: %+v / %v", entry, err)
			}
		})
	}
}

func TestRejectPathCollisionsInBothOrders(t *testing.T) {
	pairs := [][2]Entry{
		{fixtureEntry("world/a"), fixtureEntry("world/a")},
		{fixtureEntry("world/A"), fixtureEntry("world/a")},
		{fixtureEntry("world/Region/a"), fixtureEntry("world/region/b")},
		{fixtureEntry("world/dir"), fixtureEntry("world/dir/file")},
		{Entry{Path: "world/dir", Kind: Directory}, fixtureEntry("world/dir")},
		{Entry{Path: "world/Dir", Kind: Directory}, fixtureEntry("world/dir/file")},
		{fixtureEntry("plugins/A.jar"), fixtureEntry("plugins/a.jar")},
	}
	for i, pair := range pairs {
		for order := range 2 {
			t.Run(fmt.Sprintf("%d/%d", i, order), func(t *testing.T) {
				req := fixtureRequest()
				req.Entries = append(req.Entries, pair[order], pair[1-order])
				if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrInvalidInventory) {
					t.Fatalf("accepted collision: %+v / %v", pair, err)
				}
			})
		}
	}
}

func TestInventoryLimitsIncludeExcludedFiles(t *testing.T) {
	req := fixtureRequest()
	req.Entries = nil
	if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrInvalidInventory) {
		t.Fatalf("accepted empty inventory: %v", err)
	}
	req.Entries = make([]Entry, MaxEntries+1)
	if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrInvalidInventory) {
		t.Fatalf("accepted excessive entry count: %v", err)
	}
	req.Entries = nil
	for i := range 8 {
		entry := fixtureEntry(fmt.Sprintf("world/%d.mca", i))
		entry.Size = MaxFileBytes
		req.Entries = append(req.Entries, entry)
	}
	if got := mustPlan(t, req, fixtureContext()); got.TotalBytes != MaxTotalBytes {
		t.Fatalf("exact limit not accepted: %d", got.TotalBytes)
	}
	req.Entries = append(req.Entries, fixtureEntry("plugins/excluded.jar"))
	if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrInvalidInventory) {
		t.Fatalf("excluded bytes bypassed total limit: %v", err)
	}
	req = fixtureRequest()
	req.Entries = []Entry{fixtureEntry("plugins/only.jar")}
	if _, err := Plan(req, fixtureContext()); !errors.Is(err, ErrInvalidInventory) {
		t.Fatalf("accepted an inventory without world data: %v", err)
	}
}

func TestInvalidTrustedIdentifiers(t *testing.T) {
	for _, bad := range []string{"", " whitespace", "a/b", "a\nb", "é", strings.Repeat("a", 129)} {
		context := fixtureContext()
		context.RequiredPolicyRevision = bad
		if _, err := Plan(fixtureRequest(), context); !errors.Is(err, ErrInvalidContext) {
			t.Fatalf("accepted required revision %q: %v", bad, err)
		}
	}
	for _, field := range []string{"accepted", "checkpoint"} {
		context := fixtureContext()
		if field == "accepted" {
			context.AcceptedPolicyRevision = "bad/revision"
		} else {
			context.CheckpointID = "bad/checkpoint"
		}
		if _, err := Plan(fixtureRequest(), context); !errors.Is(err, ErrInvalidContext) {
			t.Fatalf("accepted malformed trusted field %s: %v", field, err)
		}
	}
}

func TestReadOnlyConcurrentPlanning(t *testing.T) {
	req, context := fixtureRequest(), fixtureContext()
	want := mustPlan(t, req, context).Digest
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			got, err := Plan(req, context)
			if err != nil || got.Digest != want || got.Executable {
				t.Errorf("inconsistent concurrent result: %v, %+v", err, got)
			}
		}()
	}
	workers.Wait()
}

func FuzzInventoryPath(f *testing.F) {
	for _, path := range []string{"world/level.dat", "world/players/data/123.dat", "../escape", "world/CON.txt", "world/secret.key", "plugins/a.jar"} {
		f.Add(path, int64(17), "file")
	}
	f.Fuzz(func(t *testing.T, path string, size int64, kind string) {
		req := fixtureRequest()
		entry := fixtureEntry(path)
		entry.Size, entry.Kind = size, Kind(kind)
		req.Entries = append(req.Entries, entry)
		got, err := Plan(req, fixtureContext())
		if err != nil {
			return
		}
		if got.Executable || got.TotalBytes < 0 || got.TotalBytes+got.ExcludedBytes > MaxTotalBytes || len(got.Blockers) < 5 {
			t.Fatalf("accepted unsafe review state: %+v", got)
		}
		for _, included := range got.Entries {
			if !strings.HasPrefix(included.Path, "world/") && included.Path != "world" {
				t.Fatalf("included outside root: %+v", included)
			}
		}
	})
}
