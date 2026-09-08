package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/timeutil"
	"offerlog/backend/internal/views"
	"offerlog/backend/internal/views/repository"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrBadFilter = errors.New("bad filter")
	ErrKeyTaken  = errors.New("key taken")
)

type Service struct {
	db           *database.DB
	repo         *repository.Repo
	stageHistory StageHistoryProvider
}

func New(db *database.DB, repo *repository.Repo) *Service { return &Service{db: db, repo: repo} }

// StageHistoryProvider computes the per-application stage history map for a
// batch of ids. Implemented at the wiring layer (bootstrap) so the views
// module never imports the applications module (which would be a cycle).
// submittedAt carries each application's user-entered 投递时间, which wins for
// the applied stage (backfilled submissions must render their real day).
type StageHistoryProvider interface {
	StageHistoryFor(ctx context.Context, ownerID int64, appIDs []int64, loc *time.Location, submittedAt map[int64]*time.Time) (map[int64]map[string]string, error)
}

// WithStageHistory attaches the cross-module stage-history provider used by
// the rich query endpoint when include=stage_history is requested.
func (s *Service) WithStageHistory(p StageHistoryProvider) *Service {
	s.stageHistory = p
	return s
}

func (s *Service) Repo() *repository.Repo { return s.repo }

// Property helpers ----------------------------------------------------------

func (s *Service) ListProperties(ctx context.Context, ownerID int64) ([]*repository.PropertyDef, error) {
	return s.repo.ListProperties(ctx, ownerID)
}

func (s *Service) CreateProperty(ctx context.Context, ownerID int64, name, key, dataType string, options []any, required bool) (*repository.PropertyDef, error) {
	if key == "" {
		key = slugify(name)
	}
	if key == "" {
		return nil, errors.New("属性键不能为空")
	}
	if _, ok := views.CoreFields[key]; ok {
		return nil, errors.New("属性键与内置字段冲突")
	}
	switch dataType {
	case views.TypeText, views.TypeNumber, views.TypeSelect, views.TypeMultiSelect, views.TypeDate, views.TypeCheckbox, views.TypeURL, views.TypeImage:
	default:
		return nil, fmt.Errorf("不支持的类型 %s", dataType)
	}
	b, _ := json.Marshal(options)
	p := &repository.PropertyDef{OwnerID: ownerID, Name: name, Key: key, DataType: dataType, Options: b, Required: required}
	if err := s.repo.CreateProperty(ctx, p); err != nil {
		// unique(owner_id,key) violation
		return nil, ErrKeyTaken
	}
	return p, nil
}

