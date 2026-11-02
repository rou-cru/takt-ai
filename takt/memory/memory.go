// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package memory writes agent memories into Engram with harness-owned metadata, links and session anchors.
package memory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Config carries the runtime dependencies Record, Continue and Close need:
// the project root, the Engram endpoint or binary to start, and injectable
// HTTP and clock seams for tests.
type Config struct {
	Root         string
	EngramURL    string
	EngramBinary string
	HTTP         *http.Client
	Now          func() time.Time
}

// RelatesTo links one new memory to an existing Engram node.
type RelatesTo struct {
	ID       int64  `json:"id"`
	Relation string `json:"relation"`
}

// RecordRequest is one validated memory entry written by a Takt crew member.
type RecordRequest struct {
	// CallerSession is the harness-delivered host session running the tool.
	// When present, the root Session is resolved from the execution history
	// under the workspace lock instead of being accepted from the caller.
	CallerSession string     `json:"caller_session,omitempty"`
	Author        string     `json:"author"`
	Session       string     `json:"session"`
	Directory     string     `json:"directory"`
	Confirmed     bool       `json:"confirmed"`
	UserOrder     bool       `json:"user_order"`
	Nature        string     `json:"nature"`
	Scope         string     `json:"scope"`
	Title         string     `json:"title"`
	Content       string     `json:"content"`
	Evidence      string     `json:"evidence,omitempty"`
	RelatesTo     *RelatesTo `json:"relates_to,omitempty"`
}

// RecordResult reports the stored entry ID and whether deduplication absorbed the write.
type RecordResult struct {
	ID           int64 `json:"id"`
	Deduplicated bool  `json:"deduplicated"`
}

// ContinueRequest declares the earlier session this one continues; an empty
// PreviousSession withdraws the declaration.
type ContinueRequest struct {
	// CallerSession is the harness-delivered host session running the tool.
	// When present, the root Session is resolved from the execution history
	// under the workspace lock instead of being accepted from the caller.
	CallerSession   string `json:"caller_session,omitempty"`
	Author          string `json:"author"`
	Session         string `json:"session"`
	Directory       string `json:"directory"`
	PreviousSession string `json:"previous_session"`
}

// CloseRequest ends a session and optionally records its outcome.
type CloseRequest struct {
	// CallerSession is the harness-delivered host session running the tool.
	// When present, the root Session is resolved from the execution history
	// under the workspace lock instead of being accepted from the caller.
	CallerSession string `json:"caller_session,omitempty"`
	Author        string `json:"author"`
	Session       string `json:"session"`
	Directory     string `json:"directory"`
	Objective     string `json:"objective,omitempty"`
	State         string `json:"state,omitempty"`
	Fallback      bool   `json:"fallback"`
}

// CloseResult reports the end anchor and how many entries the session recorded.
type CloseResult struct {
	EndAnchorID int64 `json:"end_anchor_id"`
	Entries     int   `json:"entries"`
}

// ValidationError is a contract rejection whose Reason is shown verbatim to the agent.
type ValidationError struct{ Reason string }

// Error returns the rejection reason so callers render it without unwrapping.
func (e *ValidationError) Error() string { return e.Reason }

func reject(format string, args ...any) error {
	return &ValidationError{Reason: fmt.Sprintf(format, args...)}
}

var natures = map[string]bool{"proposal": true, "decision": true, "observation": true, "hypothesis": true}

// safeContentLimitBytes margins Engram's 50,000-byte write cap, which
// truncates silently past it with no signal on a later read.
const safeContentLimitBytes = 45000

const (
	scopeProject  = "project"
	scopePersonal = "personal"
	// The two canonical memory tiers: the global tier crosses projects, the
	// workspace tier belongs to the active repository.
	tierGlobal    = "global"
	tierWorkspace = "workspace"
	// Engram verbs stored on evolution edges.
	edgeSupersedes = "supersedes"
	edgeConflicts  = "conflicts_with"
	edgeScoped     = "scoped"
	// Applicability is derived from recorded evolution and never stored on the
	// entry: nature says what an entry is, applicability whether it still governs.
	applicabilityCurrent    = "current"
	applicabilitySuperseded = "superseded"
	applicabilityDisputed   = "disputed"
)

