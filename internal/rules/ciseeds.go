package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// staleCISeed reports the release whose seed the repository's ci.yaml still
// is, byte for byte, when that seed is not the current one.
func staleCISeed(root string) (string, bool) {
	// #nosec G304 -- a fixed path under the caller-designated repository.
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pathWorkflowCI)))
	if err != nil {
		return "", false
	}

	sum := sha256.Sum256(data)
	tag, found := releasedCISeeds[hex.EncodeToString(sum[:])]

	return tag, found
}

// releasedCISeeds maps the sha256 of every ci.yaml a released limen seeded to
// the first release that seeded it. A repository whose ci.yaml is one of them
// never edited its seed, so fix replaces it with the current one; any other
// ci.yaml is the project's own and is left alone. When a release changes the
// seed, the seed it replaces is added here, or the repositories still
// carrying it unedited stop being migrated.
//
//nolint:gochecknoglobals // immutable table.
var releasedCISeeds = map[string]string{
	"21856a78f0c4c6266350867f84603ce11f7f0362c1cda0925004863f47bbef72": "v0.0.1",
	"d641ac7296886c600e342cdabf49e1ba557e0a176bcafbece8972e2b53d65e52": "v0.0.2",
	"df9ce79a8c6914196d8968cde967e2145914cd78de10ca2e5795fcf3898c4256": "v0.0.7",
	"d3070e367519d597be11789647d16a52da06e4d5dda3fb32dd627756767feb81": "v0.0.9",
	"8f04507a607c7e94d1fbf9b6de798b294ae4a6cdda967fecd6cc411f2c9d3bf4": "v0.0.14",
	"e26fda8cc2c022dafc6ef4fa7e887aced12925ea51c55cd176689f04580740fe": "v0.1.0",
	"6a758df1d7eb4db75943c4a047bfc87348511e571d7bf8f4a20edf2e95d62a47": "v0.2.0",
	"0f0fac59d02ef57f7eb8143975e1c4efb0ccfd9f5358560075b62f9c3524fbfd": "v0.3.0",
	"f796e253b4d88579ff71dfa6f4154e78c424ce7ff54f42b5fd675bfd94659002": "v0.5.0",
	"64e507ea774f21e0a3d76353c2550c4dd0c342b7df34a36516ce17b652b522cd": "v0.6.0",
	"e983c3df3f770b921ba087158a6b03b21f834c64c4024453bfdb8bc8dcab74ce": "v0.7.0",
	"854042b1d179d5b5ec53cfb4920394f959dc3ebd0f9c60a7525f2c6f9c25b231": "v0.8.0",
}
