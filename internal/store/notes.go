package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Note struct {
	ID        string
	UserID    string
	Title     string
	Content   string
	Tags      []string
	Pinned    bool
	Archived  bool
	ProjectID string
	Space     string
	Created   time.Time
	Updated   time.Time
}

// NotePatch carries the fields an update changes; nil leaves a field alone.
type NotePatch struct {
	Title     *string
	Content   *string
	Tags      *[]string
	Pinned    *bool
	Archived  *bool
	ProjectID *string
	Space     *string
}

type NoteFilter struct {
	Tag             string
	PinnedOnly      bool
	IncludeArchived bool
	ProjectID       string
	Space           string
	Limit           int
	Offset          int
}

type NoteHit struct {
	Note    Note
	Snippet string
	Score   float64
}

const (
	maxTitle   = 500
	maxContent = 1 << 20
	maxTags    = 32
	maxTagLen  = 50
)

// NormalizeTags lower-cases, trims, de-duplicates and bounds a tag list.
func NormalizeTags(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")))
		if t == "" || seen[t] {
			continue
		}
		if utf8.RuneCountInString(t) > maxTagLen {
			return nil, invalid("tag %q is longer than %d characters", t, maxTagLen)
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > maxTags {
		return nil, invalid("at most %d tags", maxTags)
	}
	sort.Strings(out)
	return out, nil
}

func checkNote(n Note) error {
	if strings.TrimSpace(n.Title) == "" && strings.TrimSpace(n.Content) == "" {
		return invalid("a note needs a title or content")
	}
	if utf8.RuneCountInString(n.Title) > maxTitle {
		return invalid("title is longer than %d characters", maxTitle)
	}
	if len(n.Content) > maxContent {
		return invalid("content is larger than %d bytes", maxContent)
	}
	return nil
}

func (s *Store) CreateNote(ctx context.Context, n Note) (Note, error) {
	var err error
	if n.Tags, err = NormalizeTags(n.Tags); err != nil {
		return Note{}, err
	}
	if err := checkNote(n); err != nil {
		return Note{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	n.ID, n.Created, n.Updated = uuid.NewString(), now, now
	if err := s.checkProject(ctx, n.UserID, n.ProjectID); err != nil {
		return Note{}, err
	}
	if n.Space, err = s.resolveSpace(ctx, n.UserID, n.Space, n.ProjectID); err != nil {
		return Note{}, err
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO notes(id, user_id, title, content, pinned, archived, project_id, space, created_ms, updated_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.UserID, n.Title, n.Content, b2i(n.Pinned), b2i(n.Archived), n.ProjectID, n.Space, ms(n.Created), ms(n.Updated))
		if err != nil {
			return err
		}
		return writeNoteIndex(ctx, tx, n)
	})
	return n, err
}

func writeNoteIndex(ctx context.Context, tx *sql.Tx, n Note) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id = ?`, n.ID); err != nil {
		return err
	}
	for _, t := range n.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO note_tags(note_id, tag) VALUES (?, ?)`, n.ID, t); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM notes_fts WHERE note_id = ?`, n.ID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO notes_fts(note_id, user_id, title, content, tags) VALUES (?, ?, ?, ?, ?)`,
		n.ID, n.UserID, n.Title, n.Content, strings.Join(n.Tags, " "))
	return err
}

const noteCols = `id, user_id, title, content, pinned, archived, project_id, space, created_ms, updated_ms`

type scanner interface{ Scan(...any) error }

func scanNote(r scanner) (Note, error) {
	var n Note
	var pinned, archived int
	var created, updated int64
	if err := r.Scan(&n.ID, &n.UserID, &n.Title, &n.Content, &pinned, &archived, &n.ProjectID, &n.Space, &created, &updated); err != nil {
		return Note{}, err
	}
	n.Pinned, n.Archived = pinned != 0, archived != 0
	n.Created, n.Updated = fromMS(created), fromMS(updated)
	return n, nil
}

func (s *Store) loadTags(ctx context.Context, table, col string, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	q := `SELECT ` + col + `, tag FROM ` + table + ` WHERE ` + col + ` IN (?` + strings.Repeat(",?", len(ids)-1) + `) ORDER BY tag`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, err
		}
		out[id] = append(out[id], tag)
	}
	return out, rows.Err()
}