// relationEdges maps the agent-facing relation to the Engram verb stored on the edge.
var relationEdges = map[string]string{
	"corrects":     edgeSupersedes,
	"supersedes":   edgeSupersedes,
	"supplements":  "compatible",
	"exception-to": edgeScoped,
	"disputes":     edgeConflicts,
}

const codeFence = "```"

func tierOf(scope string) string {
	if scope == scopePersonal {
		return tierGlobal
	}
	return tierWorkspace
}

// recordProject binds a workspace-tier entry to the active project and leaves a
// global-tier entry unbound, so the tiers stay partitioned.
func recordProject(scope, project string) string {
	if tierOf(scope) == tierGlobal {
		return ""
	}
	return project
}

// targetApplicability reads, from the edge verb of a relation, what that
// relation does to the entry it points at.
func targetApplicability(relation string) string {
	switch relationEdges[relation] {
	case edgeSupersedes:
		return applicabilitySuperseded
	case edgeConflicts:
		return applicabilityDisputed
	}
	return applicabilityCurrent
}

// applicability derives which entries stopped governing from the evolution
// links recorded against them, without waiting for any consolidation to run.
func applicability(entries []sessionIndexEntry) map[int64]string {
	out := map[int64]string{}
	for _, e := range entries {
		state := targetApplicability(e.Relation)
		if state == applicabilityCurrent {
			continue
		}
		if state == applicabilitySuperseded || out[e.Target] == "" {
			out[e.Target] = state
		}
	}
	return out
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func authorRole(author string) (model.RoleClass, error) {
	if author == shared.OrchestratorID {
		return model.RoleOrchestrator, nil
	}
	content, err := catalog.LoadNativeContent()
	if err != nil {
		return "", fmt.Errorf("load crew catalog: %w", err)
	}
	entry, ok := content[author]
	if !ok {
		return "", reject("author %q is not a Takt crew member", author)
	}
	return entry.Role, nil
}

func requireSession(session, directory string) error {
	if strings.TrimSpace(session) == "" || strings.TrimSpace(directory) == "" {
		return reject("session and directory are required")
	}
	if session == "." || session == ".." || !sessionIDPattern.MatchString(session) {
		return reject("session id %q is not a valid session identifier", session)
	}
	return nil
}

func now(cfg Config) string {
	if cfg.Now != nil {
		return cfg.Now().UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}

type openSession struct {
	lock    *os.File
	index   *sessionIndex
	client  *engramClient
	project string
}

// lock takes the session index lock and loads the index (nil when new); the caller must Close s.lock.
func lock(ctx context.Context, cfg Config, session string) (*openSession, error) {
	return openLocked(ctx, cfg, session, lockSession)
}

// openLocked returns (nil, nil) when take finds no lock to take.
func openLocked(ctx context.Context, cfg Config, session string, take func(context.Context, string, string) (*os.File, error)) (*openSession, error) {
	f, err := take(ctx, cfg.Root, session)
	if err != nil || f == nil {
		return nil, err
	}
	l, err := loadSessionIndex(cfg.Root, session)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return &openSession{lock: f, index: l, client: newClient(cfg)}, nil
}

func (s *openSession) resolveProject(ctx context.Context, directory string) string {
	if s.index != nil && s.index.Project != "" {
		return s.index.Project
	}
	if s.project == "" {
		s.project = s.client.project(ctx, directory)
	}
	return s.project
}

// start lazily creates the Engram session and its start anchor, then fixes any
// declared continuity; callers run ensureServe first.
func (s *openSession) start(ctx context.Context, cfg Config, session, directory string) error {
	if s.index == nil || s.index.StartAnchor == 0 {
		if err := s.anchorStart(ctx, cfg, session, directory); err != nil {
			return err
		}
	}
	return s.fixContinuity(ctx, cfg)
}

func (s *openSession) anchorStart(ctx context.Context, cfg Config, session, directory string) error {
	l := s.index
	if l == nil {
		l = &sessionIndex{Session: session, Directory: directory, Entries: []sessionIndexEntry{}}
		s.index = l
	}
	if l.Project == "" {
		l.Project = s.resolveProject(ctx, directory)
	}
	body := map[string]string{"id": session, "project": l.Project, "directory": l.Directory}
	if _, err := s.client.call(ctx, http.MethodPost, "/sessions", body, nil); err != nil {
		return err
	}
	id, err := s.client.addObservation(ctx, observation{
		SessionID: session,
		Type:      "session_anchor",
		Title:     fmt.Sprintf("[takt] Session %s started", session),
		Content:   fmt.Sprintf("**Session**: %s\n**Directory**: %s\n**Started**: %s", session, l.Directory, now(cfg)),
		ToolName:  harnessTool,
		Project:   l.Project,
		Scope:     "project",
	})
	if err != nil {
		return err
	}
	l.StartAnchor = id
	return saveSessionIndex(cfg.Root, l)
}

// fixContinuity links the start anchor to the declared previous session. It
// runs on the session's first entry, so the declaration stays revocable until
// then. Whether the user chose that session is the declaring agent's word; the
// harness checks only that the session exists and is closed.
func (s *openSession) fixContinuity(ctx context.Context, cfg Config) error {
	l := s.index
	if l.PendingContinues == "" {
		return nil
	}
	prevEnd, err := closedPreviousEnd(ctx, cfg, l.PendingContinues)
	if err != nil {
		return err
	}
	if err := s.client.link(ctx, l.StartAnchor, prevEnd, "related", "takt:continues", harnessTool); err != nil {
		return err
	}
	l.Continues, l.PendingContinues = l.PendingContinues, ""
	return saveSessionIndex(cfg.Root, l)
}

// Record validates and writes one memory into Engram under the session's
// lock. A new entry resumes a previously closed host conversation; historical
// close anchors remain in Engram. Contract violations are still rejected.
func Record(ctx context.Context, cfg Config, req RecordRequest) (result RecordResult, err error) {
	if isMatrixTitle(req.Title) {
		req.Title = matrixEntryTitle(req.Title)
	}
	if err := validateRecord(req); err != nil {
		return RecordResult{}, err
	}
	s, err := lock(ctx, cfg, req.Session)
	if err != nil {
		return RecordResult{}, err
	}
	defer func() { err = errors.Join(err, s.lock.Close()) }()
	if req, err = s.prepareRecord(ctx, cfg, req); err != nil {
		return RecordResult{}, err
	}
	return s.persistRecord(ctx, cfg, req)
}

// ValidateResultIDs checks that a handoff names distinct, existing Engram entries.
// Content and suitability remain the orchestrator's judgment.
func ValidateResultIDs(ctx context.Context, cfg Config, ids []int64) error {
	if len(ids) == 0 {
		return reject("handoff requires an Engram ID for each delivered result")
	}
	c := newClient(cfg)
	if err := c.ensureServe(ctx); err != nil {
		return err
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return reject("handoff requires distinct positive Engram IDs")
		}
		seen[id] = true
		var entry map[string]any
		if _, err := c.call(ctx, http.MethodGet, "/observations/"+strconv.FormatInt(id, 10), nil, &entry); err != nil {
			return fmt.Errorf("handoff Engram entry #%d: %w", id, err)
		}
	}
	return nil
}

// ValidateSessionResultIDs accepts only results author itself recorded in
// session: an existing entry from another author, session or project is not
// this delivery's result.
func ValidateSessionResultIDs(ctx context.Context, cfg Config, session, author string, ids []int64) error {
	if len(ids) == 0 {
		return reject("handoff requires an Engram ID for each delivered result")
	}
	own, err := EntryIDsByAuthor(cfg.Root, session, author)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !slices.Contains(own, id) {
			return reject("result #%d was not recorded by %s in this session; record the result with memory_record first", id, author)
		}
	}
	return ValidateResultIDs(ctx, cfg, ids)
}

