package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestOperationCatalogV2PreservesAll125PublicLegacyContractsAndDigests(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("load operation catalog v2: %v", err)
	}
	if got, want := len(catalog.Operations), 131; got != want {
		t.Fatalf("catalog v2 count=%d, want %d", got, want)
	}
	publicOperations := loadToolOperations("", "", "")
	if got, want := len(publicOperations), 125; got != want {
		t.Fatalf("public operation count=%d, want %d", got, want)
	}

	rawGolden, err := os.ReadFile(filepath.Join("testdata", "unification", "tool_contract_digests.json"))
	if err != nil {
		t.Fatalf("read digest golden: %v", err)
	}
	var golden map[string]string
	if err := json.Unmarshal(rawGolden, &golden); err != nil {
		t.Fatalf("decode digest golden: %v", err)
	}

	for _, operation := range publicOperations {
		id := string(operation.ID)
		rebuilt, err := contract.RebuildLegacyToolV1(catalog, operation)
		if err != nil {
			t.Fatalf("rebuild legacy tool %q: %v", id, err)
		}
		gotDigest := contract.LegacyToolDigestV1(rebuilt)
		if gotDigest != toolContractForDigest(operation) {
			t.Fatalf("legacy digest implementation mismatch for %q", id)
		}
		if gotDigest != golden[id] {
			t.Fatalf("legacy digest golden changed for %q: got %s want %s", id, gotDigest, golden[id])
		}
	}
}

func TestCLICatalogHasNoLegacyAdapters(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob CLI sources: %v", err)
	}
	forbidden := []string{
		"type toolCatalog struct",
		"type toolContractCatalog struct",
		"func loadToolCatalog(",
		"func toolCatalogFromLegacy(",
		"generatedToolCatalogJSON",
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, token := range forbidden {
			if strings.Contains(string(raw), token) {
				t.Fatalf("%s still contains retired catalog adapter %q", path, token)
			}
		}
	}
}

func TestOperationCatalogV2SpecialCodecsAreExplicit(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("load operation catalog v2: %v", err)
	}
	want := map[contract.OperationID]contract.CodecID{
		"log.put":     contract.CodecPutLogs,
		"log.track":   contract.CodecWebTracks,
		"log.consume": contract.CodecConsumeLogs,
	}
	for _, operation := range catalog.Operations {
		expected, special := want[operation.ID]
		if special {
			if operation.Wire.Codec != expected {
				t.Fatalf("%s codec=%q, want %q", operation.ID, operation.Wire.Codec, expected)
			}
			delete(want, operation.ID)
			continue
		}
		if operation.Wire.Codec != contract.CodecJSON {
			t.Fatalf("%s unexpected non-json codec %q", operation.ID, operation.Wire.Codec)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing special codec operations: %#v", want)
	}
}
