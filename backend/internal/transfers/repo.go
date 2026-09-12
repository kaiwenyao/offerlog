// Package transfers implements CSV preview import and exports with the shared
// view/filter inputs. Import is preview-first: the user maps columns, reviews
// type errors and duplicate candidates, then commits idempotently (§13).
package transfers

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/applications/domain"
	apprepo "offerlog/backend/internal/applications/repository"
	"offerlog/backend/internal/platform/database"
)

// Repo owns the CSV paths. apps is the applications repository: an import is a
// 建档 like any other, so it writes the same starting timeline instead of its own.
type Repo struct {
	db   *database.DB
	apps *apprepo.Repo
}

func New(db *database.DB) *Repo { return &Repo{db: db, apps: apprepo.New(db)} }

// FieldMap maps CSV column index → target field key.
type FieldMap map[string]string

// ImportPreview is the dry-run result shown before commit.
type ImportPreview struct {
	BatchID             int64      `json:"batch_id"`
	TotalRows           int        `json:"total_rows"`
	ValidRows           int        `json:"valid_rows"`
	Errors              []RowError `json:"errors"`
	DuplicateCandidates []int64    `json:"duplicate_candidates"`
	Columns             []string   `json:"columns"`
}

type RowError struct {
	Row     int    `json:"row"`
	Column  string `json:"column,omitempty"`
	Message string `json:"message"`
}

// ColumnTarget describes accepted CSV headers.
var csvFieldByHeader = map[string]string{
	"公司": "company_name", "公司名称": "company_name", "company": "company_name", "company_name": "company_name",
	"岗位": "position", "职位": "position", "岗位名称": "position", "position": "position", "title": "position",
	"链接": "job_url", "岗位链接": "job_url", "job_url": "job_url", "url": "job_url",
	"地点": "location", "location": "location",
	"远程": "remote_policy", "remote_policy": "remote_policy",
	"类型": "employment_type", "employment_type": "employment_type",
	"渠道": "channel", "channel": "channel",
	"状态": "status", "status": "status",
	"优先级": "priority", "priority": "priority",
	"标签": "tags", "tags": "tags",
	"备注": "notes", "notes": "notes", "note": "notes",
	"截止日期": "deadline", "deadline": "deadline",
	"投递时间": "submitted_at", "投递日期": "submitted_at", "submitted_at": "submitted_at",
	"薪资下限": "salary_min", "salary_min": "salary_min",
	"薪资上限": "salary_max", "salary_max": "salary_max",
	"币种": "salary_currency", "salary_currency": "salary_currency",
}

var statusAliases = map[string]string{
	"saved": domain.StatusSaved, "待投递": domain.StatusSaved, "收藏": domain.StatusSaved,
	"preparing": domain.StatusPreparing, "准备材料": domain.StatusPreparing, "准备中": domain.StatusPreparing,
	"applied": domain.StatusApplied, "已投递": domain.StatusApplied, "投递": domain.StatusApplied,
	"screening": domain.StatusScreening, "初筛": domain.StatusScreening, "沟通": domain.StatusScreening,
	"assessment": domain.StatusAssessment, "笔试": domain.StatusAssessment, "作业": domain.StatusAssessment,
	"interviewing": domain.StatusInterviewing, "面试": domain.StatusInterviewing, "面试中": domain.StatusInterviewing,
	"offer": domain.StatusOffer, "收到offer": domain.StatusOffer,
	"accepted": domain.StatusAccepted, "已接受": domain.StatusAccepted, "接受": domain.StatusAccepted,
	"rejected": domain.StatusRejected, "被拒绝": domain.StatusRejected, "拒绝": domain.StatusRejected,
	"withdrawn": domain.StatusWithdrawn, "已撤回": domain.StatusWithdrawn, "撤回": domain.StatusWithdrawn,
	"closed": domain.StatusClosed, "岗位关闭": domain.StatusClosed, "关闭": domain.StatusClosed,
}

func parseStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if v, ok := statusAliases[s]; ok {
		return v
	}
	return ""
}

func parseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02", "2006/01/02", time.RFC3339, "02/01/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

