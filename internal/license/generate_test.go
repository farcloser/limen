package license_test

import (
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/license"
)

// TestNoticeRoundTrips is the load-bearing guarantee: every license limen writes
// must be identified back as itself, so a bootstrapped repo passes the license
// check. This catches a wrong/edited embedded text or a broken placeholder.
func TestNoticeRoundTrips(t *testing.T) {
	t.Parallel()

	// Every license limen generates: Closed-source plus the open licenses whose
	// SPDX text is embedded.
	allowed := []license.ID{
		license.MIT,
		license.Apache20,
		license.AGPL30,
		license.Closed,
		license.CCBYSA40,
		license.CCBYND40,
	}

	for _, id := range allowed {
		text, ok := license.Notice(id, 2026, "Farcloser")
		if !ok {
			t.Errorf("Notice(%s) not generated", id)

			continue
		}

		if got := license.Identify(text); got != id {
			t.Errorf("Notice(%s) identified as %s", id, got)
		}

		if !license.CanGenerate(id) {
			t.Errorf("CanGenerate(%s) = false", id)
		}
	}
}

// MIT and Closed-source carry a copyright line that must be interpolated; the
// verbatim licenses (Apache, AGPL, CC) legitimately do not mention the holder in
// their body, so they are excluded here.
func TestNoticeInterpolatesCopyright(t *testing.T) {
	t.Parallel()

	for _, id := range []license.ID{license.MIT, license.Closed} {
		text, _ := license.Notice(id, 2031, "Acme Corp")
		if !strings.Contains(text, "2031") || !strings.Contains(text, "Acme Corp") {
			t.Errorf("%s did not interpolate year/holder:\n%s", id, text)
		}

		if strings.Contains(text, "{{") {
			t.Errorf("%s left an unfilled placeholder:\n%s", id, text)
		}
	}
}

// The verbatim licenses must keep the placeholders their authors publish:
// Apache's appendix and AGPL's "how to apply" section are instructions for
// source-file headers, shipped unfilled by the ASF and the FSF. Filling them
// in — or letting the year/holder leak into the text — would mean we edited
// the license.
func TestVerbatimPlaceholdersRetained(t *testing.T) {
	t.Parallel()

	for id, want := range map[license.ID]string{
		license.Apache20: "[yyyy] [name of copyright owner]",
		license.AGPL30:   "<year>  <name of author>",
	} {
		text, _ := license.Notice(id, 2031, "Acme Corp")
		if !strings.Contains(text, want) {
			t.Errorf("%s lost its canonical placeholder %q", id, want)
		}

		if strings.Contains(text, "Acme Corp") {
			t.Errorf("%s interpolated the holder into verbatim license text", id)
		}
	}
}

func TestNoticeRejectsUnknown(t *testing.T) {
	t.Parallel()

	if _, ok := license.Notice(license.Unknown, 2026, "x"); ok {
		t.Error("Notice(Unknown) should not generate")
	}

	if license.CanGenerate(license.Unknown) {
		t.Error("CanGenerate(Unknown) = true")
	}
}

func TestClosedSourceNoticeRoundTrips(t *testing.T) {
	t.Parallel()

	if id := license.Identify(license.ClosedSourceNotice(2026, "Farcloser")); id != license.Closed {
		t.Fatalf("closed-source notice identified as %s", id)
	}
}