func (s *Store) withNoteTags(ctx context.Context, notes []Note) error {
	ids := make([]string, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	tags, err := s.loadTags(ctx, "note_tags", "note_id", ids)
	if err != nil {
		return err
	}
	for i := range notes {
		notes[i].Tags = tags[notes[i].ID]
	}
	return nil
}

func (s *Store) GetNote(ctx context.Context, userID, id string) (Note, error) {
	n, err := scanNote(s.db.QueryRowContext(ctx, `SELECT `+noteCols+` FROM notes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, err
	}
	ns := []Note{n}
	if err := s.withNoteTags(ctx, ns); err != nil {
		return Note{}, err
	}
	return ns[0], nil
}

func (s *Store) UpdateNote(ctx context.Context, userID, id string, p NotePatch) (Note, error) {
	n, err := s.GetNote(ctx, userID, id)
	if err != nil {
		return Note{}, err
	}
	if p.Title != nil {
		n.Title = *p.Title
	}
	if p.Content != nil {
		n.Content = *p.Content
	}
	if p.Tags != nil {
		if n.Tags, err = NormalizeTags(*p.Tags); err != nil {
			return Note{}, err
		}
	}
	if p.Pinned != nil {
		n.Pinned = *p.Pinned
	}
	if p.Archived != nil {
		n.Archived = *p.Archived
	}
	if p.ProjectID != nil {
		n.ProjectID = *p.ProjectID
		if err := s.checkProject(ctx, userID, n.ProjectID); err != nil {
			return Note{}, err
		}
	}
	if p.Space != nil {
		if *p.Space != SpaceWork && *p.Space != SpaceLife {
			return Note{}, invalid("space must be work or life")
		}
		n.Space = *p.Space
	}
	if err := checkNote(n); err != nil {
		return Note{}, err
	}
	n.Updated = time.Now().UTC().Truncate(time.Millisecond)
	err = s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE notes SET title = ?, content = ?, pinned = ?, archived = ?, project_id = ?, space = ?, updated_ms = ?
			WHERE id = ? AND user_id = ?`,
			n.Title, n.Content, b2i(n.Pinned), b2i(n.Archived), n.ProjectID, n.Space, ms(n.Updated), n.ID, userID)
		if err != nil {
			return err
		}
		return writeNoteIndex(ctx, tx, n)
	})
	return n, err
}

func (s *Store) DeleteNote(ctx context.Context, userID, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE id = ? AND user_id = ?`, id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM notes_fts WHERE note_id = ?`, id)
		return err
	})
}

// ListNotes returns pinned notes first, then most recently updated.
func (s *Store) ListNotes(ctx context.Context, userID string, f NoteFilter) ([]Note, error) {
	q := `SELECT ` + noteCols + ` FROM notes n WHERE user_id = ?`
	args := []any{userID}
	if !f.IncludeArchived {
		q += ` AND archived = 0`
	}
	if f.PinnedOnly {
		q += ` AND pinned = 1`
	}
	if f.Tag != "" {
		q += ` AND EXISTS (SELECT 1 FROM note_tags t WHERE t.note_id = n.id AND t.tag = ?)`
		args = append(args, strings.ToLower(f.Tag))
	}
	if f.ProjectID != "" {
		q += ` AND project_id = ?`
		args = append(args, f.ProjectID)
	}
	if err := checkSpaceFilter(f.Space); err != nil {
		return nil, err
	}
	q, args = spaceClause(q, args, "space", f.Space)
	q += ` ORDER BY pinned DESC, updated_ms DESC, id LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	return out, s.withNoteTags(ctx, out)
}

// NotesByIDs returns the caller's notes among ids, in the order of ids.
func (s *Store) NotesByIDs(ctx context.Context, userID string, ids []string) ([]Note, error) {
	var out []Note
	for _, id := range ids {
		n, err := s.GetNote(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			continue // deleted since it was indexed
		}
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// SearchNotes does full-text search. The trigram index handles any term of
// three or more characters in any script (including CJK); shorter terms fall
// back to a substring scan.
func (s *Store) SearchNotes(ctx context.Context, userID, query string, limit int, includeArchived bool, space string) ([]NoteHit, error) {
	if err := checkSpaceFilter(space); err != nil {
		return nil, err
	}
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return nil, invalid("query is empty")
	}
	short := false
	for _, t := range terms {
		if utf8.RuneCountInString(t) < 3 {
			short = true
		}
	}
	var hits []NoteHit
	var err error
	if short {
		hits, err = s.searchLike(ctx, userID, terms, limit, includeArchived, space)
	} else {
		hits, err = s.searchFTS(ctx, userID, terms, limit, includeArchived, space)
	}
	if err != nil {
		return nil, err
	}
	notes := make([]Note, len(hits))
	for i := range hits {
		notes[i] = hits[i].Note
	}
	if err := s.withNoteTags(ctx, notes); err != nil {
		return nil, err
	}
	for i := range hits {
		hits[i].Note = notes[i]
	}
	return hits, nil
}

func (s *Store) searchFTS(ctx context.Context, userID string, terms []string, limit int, includeArchived bool, space string) ([]NoteHit, error) {
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	q := `
		SELECT n.id, n.user_id, n.title, n.content, n.pinned, n.archived, n.project_id, n.space, n.created_ms, n.updated_ms,
		       snippet(notes_fts, 3, '[', ']', '…', 16), bm25(notes_fts)
		FROM notes_fts JOIN notes n ON n.id = notes_fts.note_id
		WHERE notes_fts MATCH ? AND notes_fts.user_id = ?`
	args := []any{strings.Join(quoted, " AND "), userID}
	if !includeArchived {
		q += ` AND n.archived = 0`
	}
	q, args = spaceClause(q, args, "n.space", space)
	q += ` ORDER BY bm25(notes_fts) LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []NoteHit
	for rows.Next() {
		var n Note
		var pinned, archived int
		var created, updated int64
		var snip string
		var rank float64
		if err := rows.Scan(&n.ID, &n.UserID, &n.Title, &n.Content, &pinned, &archived, &n.ProjectID, &n.Space, &created, &updated, &snip, &rank); err != nil {
			return nil, err
		}
		n.Pinned, n.Archived = pinned != 0, archived != 0
		n.Created, n.Updated = fromMS(created), fromMS(updated)
		out = append(out, NoteHit{Note: n, Snippet: snip, Score: -rank})
	}
	return out, rows.Err()
}