// prepareRecord checks what a write depends on outside the request: the related
// entry, the matrix column the author may write, and the session start.
func (s *openSession) prepareRecord(ctx context.Context, cfg Config, req RecordRequest) (RecordRequest, error) {
	if err := s.client.ensureServe(ctx); err != nil {
		return req, err
	}
	project := recordProject(req.Scope, s.resolveProject(ctx, req.Directory))

	target, err := checkRelatesTo(ctx, s.client, req.RelatesTo, project)
	if err != nil {
		return req, err
	}
	if req.Title, err = checkMatrixColumn(req, target); err != nil {
		return req, err
	}
	return req, s.start(ctx, cfg, req.Session, req.Directory)
}

func (s *openSession) persistRecord(ctx context.Context, cfg Config, req RecordRequest) (RecordResult, error) {
	c, l := s.client, s.index
	id, err := c.addObservation(ctx, observation{
		SessionID: req.Session,
		Type:      req.Nature,
		Title:     fmt.Sprintf("[%s] %s", req.Author, req.Title),
		Content:   renderEntry(req),
		ToolName:  req.Author,
		Project:   recordProject(req.Scope, l.Project),
		Scope:     req.Scope,
	})
	if err != nil {
		return RecordResult{}, err
	}
	if l.hasEntry(id) {
		// A deduplicated write still names its caller, so a session recycled to
		// consume the entry is recognized; memory stays closed if it was.
		if l.addCaller(id, req.CallerSession) {
			if err := saveSessionIndex(cfg.Root, l); err != nil {
				return RecordResult{}, err
			}
		}
		return RecordResult{ID: id, Deduplicated: true}, nil
	}
	if err := s.recordLinks(ctx, cfg, req, id); err != nil {
		return RecordResult{}, err
	}
	return RecordResult{ID: id}, nil
}

