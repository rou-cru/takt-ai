package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// readBudgetBytes bounds one read: about 16k tokens. A body larger than this is read by section.
const readBudgetBytes = 64 * 1024

// maxHeadingLevel is the deepest markdown ATX heading level (#### ... ######).
const maxHeadingLevel = 6

// untitledSection names the text before the first heading in an outline.
const untitledSection = "(no heading)"

// section is one markdown section of a body: the single definition of what a section is.
// Heading is empty for the text before the first heading; Text includes the heading line.
type section struct {
	Heading string
	Text    string
}

// headingOf returns the heading text of an ATX heading line, and whether the line is one.
func headingOf(line string) (string, bool) {
	hashes := len(line) - len(strings.TrimLeft(line, "#"))
	if hashes == 0 || hashes > maxHeadingLevel {
		return "", false
	}
	rest := line[hashes:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// splitSections splits a body at its markdown headings, ignoring headings inside fenced code.
// It always returns at least one section; text before the first heading is its own section.
func splitSections(body string) []section {
	var out []section
	cur := section{}
	var text strings.Builder
	fenced := false
	flush := func() {
		cur.Text = text.String()
		text.Reset()
		if len(out) > 0 || cur.Heading != "" || strings.TrimSpace(cur.Text) != "" {
			out = append(out, cur)
		}
	}
	for _, line := range strings.SplitAfter(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
		}
		if heading, ok := headingOf(strings.TrimRight(line, "\r\n")); ok && !fenced {
			flush()
			cur = section{Heading: heading}
		}
		text.WriteString(line)
	}
	flush()
	if len(out) == 0 {
		out = append(out, section{})
	}
	return out
}

// OutlineEntry is one section of an outline: its heading and its size in bytes.
type OutlineEntry struct {
	Heading string
	Size    int
}

// RelationView is one relation of an entry, seen from that entry.
type RelationView struct {
	// Kind is the relation kind.
	Kind string
	// Outgoing is true when the entry is the source of the relation, false when it is the target.
	Outgoing bool
	// Other is the entry on the other end.
	Other      int64
	OtherTitle string
}

// SupplementView is the head version of an entry that supplements the one read. Body is empty in an outline read.
type SupplementView struct {
	ID    int64
	Title string
	Body  string
}

// ReadResult is one entry as read (MEM-RET-8). In an outline read, Outline lists every
// section and Body holds only Section; otherwise Outline is nil and Body is the complete body.
type ReadResult struct {
	ID        int64
	Nature    string
	Tier      string
	Title     string
	Author    string
	Session   string
	Evidence  string
	UserOrder bool
	CreatedAt int64
	// CurrentID and CurrentTitle name the current version when the entry was corrected or superseded; CurrentID is 0 otherwise.
	CurrentID    int64
	CurrentTitle string
	Relations    []RelationView
	Supplements  []SupplementView
	Outline      []OutlineEntry
	Section      string
	Body         string
}

// Read returns the entry with the given ID: metadata, the current version when it is superseded, relations,
// the body, and the head version of each supplement. When the body and supplements exceed the read budget it
// returns the outline, the section named by section (the first when empty), and the supplements by ID and
// title. An unknown ID or section is refused as such and never substituted (MEM-RET-8).
func (s *Store) Read(ctx context.Context, id int64, sectionHeading string) (ReadResult, error) {
	tx, err := s.reader.BeginTx(ctx, nil)
	if err != nil {
		return ReadResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	r := ReadResult{ID: id}
	err = tx.QueryRowContext(ctx, `SELECT e.nature, e.tier, e.title, e.author, e.session, e.evidence, e.user_order, e.created_at, e.body
		FROM entries e WHERE e.id = ?`, id).
		Scan(&r.Nature, &r.Tier, &r.Title, &r.Author, &r.Session, &r.Evidence, &r.UserOrder, &r.CreatedAt, &r.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadResult{}, refuse("entry #%d does not exist; report it as not found", id)
	}
	if err != nil {
		return ReadResult{}, err
	}
	sections := splitSections(r.Body)
	picked := 0
	if sectionHeading != "" {
		picked = -1
		for i, sec := range sections {
			if sec.Heading == strings.TrimSpace(sectionHeading) {
				picked = i
				break
			}
		}
		if picked < 0 {
			return ReadResult{}, refuse("entry #%d has no section %q; choose a heading from its outline: %s", id, sectionHeading, outlineHeadings(sections))
		}
	}
	head, err := chainHead(ctx, tx, id)
	if err != nil {
		return ReadResult{}, err
	}
	if head != id {
		r.CurrentID = head
		if err := tx.QueryRowContext(ctx, `SELECT title FROM entries WHERE id = ?`, head).Scan(&r.CurrentTitle); err != nil {
			return ReadResult{}, err
		}
	}
	if r.Relations, err = loadRelations(ctx, tx, id); err != nil {
		return ReadResult{}, err
	}
	if r.Supplements, err = loadSupplements(ctx, tx, id); err != nil {
		return ReadResult{}, err
	}
	size := len(r.Body)
	for _, sup := range r.Supplements {
		size += len(sup.Body)
	}
	if size <= readBudgetBytes {
		return r, nil
	}
	for _, sec := range sections {
		r.Outline = append(r.Outline, OutlineEntry{Heading: sec.Heading, Size: len(sec.Text)})
	}
	r.Section = sections[picked].Heading
	r.Body = sections[picked].Text
	for i := range r.Supplements {
		r.Supplements[i].Body = ""
	}
	return r, nil
}

func outlineHeadings(sections []section) string {
	names := make([]string, len(sections))
	for i, sec := range sections {
		names[i] = fmt.Sprintf("%q", headingLabel(sec.Heading))
	}
	return strings.Join(names, ", ")
}

func headingLabel(heading string) string {
	if heading == "" {
		return untitledSection
	}
	return heading
}

func loadRelations(ctx context.Context, tx *sql.Tx, id int64) ([]RelationView, error) {
	var out []RelationView
	for _, q := range []struct {
		outgoing bool
		sql      string
	}{
		{true, `SELECT r.kind, r.target, e.title FROM relations r JOIN entries e ON e.id = r.target WHERE r.source = ? ORDER BY r.target, r.kind`},
		{false, `SELECT r.kind, r.source, e.title FROM relations r JOIN entries e ON e.id = r.source WHERE r.target = ? ORDER BY r.source, r.kind`},
	} {
		rows, err := tx.QueryContext(ctx, q.sql, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			v := RelationView{Outgoing: q.outgoing}
			if err := rows.Scan(&v.Kind, &v.Other, &v.OtherTitle); err != nil {
				return nil, errors.Join(err, rows.Close())
			}
			out = append(out, v)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// artifactVersions returns the entry and every version it transitively corrects or supersedes: the
// versions of one artifact up to this one.
func artifactVersions(ctx context.Context, tx *sql.Tx, id int64) ([]int64, error) {
	versions := []int64{id}
	for {
		var prior int64
		err := tx.QueryRowContext(ctx, `SELECT target FROM relations WHERE source = ? AND kind IN (?, ?) LIMIT 1`,
			id, RelationCorrects, RelationSupersedes).Scan(&prior)
		if errors.Is(err, sql.ErrNoRows) {
			return versions, nil
		}
		if err != nil {
			return nil, err
		}
		versions = append(versions, prior)
		id = prior
	}
}

// loadSupplements returns each entry that supplements any version of the artifact read, resolved to its
// own head and listed once (MEM-RET-8).
func loadSupplements(ctx context.Context, tx *sql.Tx, id int64) ([]SupplementView, error) {
	versions, err := artifactVersions(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	var out []SupplementView
	seen := map[int64]bool{}
	for _, v := range versions {
		rows, err := tx.QueryContext(ctx, `SELECT source FROM relations WHERE target = ? AND kind = ? ORDER BY source`, v, RelationSupplements)
		if err != nil {
			return nil, err
		}
		var sources []int64
		for rows.Next() {
			var src int64
			if err := rows.Scan(&src); err != nil {
				return nil, errors.Join(err, rows.Close())
			}
			sources = append(sources, src)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, err
		}
		for _, src := range sources {
			head, err := chainHead(ctx, tx, src)
			if err != nil {
				return nil, err
			}
			if seen[head] {
				continue
			}
			seen[head] = true
			sup := SupplementView{ID: head}
			err = tx.QueryRowContext(ctx, `SELECT e.title, e.body FROM entries e WHERE e.id = ?`, head).
				Scan(&sup.Title, &sup.Body)
			if err != nil {
				return nil, err
			}
			out = append(out, sup)
		}
	}
	return out, nil
}

// Text formats the entry as the text the read tool returns.
func (r ReadResult) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d | %s | %s | %s\n", r.ID, r.Nature, r.Tier, r.Title)
	fmt.Fprintf(&b, "Author: %s | Session: %s | Date: %s\n", r.Author, r.Session, formatDate(r.CreatedAt))
	if r.Evidence != "" {
		fmt.Fprintf(&b, "Evidence: %s\n", r.Evidence)
	}
	if r.UserOrder {
		b.WriteString("Recorded by the user's direct order.\n")
	}
	if r.CurrentID != 0 {
		fmt.Fprintf(&b, "Superseded: the current version is #%d | %s\n", r.CurrentID, r.CurrentTitle)
	}
	for _, rel := range r.Relations {
		if rel.Outgoing {
			fmt.Fprintf(&b, "Relation: %s #%d | %s\n", rel.Kind, rel.Other, rel.OtherTitle)
		} else {
			fmt.Fprintf(&b, "Relation: #%d | %s %s this entry\n", rel.Other, rel.OtherTitle, rel.Kind)
		}
	}
	if r.Outline != nil {
		fmt.Fprintf(&b, "Outline (the body exceeds the read budget of %d bytes; read another section by its heading):\n", readBudgetBytes)
		for _, o := range r.Outline {
			fmt.Fprintf(&b, "- %s (%d bytes)\n", headingLabel(o.Heading), o.Size)
		}
		fmt.Fprintf(&b, "Section: %s\n", headingLabel(r.Section))
	}
	b.WriteString("\n")
	b.WriteString(r.Body)
	for _, sup := range r.Supplements {
		fmt.Fprintf(&b, "\n\nSupplement #%d | %s\n", sup.ID, sup.Title)
		b.WriteString(sup.Body)
	}
	return b.String()
}
