// Package license identifies the license contained in a repository's LICENSE
// file. It is deliberately heuristic: it recognizes the small, fixed set of
// licenses Farcloser allows and reports everything else as Unknown, leaving the
// policy decision to the caller.
package license

import (
	"embed"
	"fmt"
	"strconv"
	"strings"
)

// licenseTexts holds the canonical text of every open license limen can
// generate, verified against each license's authority (apache.org, gnu.org,
// and the SPDX license list). Only MIT carries {{year}} and {{holder}}
// placeholders: its copyright line is part of the license body and must be
// filled. Apache-2.0 and AGPL-3.0 keep "[yyyy] [name of copyright owner]" and
// "<year>  <name of author>" untouched — that is how the ASF and the FSF
// themselves publish the files; those appendices are instructions for
// source-file headers, not part of the LICENSE, and the FSF forbids altering
// its license text. The CC licenses have no notice line at all. See
// generate_test.go, which asserts each text round-trips through Identify and
// that the verbatim placeholders stay unfilled.
//
//go:embed texts/*.txt
var licenseTexts embed.FS

// generatableFile returns the embedded-text file for an open license limen
// can generate. Closed-source is not here: it is generated inline by
// ClosedSourceNotice from the text documented in the book, so it has no SPDX
// source file. A lookup table rather than a switch, deliberately: this is
// data, and a switch over ID would pit exhaustive (list every member) against
// identical-switch-branches (do not list members that behave like default).
func generatableFile(licenseID ID) (string, bool) {
	name, found := map[ID]string{
		MIT:      "MIT.txt",
		Apache20: "Apache-2.0.txt",
		AGPL30:   "AGPL-3.0.txt",
		CCBYSA40: "CC-BY-SA-4.0.txt",
		CCBYND40: "CC-BY-ND-4.0.txt",
	}[licenseID]

	return name, found
}

// CanGenerate reports whether limen can write a LICENSE file for the given id:
// every allowed license — Closed-source (from the book) and the open licenses
// (their verbatim SPDX text is embedded) — can be generated.
func CanGenerate(id ID) bool {
	if id == Closed {
		return true
	}

	_, ok := generatableFile(id)

	return ok
}

// Notice returns the LICENSE text for id, with year and holder filled into the
// copyright line where the license has one (MIT and Closed-source; the others
// are used verbatim). ok is false only for an id limen does not generate.
func Notice(id ID, year int, holder string) (string, bool) {
	if id == Closed {
		return ClosedSourceNotice(year, holder), true
	}

	name, ok := generatableFile(id)
	if !ok {
		return "", false
	}

	data, err := licenseTexts.ReadFile("texts/" + name)
	if err != nil {
		return "", false // unreachable: the files are embedded at build time
	}

	text := strings.ReplaceAll(string(data), "{{year}}", strconv.Itoa(year))
	text = strings.ReplaceAll(text, "{{holder}}", holder)

	return text, true
}

// ClosedSourceNotice returns the canonical closed-source LICENSE text — the text
// documented verbatim in book/mandatory-files.md — for the given copyright year
// and holder. By construction Identify(ClosedSourceNotice(y, h)) == Closed.
func ClosedSourceNotice(year int, holder string) string {
	return fmt.Sprintf(`Copyright (c) %d %s. All rights reserved.

This software and its source code are proprietary and confidential. No license,
express or implied, is granted to any person to use, copy, modify, distribute, or
create derivative works of this software, in whole or in part, without the prior
written permission of %s.
`, year, holder, holder)
}

// ID is a canonical license identifier. The recognized values mirror the
// allowed-license list in book/mandatory-files.md.
type ID string

// The recognized license identifiers — the allowed set from
// book/mandatory-files.md, plus Unknown for everything else.
const (
	// Software.

	MIT      ID = "MIT"
	Apache20 ID = "Apache-2.0"
	AGPL30   ID = "AGPL-3.0"
	Closed   ID = "Closed-source"

	// Content.

	CCBYSA40 ID = "CC-BY-SA-4.0"
	CCBYND40 ID = "CC-BY-ND-4.0"

	Unknown ID = "Unknown"
)

// Identify classifies the text of a LICENSE file. It returns Unknown when the
// text matches none of the recognized licenses, which callers should treat as a
// failure rather than as permission.
//
// "All rights reserved" notices — whether covering proprietary software or
// all-rights-reserved photography — are reported as Closed: legally they are the
// same instrument, and the distinction is one of use, not of license.
func Identify(text string) ID {
	normalized := normalize(text)
	switch {
	case isMIT(normalized):
		return MIT
	case isApache20(normalized):
		return Apache20
	case isAGPL30(normalized):
		return AGPL30
	case isCC(normalized, "sharealike", "by-sa"):
		return CCBYSA40
	case isCC(normalized, "noderivatives", "by-nd") || isCC(normalized, "noderivs", "by-nd"):
		return CCBYND40
	case isClosed(normalized):
		return Closed
	default:
		return Unknown
	}
}

func isMIT(n string) bool {
	return strings.Contains(n, "permission is hereby granted, free of charge") &&
		strings.Contains(n, "the software is provided")
}

func isApache20(n string) bool {
	return strings.Contains(n, "apache license") && strings.Contains(n, "version 2.0")
}

func isAGPL30(n string) bool {
	return strings.Contains(n, "affero general public license") && strings.Contains(n, "version 3")
}

// isCC recognizes a Creative Commons 4.0 license. It accepts either the prose
// name ("creative commons attribution-<variant>" + "4.0") or the canonical
// license URL ("creativecommons.org/licenses/<slug>/4.0"), so both the full
// legal code and a short deed are matched.
func isCC(n, variant, slug string) bool {
	byProse := strings.Contains(n, "creative commons") &&
		strings.Contains(n, "attribution-"+variant) &&
		strings.Contains(n, "4.0")
	byURL := strings.Contains(n, "creativecommons.org/licenses/"+slug+"/4.0")

	return byProse || byURL
}

// isClosed recognizes a proprietary / all-rights-reserved notice. It is only
// consulted after every named license has been ruled out. A bare "all rights
// reserved" is not enough on its own: permissive licenses (notably BSD and ISC)
// carry that exact phrase alongside an open grant of rights, and classifying
// those as Closed-source would let a license the book rejects pass the check.
// So a reservation of rights only counts as proprietary when no open grant of
// rights accompanies it. The canonical text lives in book/mandatory-files.md.
func isClosed(normalized string) bool {
	if hasOpenGrant(normalized) {
		return false
	}

	return strings.Contains(normalized, "all rights reserved") ||
		strings.Contains(normalized, "proprietary and confidential") ||
		strings.Contains(normalized, "closed source") ||
		strings.Contains(normalized, "closed-source")
}

// hasOpenGrant reports whether the text grants rights the way an open-source or
// public license does. It matches the grant phrasing of the licenses we might
// otherwise see — BSD's "redistribution and use", MIT's "permission is hereby
// granted", ISC's "permission to use, copy", and the named families — without
// matching the canonical closed-source notice, which states that no license
// "is granted". A match means the text is some license we do not recognize, so
// it must fail as Unknown rather than slip through as Closed-source.
func hasOpenGrant(normalized string) bool {
	for _, phrase := range []string{
		"redistribution and use",
		"permission is hereby granted",
		"permission to use, copy",
		"apache license",
		"general public license",
		"mozilla public license",
		"creative commons",
		"is licensed under",
	} {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}

	return false
}

// normalize lowercases the text and collapses every run of whitespace to a
// single space, so that line wrapping and indentation in a LICENSE file do not
// defeat substring matching.
func normalize(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}
