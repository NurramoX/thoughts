package store

import (
	"context"
	"database/sql"
	"slices"

	"github.com/NurramoX/thoughts/internal/api"
)

// Input is validated before the thought is looked up, so a 422 or 413 does not
// depend on the data. Then come the 404, then the Precondition (412), then the
// rules that depend on the thought's state: the tag and attribute limits.

func (s *sqliteStore) Create(ctx context.Context, req api.CreateRequest) (api.Thought, error) {
	t, err := title(req.Title)
	if err != nil {
		return api.Thought{}, err
	}
	if err := body(req.Body); err != nil {
		return api.Thought{}, err
	}
	tags, err := tagSet(req.Tags)
	if err != nil {
		return api.Thought{}, err
	}
	attrs, err := attributeSet(req.Attributes)
	if err != nil {
		return api.Thought{}, err
	}
	if _, ok := attrs[api.StatusKey]; !ok {
		attrs[api.StatusKey] = api.StatusRaw
	}
	if len(attrs) > maxAttributes {
		return api.Thought{}, invalid("%d attributes, at most %d allowed", len(attrs), maxAttributes)
	}

	var thought api.Thought
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		now := s.stamp()
		res, err := tx.ExecContext(ctx, `
			INSERT INTO thought (title, body, version, created_at, updated_at)
			VALUES (?, ?, 1, ?, ?)`, t, req.Body, now, now)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for _, tag := range tags {
			if _, err := addTag(ctx, tx, id, tag); err != nil {
				return err
			}
		}
		for k, v := range attrs {
			if _, err := setAttribute(ctx, tx, id, k, v); err != nil {
				return err
			}
		}
		thought, err = load(ctx, tx, id)
		return err
	})
	return thought, err
}

// attributeSet validates a map of attributes. Two keys that lowercase to the
// same key are an error.
func attributeSet(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		k, v, err := attribute(k, v)
		if err != nil {
			return nil, err
		}
		if _, dup := out[k]; dup {
			return nil, invalid("attribute key %q given twice", k)
		}
		out[k] = v
	}
	return out, nil
}

func (s *sqliteStore) Patch(ctx context.Context, id int64, pre api.Precondition, p api.Patch) (api.Thought, error) {
	var newTitle *string
	if p.Title != nil {
		t, err := title(*p.Title)
		if err != nil {
			return api.Thought{}, err
		}
		newTitle = &t
	}
	if p.Body != nil {
		if err := body(*p.Body); err != nil {
			return api.Thought{}, err
		}
	}
	var tags []string
	if p.Tags != nil {
		var err error
		if tags, err = tagSet(*p.Tags); err != nil {
			return api.Thought{}, err
		}
	}
	set, remove, err := attributePatch(p.Attributes)
	if err != nil {
		return api.Thought{}, err
	}

	var thought api.Thought
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		_, err := s.write(ctx, tx, id, pre, func(tx *sql.Tx, cur current) (edit, error) {
			var e edit
			if newTitle != nil && *newTitle != cur.title {
				e.title = newTitle
			}
			if p.Body != nil {
				same, err := sameBody(ctx, tx, id, *p.Body)
				if err != nil {
					return e, err
				}
				if !same {
					e.body = p.Body
				}
			}
			if p.Tags != nil {
				changed, err := replaceTags(ctx, tx, id, tags)
				if err != nil {
					return e, err
				}
				e.other = e.other || changed
			}
			for k, v := range set {
				changed, err := setAttribute(ctx, tx, id, k, v)
				if err != nil {
					return e, err
				}
				e.other = e.other || changed
			}
			for _, k := range remove {
				changed, err := removeAttribute(ctx, tx, id, k)
				if err != nil {
					return e, err
				}
				e.other = e.other || changed
			}
			return e, checkLimits(ctx, tx, id)
		})
		if err != nil {
			return err
		}
		thought, err = load(ctx, tx, id)
		return err
	})
	return thought, err
}

// attributePatch validates the attributes of a merge patch: the values to set
// and the keys to remove. Status cannot be removed, and two keys that
// lowercase to the same key are an error.
func attributePatch(in map[string]*string) (set map[string]string, remove []string, err error) {
	set = make(map[string]string)
	seen := make(map[string]bool)
	for k, v := range in {
		k, err := key(k)
		if err != nil {
			return nil, nil, err
		}
		if seen[k] {
			return nil, nil, invalid("attribute key %q given twice", k)
		}
		seen[k] = true
		if v == nil {
			if k == api.StatusKey {
				return nil, nil, invalid("status cannot be removed")
			}
			remove = append(remove, k)
			continue
		}
		if set[k], err = value(k, *v); err != nil {
			return nil, nil, err
		}
	}
	return set, remove, nil
}

func (s *sqliteStore) PutBody(ctx context.Context, id int64, pre api.Precondition, b []byte) (int64, error) {
	text := string(b) // bound as TEXT, never BLOB
	if err := body(text); err != nil {
		return 0, err
	}
	return s.writeTx(ctx, id, pre, func(tx *sql.Tx, _ current) (edit, error) {
		same, err := sameBody(ctx, tx, id, text)
		if same || err != nil {
			return edit{}, err
		}
		return edit{body: &text}, nil
	})
}