func (s *openSession) recordLinks(ctx context.Context, cfg Config, req RecordRequest, id int64) error {
	c, l := s.client, s.index
	if err := c.link(ctx, id, l.StartAnchor, "related", "takt:in-session", req.Author); err != nil {
		return err
	}
	entry := sessionIndexEntry{ID: id, Author: req.Author, Caller: req.CallerSession, Nature: req.Nature, Scope: req.Scope, Title: req.Title, At: now(cfg)}
	if rel := req.RelatesTo; rel != nil {
		if err := c.link(ctx, id, rel.ID, relationEdges[rel.Relation], "takt:"+rel.Relation, req.Author); err != nil {
			return err
		}
		entry.Relation, entry.Target = rel.Relation, rel.ID
	}
	l.Entries = append(l.Entries, entry)
	// OpenCode reuses its root session when the user continues after a reported
	// result. Only a successfully linked new entry reopens memory: failed writes
	// and deduplicated retries leave the previous close intact.
	l.EndAnchor = 0
	return saveSessionIndex(cfg.Root, l)
}

// relatedEntry is the stored title of the entry a record relates to, as Engram
// holds it: "[author] Title".
type relatedEntry struct{ title string }

// checkRelatesTo requires rel, when given, to name an existing memory entry of
// the same tier: the same project, or the unbound global tier. It returns the
// related entry's stored title.
func checkRelatesTo(ctx context.Context, c *engramClient, rel *RelatesTo, project string) (relatedEntry, error) {
	if rel == nil {
		return relatedEntry{}, nil
	}
	var target struct {
		Project *string `json:"project"`
		Type    string  `json:"type"`
		Title   string  `json:"title"`
	}
	status, err := c.call(ctx, http.MethodGet, "/observations/"+strconv.FormatInt(rel.ID, 10), nil, &target)
	if status == http.StatusNotFound {
		return relatedEntry{}, reject("relates_to #%d does not exist", rel.ID)
	}
	if err != nil {
		return relatedEntry{}, err
	}
	if target.Type == "session_anchor" {
		return relatedEntry{}, reject("relates_to #%d is a session anchor, not a memory entry", rel.ID)
	}
	if targetProject(target.Project) != project {
		if project == "" {
			return relatedEntry{}, reject("relates_to #%d is a workspace entry; a global memory relates only to global memories", rel.ID)
		}
		return relatedEntry{}, reject("relates_to #%d belongs to another project; relations stay inside project %q", rel.ID, project)
	}
	return relatedEntry{title: target.Title}, nil
}

// matrixTitle names the traceability matrix and, followed by ": <column>",
// each entry that writes one of its columns (PR-QA-9).
const matrixTitle = "Traceability Matrix"

// matrixColumns maps each traceability matrix column to its sole author.
var matrixColumns = map[string]string{
	"Requirements": "qa",
	"Owning node":  "tpm",
	"Evidence":     "verify",
}

// ownColumn is the matrix column author writes, or "" when it writes none.
func ownColumn(author string) string {
	for column, owner := range matrixColumns {
		if owner == author {
			return column
		}
	}
	return ""
}

