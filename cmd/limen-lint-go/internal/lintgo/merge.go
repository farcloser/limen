package lintgo

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The golangci-lint vocabulary an overlay may use, and what each key does to
// the baseline (book/per-language.md, "one baseline, per-project carve-outs"):
//
//	linters.disable, linters.enable  move a linter between the two sets
//	formatters.enable                adds a formatter
//	*.exclusions.paths|rules|presets append
//	*.settings.<tool>                merge key by key: a scalar overrides, a
//	                                 mapping recurses, a list appends; in a
//	                                 list of named mappings (revive's rules)
//	                                 a name the baseline has overrides that
//	                                 entry's keys, a new name appends
//	licenses.allowed                 replaces the allowed list
//	licenses.ignore                  appends ignored modules
//
// Everything else in golangci's schema — run, issues, output, severity,
// exclusions.generated, paths-except — is policy the baseline owns, and an
// overlay naming it is rejected rather than merged.
const (
	keyLinters    = "linters"
	keyFormatters = "formatters"
	keyEnable     = "enable"
	keyDisable    = "disable"
	keySettings   = "settings"
	keyExclusions = "exclusions"
	keyPaths      = "paths"
	keyRules      = "rules"
	keyPresets    = "presets"
	keyName       = "name"
	keyAllowed    = "allowed"
	keyIgnore     = "ignore"
	keyJoin       = "."

	// policyOwned rejects an overlay key the baseline owns.
	policyOwned = "%w: %s.%s is policy the baseline owns, not a carve-out"
)

// report is what a render says about the overlay it applied.
type report struct {
	// found is whether the overlay file exists at all.
	found bool
	// carveOuts counts the overlay's entries: each linter moved, each
	// exclusion and ignore added, each tool whose settings changed.
	carveOuts int
	// notes are the entries that changed nothing: a linter the baseline
	// already disables, a value equal to the baseline's.
	notes []string
}

// summary is the one line render prints about the overlay.
func (r report) summary() string {
	if !r.found {
		return "no " + OverlayFile + ": the baseline as is"
	}

	return fmt.Sprintf("%d carve-out(s) in %s", r.carveOuts, OverlayFile)
}

// merger applies one overlay to one baseline, counting as it goes.
type merger struct {
	report report
}

// merge applies overlay to base in place and reports on it.
func merge(base *baseline, overlay document) (report, error) {
	applier := &merger{}

	for key := range overlay {
		sub, err := section(overlay, key, ErrOverlay)
		if err != nil {
			return report{}, err
		}

		switch key {
		case keyGolangci:
			err = applier.golangci(base.golangci, sub)
		case keyLicenses:
			err = applier.licenses(base.licenses, sub)
		default:
			err = fmt.Errorf("%w: unknown section %q (the sections are %s and %s)",
				ErrOverlay, key, keyGolangci, keyLicenses)
		}

		if err != nil {
			return report{}, err
		}
	}

	return applier.report, nil
}

