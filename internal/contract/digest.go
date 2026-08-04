package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func LegacyToolDigestV1(tool LegacyToolV1) string {
	raw, err := json.Marshal(tool)
	if err != nil {
		return ""
	}
	return sha256Hex(raw)
}

func CatalogV2Digest(catalog Catalog) (string, error) {
	canonical := catalog
	canonical.Operations = append([]Operation(nil), catalog.Operations...)
	sort.Slice(canonical.Operations, func(i, j int) bool {
		left, right := canonical.Operations[i], canonical.Operations[j]
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		if left.Wire.Method != right.Wire.Method {
			return left.Wire.Method < right.Wire.Method
		}
		return left.Wire.Path < right.Wire.Path
	})
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