// declaredColumn is the matrix column a title declares, or "" when it declares none.
func declaredColumn(title string) string {
	column, ok := strings.CutPrefix(matrixEntryTitle(title), matrixTitle+":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(column)
}

// columnTitle is the exact title an entry writing column carries.
func columnTitle(column string) string { return matrixTitle + ": " + column }

// matrixEntryTitle removes attribution, which identifies the writer, not the
// artifact. Column ownership also applies to entries stored by tpm or verify.
func matrixEntryTitle(title string) string {
	title = strings.TrimSpace(title)
	for strings.HasPrefix(title, "[") {
		_, rest, ok := strings.Cut(title, "] ")
		if !ok {
			break
		}
		title = strings.TrimSpace(rest)
	}
	return title
}

func isMatrixTitle(title string) bool {
	title = matrixEntryTitle(title)
	return title == matrixTitle || strings.HasPrefix(title, matrixTitle+":")
}

// checkMatrixColumn keeps each traceability matrix column to its sole author
// and returns the title the entry is stored under: only qa creates the matrix,
// and an entry that adds to or revises it writes its author's own column, which
// the harness derives from the author and titles itself. A dispute stays open
// to any author, as the way to state a contradiction.
//
// Column entries must relate to the matrix or their own column; tpm and verify
// must use supplements when targeting the matrix itself. Requests unrelated to
// the matrix and disputes keep their title. Violations return a *ValidationError.
func checkMatrixColumn(req RecordRequest, target relatedEntry) (string, error) {
	// A dispute records disagreement, never an authorized column evolution.
	if req.RelatesTo != nil && req.RelatesTo.Relation == "disputes" {
		return req.Title, nil
	}
	own := ownColumn(req.Author)
	requested, related := isMatrixTitle(req.Title), isMatrixTitle(target.title)
	if !requested && !related {
		return req.Title, nil
	}
	if matrixEntryTitle(req.Title) == matrixTitle && !related && req.Author == matrixColumns["Requirements"] {
		return req.Title, nil
	}
	switch {
	case own == "":
		return "", reject("only qa, tpm and verify write the traceability matrix; record your fact under another title and use disputes to state a contradiction")
	case !related:
		return "", reject("a matrix column relates to the matrix or its column entry with supplements")
	case declaredColumn(target.title) != "" && declaredColumn(target.title) != own:
		return "", reject("the related entry writes %s; only %s may evolve that column; use disputes to state a contradiction", declaredColumn(target.title), matrixColumns[declaredColumn(target.title)])
	// Only qa revises the matrix itself; another column only adds to it.
	case matrixEntryTitle(target.title) == matrixTitle && req.Author != matrixColumns["Requirements"] && req.RelatesTo.Relation != "supplements":
		return "", reject("the traceability matrix is qa's; relate your column to it with supplements")
	}
	return columnTitle(own), nil
}

// targetProject reports the project an existing entry is bound to, or "" when it
// belongs to the global tier.
func targetProject(p *string) string {
	if p == nil || strings.TrimSpace(*p) == "" {
		return ""
	}
	return normalizeProject(*p)
}

func validateRecord(req RecordRequest) error {
	if _, err := authorRole(req.Author); err != nil {
		return err
	}
	if err := requireSession(req.Session, req.Directory); err != nil {
		return err
	}
	if err := validateShape(req); err != nil {
		return err
	}
	if err := validateSize(req); err != nil {
		return err
	}
	if err := validateTier(req); err != nil {
		return err
	}
	if err := validateAuthority(req); err != nil {
		return err
	}
	return validateRelation(req.RelatesTo)
}

// validateShape checks the fields every record needs.
func validateShape(req RecordRequest) error {
	if !natures[req.Nature] {
		return reject("nature must be one of proposal, decision, observation, hypothesis")
	}
	if err := validateScope(req.Scope); err != nil {
		return err
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" {
		return reject("title and content are required")
	}
	return nil
}

// validateScope accepts the two scopes that name the memory tiers.
func validateScope(scope string) error {
	if scope != scopeProject && scope != scopePersonal {
		return reject("scope must be project or personal")
	}
	return nil
}

// validateSize measures the exact string Engram will receive.
func validateSize(req RecordRequest) error {
	if n := len(renderEntry(req)); n > safeContentLimitBytes {
		return reject("entry is %d bytes, over the %d-byte limit; split it into supplements", n, safeContentLimitBytes)
	}
	return nil
}

// validateTier keeps workspace artifacts out of the global tier: crossing tiers
// is a deliberate write, and a project agent never lands repository content in
// the cross-project tier.
func validateTier(req RecordRequest) error {
	if tierOf(req.Scope) != tierGlobal {
		return nil
	}
	for _, text := range []string{req.Title, req.Content, req.Evidence} {
		if strings.Contains(text, codeFence) || strings.Contains(text, req.Directory) {
			return reject("the global tier holds cross-project user preferences: a code excerpt or the workspace directory belongs in a project-scope memory")
		}
	}
	return nil
}

// validateAuthority checks what the nature and scope of a record demand of
// who writes it: evidence or confirmation.
func validateAuthority(req RecordRequest) error {
	if req.Nature == "observation" && strings.TrimSpace(req.Evidence) == "" {
		return reject("an observation requires evidence: name the file, command output or source that shows it")
	}
	if req.Nature == "decision" && strings.TrimSpace(req.Evidence) == "" {
		return reject("a decision requires evidence: quote the user's approval or name the memory entry #id that records it")
	}
	if req.Scope == scopePersonal && !req.Confirmed {
		return reject("a personal memory requires the user's confirmation")
	}
	return nil
}

func validateRelation(rel *RelatesTo) error {
	if rel == nil {
		return nil
	}
	if _, ok := relationEdges[rel.Relation]; !ok {
		return reject("relates_to relation must be one of corrects, supersedes, supplements, exception-to, disputes")
	}
	if rel.ID <= 0 {
		return reject("relates_to id must be a positive memory id")
	}
	return nil
}

func renderEntry(req RecordRequest) string {
	authority := "author"
	switch {
	// Only a confirmed user order is stamped as the user's own; a decision's
	// approval otherwise is the author's quote, not one the harness saw given.
	case req.UserOrder:
		authority = "user"
	case req.Nature == "decision":
		authority = "user, as quoted by " + req.Author + " — " + req.Evidence
	case strings.TrimSpace(req.Evidence) != "":
		authority = "evidence: " + req.Evidence
	}
	relation := "none"
	if r := req.RelatesTo; r != nil {
		relation = fmt.Sprintf("%s #%d", r.Relation, r.ID)
		if state := targetApplicability(r.Relation); state != applicabilityCurrent {
			relation += fmt.Sprintf(" (#%d is %s)", r.ID, state)
		}
	}
	return fmt.Sprintf("**Author**: %s\n**Nature**: %s\n**Tier**: %s\n**Authority**: %s\n**Session**: %s\n**Relation**: %s\n\n%s",
		req.Author, req.Nature, tierOf(req.Scope), authority, req.Session, relation, req.Content)
}

// Continue declares the previous session the current one continues; an empty
// PreviousSession withdraws the declaration. The link is fixed by start on the
// session's first entry, so a wrong declaration never becomes permanent. The
// previous session may belong to any project: one objective can span several.
func Continue(ctx context.Context, cfg Config, req ContinueRequest) (err error) {
	if err := validateContinueRequest(req); err != nil {
		return err
	}
	// The previous session is locked and released before this session's lock is taken: never two held at once.
	if req.PreviousSession != "" {
		if _, err := closedPreviousEnd(ctx, cfg, req.PreviousSession); err != nil {
			return err
		}
	}
	s, err := lock(ctx, cfg, req.Session)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.lock.Close()) }()
	l := s.index
	if l != nil && l.Continues != "" {
		return reject("this session already continues %q", l.Continues)
	}
	if l != nil && l.StartAnchor != 0 {
		return reject("this session already recorded memory; continuity is declared before its first entry")
	}
	if l == nil {
		if req.PreviousSession == "" {
			return nil
		}
		l = &sessionIndex{Session: req.Session, Directory: req.Directory, Entries: []sessionIndexEntry{}}
	}
	l.PendingContinues = req.PreviousSession
	return saveSessionIndex(cfg.Root, l)
}

