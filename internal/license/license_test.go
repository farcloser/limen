package license_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/farcloser/limen/internal/license"
)

const mitText = `The MIT License (MIT)

Copyright (c) 2024 Farcloser

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND.`

const apacheText = `                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION`

const closedText = `Copyright (c) 2026 Farcloser. All rights reserved.

This software and its source code are proprietary and confidential.`

const agplText = `                    GNU AFFERO GENERAL PUBLIC LICENSE
                       Version 3, 19 November 2007

 Copyright (C) 2007 Free Software Foundation, Inc.`

const ccBySaText = `Creative Commons Attribution-ShareAlike 4.0 International Public License

By exercising the Licensed Rights, You accept and agree to be bound...`

const ccByNdText = `Creative Commons Attribution-NoDerivatives 4.0 International Public License

By exercising the Licensed Rights, You accept and agree to be bound...`

const ccBySaDeed = `This work is licensed under a Creative Commons license.
See https://creativecommons.org/licenses/by-sa/4.0/ for details.`

// BSD and ISC both reserve rights with "All rights reserved" while granting an
// open license; neither is allowed, so both must classify as Unknown and not be
// swept into Closed-source by the reservation phrase.
const bsdText = `Copyright (c) 2026, Farcloser. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:`

const iscText = `Copyright (c) 2026 Farcloser

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted.`

// A bare all-rights-reserved notice with no open grant — e.g. a photography
// repository — is the genuine Closed-source case.
const photographyText = `Copyright (c) 2026 Farcloser. All rights reserved.`

// rewrapped lowercases the text and collapses all whitespace, mimicking a
// LICENSE file whose wrapping and casing drifted from the canonical.
func rewrapped(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

func TestIdentify(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want license.ID
	}{
		{"mit", mitText, license.MIT},
		{"apache", apacheText, license.Apache20},
		{"agpl", agplText, license.AGPL30},
		{"cc-by-sa prose", ccBySaText, license.CCBYSA40},
		{"cc-by-nd prose", ccByNdText, license.CCBYND40},
		{"cc-by-sa url deed", ccBySaDeed, license.CCBYSA40},
		{"closed", closedText, license.Closed},
		{"photography all rights reserved", photographyText, license.Closed},
		{"empty", "", license.Unknown},
		{"gpl not affero", "GNU GENERAL PUBLIC LICENSE Version 3", license.Unknown},
		{"agpl behind a short preamble", "Copyright (c) 2026 Farcloser\n\n" + agplText, license.AGPL30},
		{"bsd not closed", bsdText, license.Unknown},
		{"isc not closed", iscText, license.Unknown},
		{"cc-by-nc not allowed", "Creative Commons Attribution-NonCommercial 4.0", license.Unknown},
		{"mit lowercased and rewrapped", rewrapped(mitText), license.MIT},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := license.Identify(tc.text); got != tc.want {
				t.Errorf("Identify() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIdentifyRejectsFullTexts pins the full official texts (SPDX
// license-list-data) of the rejected set to Unknown. Synthetic snippets are not
// enough here: the real texts carry other licenses' names in their bodies —
// GPL-3.0 §13 names the AGPL, SPDX's LGPL-3.0 appends the entire GPL-3.0, and
// MPL-2.0 §1.12 names the AGPL among its Secondary Licenses — and substring
// matching once passed all three as the allowed AGPL-3.0 while the header-only
// fixture above kept the suite green.
func TestIdentifyRejectsFullTexts(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob(filepath.Join("testdata", "rejected", "*.txt"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("no fixtures under testdata/rejected — the rejected set must stay pinned")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}

			if got := license.Identify(string(data)); got != license.Unknown {
				t.Errorf("Identify(%s) = %q, want %q", filepath.Base(file), got, license.Unknown)
			}
		})
	}
}