func (s *sqliteStore) PutTag(ctx context.Context, id int64, pre api.Precondition, t string) (int64, error) {
	t, err := tag(t)
	if err != nil {
		return 0, err
	}
	return s.writeTx(ctx, id, pre, func(tx *sql.Tx, _ current) (edit, error) {
		changed, err := addTag(ctx, tx, id, t)
		if err != nil {
			return edit{}, err
		}
		return edit{other: changed}, checkLimits(ctx, tx, id)
	})
}

func (s *sqliteStore) DeleteTag(ctx context.Context, id int64, pre api.Precondition, t string) (int64, error) {
	t, err := tag(t)
	if err != nil {
		return 0, err
	}
	return s.writeTx(ctx, id, pre, func(tx *sql.Tx, _ current) (edit, error) {
		changed, err := affected(tx.ExecContext(ctx, `DELETE FROM thought_tag WHERE thought_id = ? AND tag = ?`, id, t))
		return edit{other: changed}, err
	})
}

func (s *sqliteStore) PutAttribute(ctx context.Context, id int64, pre api.Precondition, k, v string) (int64, error) {
	k, v, err := attribute(k, v)
	if err != nil {
		return 0, err
	}
	return s.writeTx(ctx, id, pre, func(tx *sql.Tx, _ current) (edit, error) {
		changed, err := setAttribute(ctx, tx, id, k, v)
		if err != nil {
			return edit{}, err
		}
		return edit{other: changed}, checkLimits(ctx, tx, id)
	})
}

func (s *sqliteStore) DeleteAttribute(ctx context.Context, id int64, pre api.Precondition, k string) (int64, error) {
	k, err := key(k)
	if err != nil {
		return 0, err
	}
	if k == api.StatusKey {
		return 0, invalid("status cannot be removed")
	}
	return s.writeTx(ctx, id, pre, func(tx *sql.Tx, _ current) (edit, error) {
		changed, err := removeAttribute(ctx, tx, id, k)
		return edit{other: changed}, err
	})
}

// Delete removes the thought for good; its tags and attributes go with it.
func (s *sqliteStore) Delete(ctx context.Context, id int64, pre api.Precondition) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		cur, err := loadCurrent(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := check(pre, cur.version); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM thought WHERE id = ?`, id)
		return err
	})
}

// writeTx is write in a transaction of its own.
func (s *sqliteStore) writeTx(ctx context.Context, id int64, pre api.Precondition, mutate func(*sql.Tx, current) (edit, error)) (int64, error) {
	var version int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		version, err = s.write(ctx, tx, id, pre, mutate)
		return err
	})
	return version, err
}

func sameBody(ctx context.Context, tx *sql.Tx, id int64, b string) (bool, error) {
	var same bool
	err := tx.QueryRowContext(ctx, `SELECT body = ? FROM thought WHERE id = ?`, b, id).Scan(&same)
	return same, err
}

func addTag(ctx context.Context, tx *sql.Tx, id int64, t string) (bool, error) {
	return affected(tx.ExecContext(ctx, `
		INSERT INTO thought_tag (thought_id, tag) VALUES (?, ?)
		ON CONFLICT DO NOTHING`, id, t))
}

// replaceTags makes tags (sorted, deduplicated) the thought's tag set.
func replaceTags(ctx context.Context, tx *sql.Tx, id int64, tags []string) (bool, error) {
	old, err := tagsOf(ctx, tx, id)
	if err != nil || slices.Equal(old, tags) {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thought_tag WHERE thought_id = ?`, id); err != nil {
		return false, err
	}
	for _, t := range tags {
		if _, err := addTag(ctx, tx, id, t); err != nil {
			return false, err
		}
	}
	return true, nil
}

func tagsOf(ctx context.Context, tx *sql.Tx, id int64) ([]string, error) {
	var tags []string
	err := each(ctx, tx, `SELECT tag FROM thought_tag WHERE thought_id = ? ORDER BY tag`, []any{id}, func(r *sql.Rows) error {
		var t string
		err := r.Scan(&t)
		tags = append(tags, t)
		return err
	})
	return tags, err
}

// setAttribute upserts one validated attribute; storing the same value again
// is no change.
func setAttribute(ctx context.Context, tx *sql.Tx, id int64, k, v string) (bool, error) {
	return affected(tx.ExecContext(ctx, `
		INSERT INTO thought_attribute (thought_id, key, value, value_folded) VALUES (?, ?, ?, ?)
		ON CONFLICT (thought_id, key) DO UPDATE
			SET value = excluded.value, value_folded = excluded.value_folded
			WHERE value IS NOT excluded.value`, id, k, v, fold(v)))
}

func removeAttribute(ctx context.Context, tx *sql.Tx, id int64, k string) (bool, error) {
	return affected(tx.ExecContext(ctx, `DELETE FROM thought_attribute WHERE thought_id = ? AND key = ?`, id, k))
}

// checkLimits enforces the per-thought tag and attribute limits after a write
// that may have added some.
func checkLimits(ctx context.Context, tx *sql.Tx, id int64) error {
	var tags, attrs int
	err := tx.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM thought_tag WHERE thought_id = ?1),
		       (SELECT count(*) FROM thought_attribute WHERE thought_id = ?1)`, id).Scan(&tags, &attrs)
	switch {
	case err != nil:
		return err
	case tags > maxTags:
		return invalid("%d tags, at most %d allowed", tags, maxTags)
	case attrs > maxAttributes:
		return invalid("%d attributes, at most %d allowed", attrs, maxAttributes)
	}
	return nil
}

// affected reports whether a statement changed any row.
func affected(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