func (s *Service) UpdateProperty(ctx context.Context, ownerID int64, id int64, name, dataType string, options []any, required bool) (*repository.PropertyDef, error) {
	p, err := s.repo.GetProperty(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}
	// type change requires explicit conversion (plan §3.2): re-parse stored
	// values is out of scope for v1 custom types because every custom value is
	// JSONB; we validate that if dataType changes, values convert. For
	// simplicity v1 keeps the old values and lets the UI surface mismatches.
	p.Name = name
	p.DataType = dataType
	p.Options, _ = json.Marshal(options)
	p.Required = required
	if err := s.repo.UpdateProperty(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) DeleteProperty(ctx context.Context, ownerID, id int64) error {
	p, err := s.repo.GetProperty(ctx, ownerID, id)
	if err != nil || p == nil {
		if p == nil {
			return ErrNotFound
		}
		return err
	}
	// deleting an option/definition shows affected count on the frontend; here
	// we just remove the key from all custom_values.
	if _, err := s.db.Pool().Exec(ctx, `UPDATE applications SET custom_values = custom_values - $2, updated_at=now() WHERE owner_id=$1`,
		ownerID, p.Key); err != nil {
		return err
	}
	return s.repo.DeleteProperty(ctx, ownerID, id)
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// Views ---------------------------------------------------------------------

func (s *Service) ListViews(ctx context.Context, ownerID int64) ([]*repository.SavedView, error) {
	return s.repo.ListViews(ctx, ownerID)
}

func (s *Service) SaveView(ctx context.Context, ownerID int64, v *repository.SavedView) (*repository.SavedView, error) {
	if v.Name == "" {
		return nil, errors.New("视图名称不能为空")
	}
	// validate embedded config against known fields
	var filters []views.FilterNode
	var sorts []views.SortItem
	if len(v.FilterAST) > 0 {
		if err := json.Unmarshal(v.FilterAST, &filters); err != nil {
			return nil, ErrBadFilter
		}
	}
	if len(v.Sort) > 0 {
		if err := json.Unmarshal(v.Sort, &sorts); err != nil {
			return nil, ErrBadFilter
		}
	}
	// build a resolver with this user's custom properties
	res := s.resolverFor(ctx, ownerID)
	if err := res.Validate(filters); err != nil {
		return nil, err
	}
	if err := res.ValidateSort(sorts); err != nil {
		return nil, err
	}
	if v.ID > 0 {
		if err := s.repo.UpdateView(ctx, v); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return v, nil
	}
	if err := s.repo.CreateView(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Service) GetView(ctx context.Context, ownerID, id int64) (*repository.SavedView, error) {
	v, err := s.repo.GetView(ctx, ownerID, id)
	if v == nil {
		return nil, ErrNotFound
	}
	return v, err
}

func (s *Service) DeleteView(ctx context.Context, ownerID, id int64) error {
	if err := s.repo.DeleteView(ctx, ownerID, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// resolverFor loads custom property definitions and exposes them to the
// filter compiler as JSONB expressions.
func (s *Service) resolverFor(ctx context.Context, ownerID int64) *views.Resolver {
	res := views.NewResolver()
	props, err := s.repo.ListProperties(ctx, ownerID)
	if err != nil {
		return res
	}
	for _, p := range props {
		res.SetCustom(p.Key, p.DataType, fmt.Sprintf("a.custom_values->>'%s'", p.Key))
	}
	return res
}

// QueryResult is a page of applications plus counts for the applied filters.
type QueryRow struct {
	*repository.SavedView
	Items   []json.RawMessage `json:"items"`
	Total   int64             `json:"total"`
	Applied int               `json:"applied_conditions"`
}

// ParseFilters decodes wire-format JSON filter/sort arrays into the typed
// structures used by RunQuery.
func ParseFilters(rawF []map[string]any, rawS []map[string]any) ([]views.FilterNode, []views.SortItem, error) {
	var filters []views.FilterNode
	for _, m := range rawF {
		node, err := filterFromMap(m)
		if err != nil {
			return nil, nil, err
		}
		filters = append(filters, node)
	}
	var sorts []views.SortItem
	for _, m := range rawS {
		field, _ := m["field"].(string)
		dir, _ := m["dir"].(string)
		if dir == "" {
			dir, _ = m["direction"].(string)
		}
		sorts = append(sorts, views.SortItem{Field: field, Dir: dir})
	}
	return filters, sorts, nil
}

func filterFromMap(m map[string]any) (views.FilterNode, error) {
	if raw, ok := m["conditions"]; ok {
		op, _ := m["op"].(string)
		var children []views.FilterNode
		if arr, ok := raw.([]any); ok {
			for _, item := range arr {
				sub, ok := item.(map[string]any)
				if !ok {
					return views.FilterNode{}, errors.New("筛选结构错误")
				}
				child, err := filterFromMap(sub)
				if err != nil {
					return views.FilterNode{}, err
				}
				children = append(children, child)
			}
		}
		return views.FilterNode{Op: op, Conditions: children}, nil
	}
	field, _ := m["field"].(string)
	op, _ := m["op"].(string)
	value := m["value"]
	return views.FilterNode{Field: field, Op: op, Value: value}, nil
}

// RunQuery lists applications through the shared filter/sort/group machinery.
// It mirrors POST /applications/query but supports full filter trees.
func (s *Service) RunQuery(ctx context.Context, ownerID int64, filters []views.FilterNode, sorts []views.SortItem, page, pageSize int) ([]map[string]any, int64, int, error) {
	return s.RunQueryOpts(ctx, ownerID, filters, sorts, page, pageSize, QueryOptions{})
}

// QueryOptions carries the optional enrichments of the rich query endpoint.
type QueryOptions struct {
	// IncludeStageHistory adds a "stage_history" map to every row: each
	// reached status → earliest user-zone calendar day (YYYY-MM-DD), so a
	// whole list page renders stage rails (including terminal dead-ends) in
	// one round-trip.
	IncludeStageHistory bool
	// Timezone is the owner's IANA zone used to bucket event instants into
	// calendar days; defaults to the app fallback when empty.
	Timezone string
}

// RunQueryOpts is RunQuery with explicit enrichment options.
func (s *Service) RunQueryOpts(ctx context.Context, ownerID int64, filters []views.FilterNode, sorts []views.SortItem, page, pageSize int, opts QueryOptions) ([]map[string]any, int64, int, error) {
	withStage := opts.IncludeStageHistory
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	res := s.resolverFor(ctx, ownerID)
	// placeholders start at $2 because $1 is reserved for owner_id
	compiled, err := res.Compile(filters, 2)
	if err != nil {
		return nil, 0, 0, err
	}
	where := []string{"a.owner_id = $1", "a.deleted_at IS NULL"}
	args := []any{ownerID}
	if compiled.Where != "" {
		where = append(where, compiled.Where)
	}
	for _, a := range compiled.Args {
		args = append(args, a)
	}
	whereSQL := strings.Join(where, " AND ")
	orderBy, err := res.OrderBy(sorts)
	if err != nil {
		return nil, 0, 0, err
	}

	var total int64
	q := s.db.Pool()
	if err := q.QueryRow(ctx, `SELECT count(*) FROM applications a WHERE `+whereSQL, args...).Scan(&total); err != nil {
		fmt.Printf("DEBUG COUNT SQL where=%s args=%#v err=%v\n", whereSQL, args, err)
		return nil, 0, 0, err
	}
	offset := (page - 1) * pageSize
	args2 := append(append([]any{}, args...), pageSize, offset)
	rows, err := q.Query(ctx, `SELECT a.id, a.company_name, a.position, a.job_url, a.location, a.remote_policy,
		a.employment_type, a.salary_min, a.salary_max, a.salary_currency, a.channel, a.status, a.priority,
		a.tags, a.custom_values, a.saved_at, a.submitted_at, a.first_response_at, a.deadline, a.accepted_at,
		a.rejected_at, a.reason, a.next_action, a.next_action_due_at, a.archived_at, a.deleted_at,
		a.version, a.created_at, a.updated_at, a.notes
		FROM applications a WHERE `+whereSQL+` ORDER BY `+orderBy+` LIMIT $`+fmt.Sprint(len(args2)-1)+` OFFSET $`+fmt.Sprint(len(args2)), args2...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var (
			id           int64
			companyName  string
			position     string
			jobURL       string
			location     string
			remotePolicy string
			empType      string
			salaryMin    *int64
			salaryMax    *int64
			currency     string
			channel      string
			status       string
			priority     string
			tags         []string
			custom       json.RawMessage
			savedAt      *time.Time
			submittedAt  *time.Time
			firstResp    *time.Time
			deadline     *time.Time
			acceptedAt   *time.Time
			rejectedAt   *time.Time
			reason       string
			nextAction   string
			nextDueAt    *time.Time
			archivedAt   *time.Time
			deletedAt    *time.Time
			version      int
			createdAt    time.Time
			updatedAt    time.Time
			notes        string
		)
		if err := rows.Scan(&id, &companyName, &position, &jobURL, &location,
			&remotePolicy, &empType, &salaryMin, &salaryMax, &currency,
			&channel, &status, &priority, &tags, &custom, &savedAt, &submittedAt,
			&firstResp, &deadline, &acceptedAt, &rejectedAt, &reason,
			&nextAction, &nextDueAt, &archivedAt, &deletedAt, &version,
			&createdAt, &updatedAt, &notes); err != nil {
			return nil, 0, 0, err
		}
		// DATE columns are emitted date-only (YYYY-MM-DD) so every surface —
		// applications list/detail and the views query — shares one wire format.
		m := map[string]any{
			"id": id, "company_name": companyName, "position": position, "job_url": jobURL,
			"location": location, "remote_policy": remotePolicy, "employment_type": empType,
			"salary_min": salaryMin, "salary_max": salaryMax, "salary_currency": currency,
			"channel": channel, "status": status, "priority": priority, "tags": tags,
			"custom_values": json.RawMessage(custom), "notes": notes,
			"saved_at": savedAt, "submitted_at": submittedAt, "first_response_at": firstResp,
			"deadline": dayString(deadline), "accepted_at": acceptedAt, "rejected_at": rejectedAt,
			"reason": reason, "next_action": nextAction, "next_action_due_at": dayString(nextDueAt),
			"version": version, "archived": archivedAt != nil, "deleted": deletedAt != nil,
			"created_at": createdAt, "updated_at": updatedAt,
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	if withStage && len(items) > 0 {
		if s.stageHistory != nil {
			ids := make([]int64, 0, len(items))
			submitted := make(map[int64]*time.Time, len(items))
			for _, m := range items {
				if id, ok := m["id"].(int64); ok {
					ids = append(ids, id)
					if sub, ok := m["submitted_at"].(*time.Time); ok && sub != nil {
						submitted[id] = sub
					}
				}
			}
			loc, _ := timeutil.SafeLocation(opts.Timezone)
			// Stage history is an enrichment: never fail the page over it.
			if stage, err := s.stageHistory.StageHistoryFor(ctx, ownerID, ids, loc, submitted); err == nil {
				for _, m := range items {
					if id, ok := m["id"].(int64); ok {
						if sh, ok := stage[id]; ok && len(sh) > 0 {
							m["stage_history"] = sh
						}
					}
				}
			}
		}
	}
	return items, total, compiled.Count, nil
}

// dayString renders a DATE-derived time.Time as a date-only string (nil → nil).
func dayString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := t.UTC().Format("2006-01-02")
	return &v
}