func (s *Store) searchLike(ctx context.Context, userID string, terms []string, limit int, includeArchived bool, space string) ([]NoteHit, error) {
	q := `SELECT ` + noteCols + ` FROM notes n WHERE user_id = ?`
	args := []any{userID}
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q, args = spaceClause(q, args, "space", space)
	for _, t := range terms {
		q += ` AND (title LIKE ? ESCAPE '\' OR content LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM note_tags g WHERE g.note_id = n.id AND g.tag LIKE ? ESCAPE '\'))`
		p := "%" + likeEscape(t) + "%"
		args = append(args, p, p, p)
	}
	q += ` ORDER BY updated_ms DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []NoteHit
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, NoteHit{Note: n, Snippet: Snippet(n.Content, terms[0], 60), Score: 1})
	}
	return out, rows.Err()
}

// Snippet returns about radius runes of text on each side of the first
// case-insensitive occurrence of term, or the start of text when absent.
func Snippet(text, term string, radius int) string {
	rs := []rune(text)
	lower := []rune(strings.ToLower(text))
	needle := []rune(strings.ToLower(term))
	at := -1
	if len(lower) == len(rs) { // ToLower can change rune counts for exotic scripts
	search:
		for i := 0; i+len(needle) <= len(lower); i++ {
			for j := range needle {
				if lower[i+j] != needle[j] {
					continue search
				}
			}
			at = i
			break
		}
	}
	start, end := 0, min(len(rs), 2*radius)
	if at >= 0 {
		start = max(0, at-radius)
		end = min(len(rs), at+len(needle)+radius)
	}
	s := strings.TrimSpace(string(rs[start:end]))
	if start > 0 {
		s = "…" + s
	}
	if end < len(rs) {
		s += "…"
	}
	return s
}

// NotesNeedingIndex lists notes whose AI index is missing or older than the
// note itself, oldest edit first.
func (s *Store) NotesNeedingIndex(ctx context.Context, limit int) ([]Note, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+noteCols+` FROM notes
		WHERE ai_indexed_ms IS NULL OR ai_indexed_ms < updated_ms
		ORDER BY updated_ms LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	return out, s.withNoteTags(ctx, out)
}

// MarkNoteIndexed records that the AI index covers the note as of version
// (its Updated time when it was read), so a later edit re-queues it.
func (s *Store) MarkNoteIndexed(ctx context.Context, id string, version time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notes SET ai_indexed_ms = ? WHERE id = ?`, ms(version), id)
	return err
}

// ResetNoteIndex forces every note to be re-indexed, e.g. after the embedding
// model changed.
func (s *Store) ResetNoteIndex(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notes SET ai_indexed_ms = NULL`)
	return err
}
