package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Rule is what a harness path demands of the entries it names. Every path demands that an
// entry exists and is current; the other demands apply when set.
type Rule struct {
	// WrittenBy is the exact host session that must have written every entry.
	WrittenBy string
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