// ParseCSV parses rows and builds a preview without touching the DB.
func (r *Repo) ParseCSV(ctx context.Context, ownerID int64, filename string, data io.Reader) (*ImportPreview, error) {
	rd := csv.NewReader(data)
	rd.FieldsPerRecord = -1
	header, err := rd.Read()
	if err != nil {
		return nil, errors.New("CSV 无法解析表头")
	}
	cols := make([]string, len(header))
	colMap := map[int]string{} // index → target key
	for i, h := range header {
		h = strings.TrimSpace(h)
		cols[i] = h
		if target, ok := csvFieldByHeader[strings.ToLower(h)]; ok {
			colMap[i] = target
		}
	}
	preview := &ImportPreview{Columns: cols}
	rowsAll := 0
	recs := [][]string{}
	for {
		rec, err := rd.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			preview.Errors = append(preview.Errors, RowError{Row: rowsAll + 2, Message: "行解析失败"})
			continue
		}
		rowsAll++
		recs = append(recs, rec)
	}
	preview.TotalRows = rowsAll

	valid := 0
	var dupIDs []int64
	seen := map[string]bool{}
	for i, rec := range recs {
		rowNo := i + 2 // header is row 1
		row := map[string]string{}
		for ci, target := range colMap {
			if ci < len(rec) {
				row[target] = rec[ci]
			}
		}
		company := strings.TrimSpace(row["company_name"])
		position := strings.TrimSpace(row["position"])
		if company == "" {
			preview.Errors = append(preview.Errors, RowError{Row: rowNo, Column: "公司", Message: "公司不能为空"})
			continue
		}
		if position == "" {
			preview.Errors = append(preview.Errors, RowError{Row: rowNo, Column: "岗位", Message: "岗位不能为空"})
			continue
		}
		if st := row["status"]; st != "" && parseStatus(st) == "" {
			preview.Errors = append(preview.Errors, RowError{Row: rowNo, Column: "状态", Message: "未知状态：" + st})
			continue
		}
		if dt := row["deadline"]; dt != "" && parseDate(dt) == nil {
			preview.Errors = append(preview.Errors, RowError{Row: rowNo, Column: "截止日期", Message: "日期格式错误"})
			continue
		}
		if st := row["submitted_at"]; st != "" && parseDate(st) == nil {
			preview.Errors = append(preview.Errors, RowError{Row: rowNo, Column: "投递时间", Message: "日期格式错误"})
			continue
		}
		valid++
		// duplicate candidate: same (company, position, url) pair already seen
		key := company + "|" + position + "|" + row["job_url"]
		if seen[key] {
			dupIDs = append(dupIDs, int64(i))
		}
		seen[key] = true
	}
	preview.ValidRows = valid
	preview.DuplicateCandidates = dupIDs

	// persist a batch row in preview state
	var batchID int64
	previewJSON, _ := json.Marshal(map[string]any{
		"columns":    cols,
		"total_rows": preview.TotalRows,
		"valid_rows": preview.ValidRows,
	})
	err = r.db.Pool().QueryRow(ctx, `INSERT INTO import_batches(owner_id, filename, status, preview) VALUES($1,$2,'preview',$3) RETURNING id`,
		ownerID, filename, previewJSON).Scan(&batchID)
	if err != nil {
		return nil, err
	}
	preview.BatchID = batchID
	return preview, nil
}

// CommitImport inserts the valid rows of a previously previewed batch. It is
// idempotent: a committed batch returns the same count without re-inserting.
func (r *Repo) CommitImport(ctx context.Context, ownerID, batchID int64, data io.Reader) (int64, error) {
	var status string
	if err := r.db.Pool().QueryRow(ctx, `SELECT status FROM import_batches WHERE id=$1 AND owner_id=$2`, batchID, ownerID).Scan(&status); err != nil {
		return 0, errors.New("批次不存在")
	}
	if status == "committed" {
		var prev json.RawMessage
		_ = r.db.Pool().QueryRow(ctx, `SELECT preview FROM import_batches WHERE id=$1`, batchID).Scan(&prev)
		var p struct {
			ValidRows int `json:"valid_rows"`
		}
		_ = json.Unmarshal(prev, &p)
		return int64(p.ValidRows), nil
	}
	rd := csv.NewReader(data)
	rd.FieldsPerRecord = -1
	header, err := rd.Read()
	if err != nil {
		return 0, errors.New("CSV 无法解析表头")
	}
	colMap := map[int]string{}
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(h))
		if target, ok := csvFieldByHeader[h]; ok {
			colMap[i] = target
		}
	}
	inserted := int64(0)
	for {
		rec, err := rd.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		row := map[string]string{}
		for ci, target := range colMap {
			if ci < len(rec) {
				row[target] = strings.TrimSpace(rec[ci])
			}
		}
		company := row["company_name"]
		position := row["position"]
		if company == "" || position == "" {
			continue
		}
		st := domain.StatusSaved
		if s := parseStatus(row["status"]); s != "" {
			st = s
		}
		// build the insert via apps repo (respecting owner + company)
		if err := r.insertApp(ctx, ownerID, row, st); err != nil {
			continue
		}
		inserted++
	}
	_, err = r.db.Pool().Exec(ctx, `UPDATE import_batches SET status='committed', committed_at=now() WHERE id=$1 AND owner_id=$2`, batchID, ownerID)
	return inserted, err
}

