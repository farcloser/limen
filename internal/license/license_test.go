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

const gpl2Text = `                    GNU GENERAL PUBLIC LICENSE
                       Version 2, June 1991

 Copyright (C) 1989, 1991 Free Software Foundation, Inc.`

const ccBySaText = `Creative Commons Attribution-ShareAlike 4.0 International Public License

By exercising the Licensed Rights, You accept and agree to be bound...`

const ccByNdText = `Creative Commons Attribution-NoDerivatives 4.0 International Public License

By exercising the Licensed Rights, You accept and agree to be bound...`

const ccBySaDeed = `This work is licensed under a Creative Commons license.
See https://creativecommons.org/licenses/by-sa/4.0/ for details.`

// BSD-2-Clause and ISC both reserve rights with "All rights reserved" while
// granting an open license; neither is allowed (only BSD-3-Clause is, and only
// as inherited), so both must classify as Unknown and not be swept into
// Closed-source by the reservation phrase.
const bsdText = `Copyright (c) 2026, Farcloser. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:`

// The canonical BSD-3-Clause wording, clause 3 as SPDX publishes it.
const bsd3Text = `Copyright (c) 2026, Farcloser. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice.
2. Redistributions in binary form must reproduce the above copyright notice.
3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS".`

// A personalized clause 3, as older BSD projects wrote it (ulikunitz/xz is the
// live example). Accepting BSD-3-Clause exists for inherited licenses we
// cannot edit, so the hand-edited subject must classify the same as the
// canonical wording.
const bsd3PersonalizedText = `Copyright (c) 2014-2022  Ulrich Kunitz
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice.

* My name, Ulrich Kunitz, may not be used to endorse or promote products
  derived from this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS".`

// BSD-4-Clause carries the advertising clause on top of the non-endorsement
// clause; it must not ride in on the BSD-3-Clause acceptance.
const bsd4Text = `Copyright (c) 2026, Farcloser. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

3. All advertising materials mentioning features or use of this software must
   display the following acknowledgement: This product includes software
   developed by Farcloser.
4. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.`

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
		{"gpl-2.0", gpl2Text, license.GPL20},
		{"gpl-2.0 behind a short preamble", "Copyright (c) 2026 Farcloser\n\n" + gpl2Text, license.GPL20},
		{"cc-by-sa prose", ccBySaText, license.CCBYSA40},
		{"cc-by-nd prose", ccByNdText, license.CCBYND40},
		{"cc-by-sa url deed", ccBySaDeed, license.CCBYSA40},
		{"closed", closedText, license.Closed},
		{"photography all rights reserved", photographyText, license.Closed},
		{"empty", "", license.Unknown},
		{"gpl not affero", "GNU GENERAL PUBLIC LICENSE Version 3", license.Unknown},
		{"agpl behind a short preamble", "Copyright (c) 2026 Farcloser\n\n" + agplText, license.AGPL30},
		{"bsd-2 not closed, not bsd-3", bsdText, license.Unknown},
		{"bsd-3 canonical", bsd3Text, license.BSD3},
		{"bsd-3 personalized clause 3", bsd3PersonalizedText, license.BSD3},
		{"bsd-3 rewrapped", rewrapped(bsd3Text), license.BSD3},
		{"bsd-4 not bsd-3", bsd4Text, license.Unknown},
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
