package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Rule is what a harness path demands of the entries it names. Every path demands that an
// entry exists and is current; the other demands apply when set.
type Rule struct {
	// WrittenBy is the exact host session that must have written every entry.
	WrittenBy string
}

// ErrMissingEntries marks a refusal of IDs that name no entry: only those IDs
// need correcting; any other Current error leaves the IDs valid to retry.
var ErrMissingEntries = errors.New("refused Takt memory entries")

// Current carries each entry ID forward to the current version of its chain. heads is aligned
// with ids; described holds each distinct head once, in first-seen order. Only an ID that does
// not exist is refused.
func Current(ctx context.Context, a *Admin, ids []int64) (heads []int64, described DescribeResult, err error) {
	res, err := a.Describe(ctx, DescribeInput{IDs: append([]int64{}, ids...)})
	if err != nil {
		return nil, res, err
	}
	var missing []string
	var distinct []int64
	for _, d := range res.Entries {
		if !d.Exists {
			missing = append(missing, fmt.Sprintf("#%d does not exist", d.ID))
			continue
		}
		heads = append(heads, d.CurrentVersion)
		if !slices.Contains(distinct, d.CurrentVersion) {
			distinct = append(distinct, d.CurrentVersion)
		}
	}
	if len(missing) > 0 {
		return nil, res, fmt.Errorf("%w: %s", ErrMissingEntries, strings.Join(missing, "; "))
	}
	if slices.Equal(distinct, ids) {
		return heads, res, nil
	}
	if described, err = a.Describe(ctx, DescribeInput{IDs: distinct}); err != nil {
		return nil, described, err
	}
	for _, d := range described.Entries {
		if d.CurrentVersion != d.ID {
			return nil, described, fmt.Errorf("refused Takt memory entry #%d: superseded during resolution; retry", d.ID)
		}
	}
	return heads, described, nil
}

// Validate is the single validator of the harness: one describe call checks every ID
// against rule and refuses with one message that names each failing entry and its correction. It
// returns the describe result even when it refuses.
func Validate(ctx context.Context, a *Admin, ids []int64, rule Rule) (DescribeResult, error) {
	res, err := a.Describe(ctx, DescribeInput{IDs: append([]int64{}, ids...)})
	if err != nil {
		return res, err
	}
	var problems []string
	for _, d := range res.Entries {
		switch {
		case !d.Exists:
			problems = append(problems, fmt.Sprintf("#%d does not exist", d.ID))
		case rule.WrittenBy != "" && d.WriterHostSession != rule.WrittenBy:
			problems = append(problems, fmt.Sprintf("#%d was written by another session", d.ID))
		case d.CurrentVersion != d.ID:
			problems = append(problems, fmt.Sprintf("#%d is superseded; its current version is #%d", d.ID, d.CurrentVersion))
		}
	}
	if len(problems) > 0 {
		return res, errors.New("refused Takt memory entries: " + strings.Join(problems, "; "))
	}
	return res, nil
}