// golangci applies the golangci section: linters and formatters, nothing else.
func (m *merger) golangci(base, over document) error {
	for key := range over {
		sub, err := section(over, key, ErrOverlay)
		if err != nil {
			return err
		}

		target, err := section(base, key, ErrBaseline)
		if err != nil {
			return err
		}

		base[key] = target

		switch key {
		case keyLinters:
			err = m.linters(target, sub)
		case keyFormatters:
			err = m.formatters(target, sub)
		default:
			err = fmt.Errorf(policyOwned, ErrOverlay, keyGolangci, key)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// linters applies the linters section.
func (m *merger) linters(base, over document) error {
	path := keyGolangci + keyJoin + keyLinters

	for key, value := range over {
		var err error

		switch key {
		case keyEnable:
			err = m.move(base, value, path, keyEnable, keyDisable)
		case keyDisable:
			err = m.move(base, value, path, keyDisable, keyEnable)
		case keyExclusions:
			err = m.exclusions(base, value, path, []string{keyPaths, keyRules, keyPresets})
		case keySettings:
			err = m.settings(base, value, path)
		default:
			err = fmt.Errorf(policyOwned, ErrOverlay, path, key)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// formatters applies the formatters section; golangci's schema has no
// formatters.disable, so neither has the overlay.
func (m *merger) formatters(base, over document) error {
	path := keyGolangci + keyJoin + keyFormatters

	for key, value := range over {
		var err error

		switch key {
		case keyEnable:
			err = m.move(base, value, path, keyEnable, "")
		case keyExclusions:
			err = m.exclusions(base, value, path, []string{keyPaths})
		case keySettings:
			err = m.settings(base, value, path)
		default:
			err = fmt.Errorf(policyOwned, ErrOverlay, path, key)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// move puts each name in value into base's `into` list and takes it out of
// its `from` list (none when from is empty); a name already where it is
// going is a note, not a carve-out.
func (m *merger) move(base document, value any, path, into, from string) error {
	names, err := stringList(value, path+keyJoin+into)
	if err != nil {
		return err
	}

	current, err := stringList(base[into], path+keyJoin+into)
	if err != nil {
		return err
	}

	for _, name := range names {
		if slices.Contains(current, name) {
			m.note(path + keyJoin + into + " already lists " + name)

			continue
		}

		current = append(current, name)
		m.report.carveOuts++
	}

	base[into] = anyList(current)

	if from == "" {
		return nil
	}

	other, err := stringList(base[from], path+keyJoin+from)
	if err != nil {
		return err
	}

	base[from] = anyList(slices.DeleteFunc(other, func(name string) bool { return slices.Contains(names, name) }))

	return nil
}

// exclusions appends each allowed list of value to base's exclusions.
func (m *merger) exclusions(base document, value any, path string, allowed []string) error {
	path += keyJoin + keyExclusions

	over, err := mapping(value, path)
	if err != nil {
		return err
	}

	target, err := section(base, keyExclusions, ErrBaseline)
	if err != nil {
		return err
	}

	base[keyExclusions] = target

	for key, entries := range over {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf(policyOwned+" (the lists that append: %s)",
				ErrOverlay, path, key, strings.Join(allowed, ", "))
		}

		added, err := list(entries, path+keyJoin+key)
		if err != nil {
			return err
		}

		current, err := list(target[key], path+keyJoin+key)
		if err != nil {
			return err
		}

		target[key] = append(current, added...)
		m.report.carveOuts += len(added)
	}

	return nil
}

// settings merges each tool's settings in value into base's, one carve-out
// per tool.
func (m *merger) settings(base document, value any, path string) error {
	path += keyJoin + keySettings

	over, err := mapping(value, path)
	if err != nil {
		return err
	}

	target, err := section(base, keySettings, ErrBaseline)
	if err != nil {
		return err
	}

	base[keySettings] = target

	for tool, overrides := range over {
		merged, err := m.value(target[tool], overrides, path+keyJoin+tool)
		if err != nil {
			return err
		}

		target[tool] = merged
		m.report.carveOuts++
	}

	return nil
}

// value merges one overlay value into its baseline counterpart: a mapping
// recurses, a list appends (by name when every element is a named mapping),
// a scalar overrides. A shape that differs from the baseline's is an error.
func (m *merger) value(base, over any, path string) (any, error) {
	switch over := over.(type) {
	case document:
		target, err := mapping(base, path)
		if err != nil {
			return nil, err
		}

		for key, sub := range over {
			merged, err := m.value(target[key], sub, path+keyJoin+key)
			if err != nil {
				return nil, err
			}

			target[key] = merged
		}

		return target, nil
	case []any:
		current, err := list(base, path)
		if err != nil {
			return nil, err
		}

		return m.lists(current, over, path)
	default:
		if !scalar(base) {
			return nil, fmt.Errorf("%w: %s is a mapping or a list in the baseline, not a scalar", ErrOverlay, path)
		}

		if base != nil && base == over {
			m.note(path + " equals the baseline's")
		}

		return over, nil
	}
}

// lists appends over to base: by name when both are lists of named mappings
// (an element with a name the baseline has merges into it), otherwise
// element by element, a scalar the baseline already lists being a note.
func (m *merger) lists(base, over []any, path string) ([]any, error) {
	if named(base) && named(over) {
		return byName(base, over, path)
	}

	for _, element := range over {
		if scalar(element) && slices.Contains(base, element) {
			m.note(fmt.Sprintf("%s already lists %v", path, element))

			continue
		}

		base = append(base, element)
	}

	return base, nil
}

// byName appends the named mappings the baseline lacks; one it has takes the
// overlay's keys, each replacing its namesake's (a rule's arguments are a
// new list, not the old one grown), and keeps the rest.
func byName(base, over []any, path string) ([]any, error) {
	for _, element := range over {
		name := nameOf(element)
		index := slices.IndexFunc(base, func(candidate any) bool { return nameOf(candidate) == name })

		if index < 0 {
			base = append(base, element)

			continue
		}

		target, err := mapping(base[index], path+"["+name+"]")
		if err != nil {
			return nil, err
		}

		overrides, err := mapping(element, path+"["+name+"]")
		if err != nil {
			return nil, err
		}

		maps.Copy(target, overrides)

		base[index] = target
	}

	return base, nil
}

// licenses applies the licenses section: allowed replaces, ignore appends.
func (m *merger) licenses(base, over document) error {
	for key, value := range over {
		path := keyLicenses + keyJoin + key

		entries, err := stringList(value, path)
		if err != nil {
			return err
		}

		switch key {
		case keyAllowed:
			base[keyAllowed] = anyList(entries)
			m.report.carveOuts++
		case keyIgnore:
			current, err := stringList(base[keyIgnore], path)
			if err != nil {
				return err
			}

			base[keyIgnore] = anyList(append(current, entries...))
			m.report.carveOuts += len(entries)
		default:
			return fmt.Errorf("%w: unknown key %s (the keys are %s and %s)", ErrOverlay, path, keyAllowed, keyIgnore)
		}
	}

	return nil
}

func (m *merger) note(text string) {
	m.report.notes = append(m.report.notes, text)
}

// mapping is value as a mapping; nil is an empty one.
func mapping(value any, path string) (document, error) {
	if value == nil {
		return document{}, nil
	}

	doc, isMap := value.(document)
	if !isMap {
		return nil, fmt.Errorf("%w: %s must be a mapping", ErrOverlay, path)
	}

	return doc, nil
}

// list is value as a list; nil is an empty one.
func list(value any, path string) ([]any, error) {
	if value == nil {
		return []any{}, nil
	}

	entries, isList := value.([]any)
	if !isList {
		return nil, fmt.Errorf("%w: %s must be a list", ErrOverlay, path)
	}

	return entries, nil
}

// stringList is value as a list of strings; nil is an empty one.
func stringList(value any, path string) ([]string, error) {
	entries, err := list(value, path)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		name, isString := entry.(string)
		if !isString {
			return nil, fmt.Errorf("%w: %s must be a list of names, got %v", ErrOverlay, path, entry)
		}

		names = append(names, name)
	}

	return names, nil
}

// anyList is names as the parser would have produced them, so the encoder
// treats a merged list like an untouched one.
func anyList(names []string) []any {
	entries := make([]any, 0, len(names))
	for _, name := range names {
		entries = append(entries, name)
	}

	return entries
}

// named is whether every element of entries is a mapping with a string name.
func named(entries []any) bool {
	if len(entries) == 0 {
		return false
	}

	for _, entry := range entries {
		if nameOf(entry) == "" {
			return false
		}
	}

	return true
}

// nameOf is the name of a named mapping, "" for anything else.
func nameOf(entry any) string {
	doc, isMap := entry.(document)
	if !isMap {
		return ""
	}

	name, isString := doc[keyName].(string)
	if !isString {
		return ""
	}

	return name
}

// scalar is whether value is comparable with ==: not a mapping, not a list.
func scalar(value any) bool {
	switch value.(type) {
	case document, []any:
		return false
	default:
		return true
	}
}