func (r *Repo) insertApp(ctx context.Context, ownerID int64, row map[string]string, status string) error {
	tx, err := r.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var companyID int64
	err = tx.QueryRow(ctx, `SELECT id FROM companies WHERE owner_id=$1 AND name=$2`, ownerID, row["company_name"]).Scan(&companyID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO companies(owner_id, name) VALUES($1,$2) RETURNING id`, ownerID, row["company_name"]).Scan(&companyID)
		if err != nil {
			return err
		}
	}
	var minP, maxP *int64
	if v := row["salary_min"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			minP = &n
		}
	}
	if v := row["salary_max"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			maxP = &n
		}
	}
	var sub *time.Time
	if s := row["submitted_at"]; s != "" {
		sub = parseDate(s)
	}
	// 「待投递 + 有投递时间」是自相矛盾的一行：以用户选的阶段为准，否则列里会留下
	// 一个时间线永远解释不了的投递时间，下一次回放就把它清掉（同 normalizeCreate）。
	sub = domain.SubmissionTimeFor(status, sub)
	var deadline *time.Time
	if d := row["deadline"]; d != "" {
		deadline = parseDate(d)
	}
	tags := []string{}
	if t := row["tags"]; t != "" {
		for _, p := range strings.Split(t, "|") {
			if p = strings.TrimSpace(p); p != "" {
				tags = append(tags, p)
			}
		}
	}
	var appID int64
	now := time.Now()
	err = tx.QueryRow(ctx, `INSERT INTO applications(owner_id, company_id, company_name, position, job_url,
		location, remote_policy, employment_type, channel, status, priority, tags, notes, deadline, submitted_at,
		salary_min, salary_max, salary_currency, saved_at, custom_values)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,'{}') RETURNING id`,
		ownerID, companyID, row["company_name"], row["position"], row["job_url"], row["location"],
		row["remote_policy"], row["employment_type"], row["channel"], status,
		defStr(row["priority"], "medium"), tags, row["notes"], deadline, sub,
		minP, maxP, row["salary_currency"], now).Scan(&appID)
	if err != nil {
		return err
	}
	// 导入必须留下和手工建档同一套落点（repository.InitialEvents）：CSV 里的投递
	// 时间只写进列、时间线上没有对应的「已投递」，用户一编辑时间线，回放就把真实
	// 投递时间换成导入时刻（后段阶段的记录则直接清空）。
	if err := r.apps.InsertInitialEvents(ctx, tx,
		apprepo.InitialEvents(appID, ownerID, status, now, sub, "导入创建")); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func defStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Errors returns the stored preview report of a batch (per-row errors).
func (r *Repo) Errors(ctx context.Context, ownerID, batchID int64, out *json.RawMessage) error {
	return r.db.Pool().QueryRow(ctx, `SELECT preview FROM import_batches WHERE id=$1 AND owner_id=$2`,
		batchID, ownerID).Scan(out)
}

// ExportRows streams every non-deleted application as CSV-safe string rows.
func (r *Repo) ExportRows(ctx context.Context, ownerID int64) ([][]string, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT company_name, position, job_url, location, remote_policy,
		employment_type, channel, status, priority, array_to_string(tags,'|'), deadline, submitted_at,
		salary_min, salary_max, salary_currency, notes
		FROM applications WHERE owner_id=$1 AND deleted_at IS NULL ORDER BY id`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var company, position, jobURL, location, remote, emp, channel, status, priority, tags string
		var deadline, submitted *time.Time
		var minP, maxP *int64
		var currency, notes string
		if err := rows.Scan(&company, &position, &jobURL, &location, &remote, &emp, &channel,
			&status, &priority, &tags, &deadline, &submitted, &minP, &maxP, &currency, &notes); err != nil {
			return nil, err
		}
		d := ""
		s := ""
		if deadline != nil {
			d = deadline.Format("2006-01-02")
		}
		if submitted != nil {
			s = submitted.Format("2006-01-02 15:04")
		}
		mn, mx := "", ""
		if minP != nil {
			mn = strconv.FormatInt(*minP, 10)
		}
		if maxP != nil {
			mx = strconv.FormatInt(*maxP, 10)
		}
		out = append(out, []string{company, position, jobURL, location, remote, emp, channel, status, priority, tags, d, s, mn, mx, currency, notes})
	}
	return out, rows.Err()
}