func validateContinueRequest(req ContinueRequest) error {
	if err := holdsSession(req.Author, req.Session, req.Directory); err != nil {
		return err
	}
	if err := callerHolds(req.CallerSession, req.Session, req.Directory); err != nil {
		return err
	}
	if err := requireSession(req.Session, req.Directory); err != nil {
		return err
	}
	if req.PreviousSession == "" {
		return nil
	}
	if err := requireSession(req.PreviousSession, req.Directory); err != nil {
		return err
	}
	if req.PreviousSession == req.Session {
		return reject("a session cannot continue itself")
	}
	return nil
}

// closedPreviousEnd returns the end anchor of a closed previous session, read
// under its lock without creating it. A closed session holds no pending
// continuity, so taking this lock under the caller's own never waits in a cycle.
func closedPreviousEnd(ctx context.Context, cfg Config, previous string) (end int64, err error) {
	s, err := openLocked(ctx, cfg, previous, lockExistingSession)
	if err != nil {
		return 0, err
	}
	if s == nil {
		return 0, reject("previous session %q has no closed record to continue from", previous)
	}
	defer func() { err = errors.Join(err, s.lock.Close()) }()
	if s.index == nil || s.index.EndAnchor == 0 {
		return 0, reject("previous session %q has no closed record to continue from", previous)
	}
	return s.index.EndAnchor, nil
}

