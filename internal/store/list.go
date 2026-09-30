package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/NurramoX/thoughts/internal/api"
)

// List returns the metadata of the thoughts q selects, in q's order. Total
// counts every match, before Limit and Offset. With ranked text terms the
// query joins thought_fts, and each thought carries a snippet.
func (s *sqliteStore) List(ctx context.Context, q Query) (api.List, error) {
	c, err := compile(q.Filter)
	if err != nil {
		return api.List{}, err
	}
	ranked := c.match != ""
	order, err := orderBy(q.Sort, q.Desc, ranked)
	if err != nil {
		return api.List{}, err
	}

	from := `thought i`
	where := c.where
	args := c.args
	if ranked {
		from = `thought_fts JOIN thought i ON i.id = thought_fts.rowid`
		where = `thought_fts MATCH ? AND (` + where + `)`
		args = append([]any{c.match}, args...)
	}
	columns := metaColumns
	if ranked {
		columns += `, snippet(thought_fts, -1, '', '', '…', 16)`
	}
	limit, offset := q.Limit, q.Offset
	if limit <= 0 {
		limit = -1 // SQLite: no limit
	}

	list := api.List{Thoughts: []api.Meta{}}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+from+` WHERE `+where, args...).Scan(&list.Total); err != nil {
			return err
		}
		query := `SELECT ` + columns + ` FROM ` + from + ` WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
		return each(ctx, tx, query, append(args, limit, offset), func(r *sql.Rows) error {
			var extra []any
			var snippet string
			if ranked {
				extra = append(extra, &snippet)
			}
			m, err := scanMeta(r, extra...)
			if err != nil {
				return err
			}
			m.Snippet = snippet
			list.Thoughts = append(list.Thoughts, m)
			return nil
		})
	})
	return list, err
}

// orderBy renders the ORDER BY clause of a sort, ending in a tiebreak on id
// in the same direction.
func orderBy(sort string, desc, ranked bool) (string, error) {
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	tie := ", i.id " + dir
	switch sort {
	case SortUpdated:
		return "i.updated_at " + dir + tie, nil
	case SortCreated:
		return "i.created_at " + dir + tie, nil
	case SortTitle:
		return "i.title COLLATE " + foldCollation + " " + dir + tie, nil
	case SortRank:
		if !ranked {
			return "", fmt.Errorf("sort by rank needs a ranked text term")
		}
		// rank is more negative for a better match; desc is best first.
		if desc {
			return "thought_fts.rank ASC" + tie, nil
		}
		return "thought_fts.rank DESC" + tie, nil
	}
	return "", fmt.Errorf("unknown sort %q", sort)
}
