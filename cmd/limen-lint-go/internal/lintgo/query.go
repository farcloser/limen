package lintgo

import (
	"fmt"
	"io"
	"slices"
)

// What the lanes ask about the rendered policy beyond flags.
const (
	modeBlocking      = "blocking"
	modeInformational = "informational"
	linterRevive      = "revive"
	keyDisabled       = "disabled"
	queryArgs         = 1
)

// mode prints whether a lane's findings fail the run: "blocking" or
// "informational". Only NilAway has the choice, having no per-line
// suppression to make a false positive survivable any other way.
func mode(args []string, dir string, stdout io.Writer) error {
	if len(args) != queryArgs || args[0] != laneNilaway {
		return fmt.Errorf("%w: %s takes one lane, %s", ErrUsage, cmdMode, laneNilaway)
	}

	base, _, err := load(dir)
	if err != nil {
		return err
	}

	verdict := modeInformational
	if blocking, isBool := base.nilaway[keyBlocking].(bool); isBool && blocking {
		verdict = modeBlocking
	}

	_, _ = fmt.Fprintln(stdout, verdict)

	return nil
}

// disabled prints the revive rules the rendered configuration turns off,
// one per line, sorted: the suppression check fails a `//revive:disable`
// directive naming one of them, since nothing else ever reports a revive
// directive that silences nothing.
func disabled(args []string, dir string, stdout io.Writer) error {
	if len(args) != queryArgs || args[0] != linterRevive {
		return fmt.Errorf("%w: %s takes one linter, %s", ErrUsage, cmdDisabled, linterRevive)
	}

	base, _, err := load(dir)
	if err != nil {
		return err
	}

	linters, err := section(base.golangci, keyLinters, ErrBaseline)
	if err != nil {
		return err
	}

	settings, err := section(linters, keySettings, ErrBaseline)
	if err != nil {
		return err
	}

	revive, err := section(settings, linterRevive, ErrBaseline)
	if err != nil {
		return err
	}

	rules, err := list(revive[keyRules], keyLinters+keyJoin+keySettings+keyJoin+linterRevive+keyJoin+keyRules)
	if err != nil {
		return err
	}

	for _, name := range disabledRuleNames(rules) {
		_, _ = fmt.Fprintln(stdout, name)
	}

	return nil
}

// disabledRuleNames is the names of the named rule entries that carry
// `disabled: true`, sorted.
func disabledRuleNames(rules []any) []string {
	var names []string

	for _, rule := range rules {
		entry, isMap := rule.(document)
		if !isMap {
			continue
		}

		if off, isBool := entry[keyDisabled].(bool); isBool && off {
			if name := nameOf(entry); name != "" {
				names = append(names, name)
			}
		}
	}

	slices.Sort(names)

	return names
}