// CapabilitiesRequest identifies a harness-resolved caller without model input.
type CapabilitiesRequest struct {
	// CallerSession is the harness-delivered host session running the tool.
	// When present, the root Session is resolved from the execution history
	// under the workspace lock instead of being accepted from the caller.
	CallerSession string `json:"caller_session,omitempty"`
	Author        string `json:"author"`
	Session       string `json:"session"`
	Directory     string `json:"directory"`
}

// Capabilities projects session ownership without writing memory. An unavailable
// ownership authority hides lifecycle operations, but never prevents recording.
func Capabilities(req CapabilitiesRequest) []string {
	tools := []string{"memory_record"}
	if requireSession(req.Session, req.Directory) == nil && holdsSession(req.Author, req.Session, req.Directory) == nil &&
		callerHolds(req.CallerSession, req.Session, req.Directory) == nil {
		tools = append(tools, "memory_continue_session", "memory_close_session")
	}
	return tools
}

// holdsSession accepts the orchestrator, or the interface-holding role only
// when it is either the base (no switch recorded) or the temporary holder the
// interlocutor stack (IR-14..17) currently has on session's root.
func holdsSession(author, session, directory string) (err error) {
	role, err := authorRole(author)
	if err != nil {
		return err
	}
	if role == model.RoleOrchestrator {
		return nil
	}
	if !role.HoldsInterface() {
		return reject("only the agent holding the user conversation links or closes a session")
	}
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return reject("workspace directory is not a directory")
	}
	// Read the same private store dispatch uses, never a history at workspace root.
	projection, err := history.ReadProjection(history.StateDir(directory))
	if err != nil {
		return err
	}
	budgets := projection.Budgets(session)
	if budgets.InterlocutorHolder == "" || author == budgets.InterlocutorAgent {
		return nil
	}
	return reject("only the agent holding the user conversation links or closes a session")
}

// callerHolds refuses a caller session other than the root itself or the
// session the history records as the root's interlocutor holder: another
// session of the holder's agent does not hold the conversation. An empty
// caller names no session to check.
func callerHolds(caller, session, directory string) error {
	if caller == "" || caller == session {
		return nil
	}
	projection, err := history.ReadProjection(history.StateDir(directory))
	if err != nil {
		return err
	}
	if holder := projection.Budgets(session).InterlocutorHolder; holder != caller {
		return reject("only the agent holding the user conversation links or closes a session")
	}
	return nil
}

// Close ends a session: only the session holder may close one unless it is a fallback.
// A fallback close is idempotent and reads its facts from the session index alone:
// it needs no directory, and a session that is unknown, empty or already closed is a no-op.
func Close(ctx context.Context, cfg Config, req CloseRequest) (result CloseResult, err error) {
	if req.Fallback {
		return fallbackClose(ctx, cfg, req)
	}
	if err := requireSession(req.Session, req.Directory); err != nil {
		return CloseResult{}, err
	}
	if err := holdsSession(req.Author, req.Session, req.Directory); err != nil {
		return CloseResult{}, err
	}
	if err := callerHolds(req.CallerSession, req.Session, req.Directory); err != nil {
		return CloseResult{}, err
	}
	// An explicit close of a never-recorded session still records it: start and end anchors (MEM-OPS-4/5).
	s, err := lock(ctx, cfg, req.Session)
	if err != nil {
		return CloseResult{}, err
	}
	defer func() { err = errors.Join(err, s.lock.Close()) }()
	if s.index != nil && s.index.EndAnchor != 0 {
		return CloseResult{}, reject("session %q is already closed", req.Session)
	}
	return finishClose(ctx, cfg, req, s)
}

func fallbackClose(ctx context.Context, cfg Config, req CloseRequest) (result CloseResult, err error) {
	if err := requireSession(req.Session, "-"); err != nil {
		return CloseResult{}, err
	}
	// A session with no lock file never recorded anything: leave no file behind.
	s, err := openLocked(ctx, cfg, req.Session, lockExistingSession)
	if err != nil || s == nil {
		return CloseResult{}, err
	}
	defer func() { err = errors.Join(err, s.lock.Close()) }()
	if emptyIndex(s.index) || s.index.EndAnchor != 0 {
		return CloseResult{}, nil
	}
	req.Directory = s.index.Directory
	return finishClose(ctx, cfg, req, s)
}

func finishClose(ctx context.Context, cfg Config, req CloseRequest, s *openSession) (CloseResult, error) {
	if err := s.client.ensureServe(ctx); err != nil {
		return CloseResult{}, err
	}
	if err := s.start(ctx, cfg, req.Session, req.Directory); err != nil {
		return CloseResult{}, err
	}
	l, c := s.index, s.client

	endID, err := c.addObservation(ctx, observation{
		SessionID: req.Session,
		Type:      "session_anchor",
		Title:     fmt.Sprintf("[takt] Session %s closed", req.Session),
		Content:   renderClose(l, req, now(cfg)),
		ToolName:  harnessTool,
		Project:   l.Project,
		Scope:     "project",
	})
	if err != nil {
		return CloseResult{}, err
	}
	if err := linkClosed(ctx, c, l, endID); err != nil {
		return CloseResult{}, err
	}
	l.EndAnchor = endID
	if err := saveSessionIndex(cfg.Root, l); err != nil {
		return CloseResult{}, err
	}
	return CloseResult{EndAnchorID: endID, Entries: len(l.Entries)}, nil
}

func emptyIndex(l *sessionIndex) bool { return l == nil || len(l.Entries) == 0 }

// linkClosed anchors the end record to the session start and every entry to it.
func linkClosed(ctx context.Context, c *engramClient, l *sessionIndex, endID int64) error {
	if err := c.link(ctx, endID, l.StartAnchor, "related", "takt:session-end", harnessTool); err != nil {
		return err
	}
	for _, e := range l.Entries {
		if err := c.link(ctx, e.ID, endID, "related", "takt:closed-in", harnessTool); err != nil {
			return err
		}
	}
	return nil
}

func orNotStated(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Not stated"
	}
	return s
}

// applicabilityNote marks an entry the recorded evolution took out of force, so
// the narrative never reads it back as a current directive and never settles an
// open disagreement by recency.
func applicabilityNote(state string) string {
	switch state {
	case applicabilitySuperseded:
		return " (superseded: history, not a current directive)"
	case applicabilityDisputed:
		return " (disputed: open, not settled by recency)"
	}
	return ""
}

func renderClose(l *sessionIndex, req CloseRequest, closedAt string) string {
	continues := l.Continues
	if continues == "" {
		continues = "none"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Session**: %s\n**Closed**: %s\n**Continues**: %s\n\n", l.Session, closedAt, continues)
	fmt.Fprintf(&b, "## Objective\n%s\n\n## What happened\n", orNotStated(req.Objective))
	if len(l.Entries) == 0 {
		b.WriteString("No entries recorded\n")
	}
	var relations []string
	states := applicability(l.Entries)
	for i, e := range l.Entries {
		fmt.Fprintf(&b, "%d. %s [%s] %s #%d — %s%s\n", i+1, e.At, e.Author, e.Nature, e.ID, e.Title, applicabilityNote(states[e.ID]))
		if e.Relation != "" {
			relations = append(relations, fmt.Sprintf("- #%d %s #%d", e.ID, e.Relation, e.Target))
		}
	}
	if len(relations) > 0 {
		fmt.Fprintf(&b, "\n## How the entries relate\n%s\n", strings.Join(relations, "\n"))
	}
	fmt.Fprintf(&b, "\n## State at close\n%s", orNotStated(req.State))
	return b.String()
}
