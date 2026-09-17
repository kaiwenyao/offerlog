// Package views implements custom property definitions and the saved-view
// model: filter trees, sorting, grouping and column config. The filter DSL is
// validated against known fields before it is compiled to parameterized SQL;
// the frontend never supplies raw SQL.
package views

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Field types
const (
	TypeText        = "text"
	TypeNumber      = "number"
	TypeSelect      = "select"
	TypeMultiSelect = "multi_select"
	TypeDate        = "date"
	TypeCheckbox    = "checkbox"
	TypeURL         = "url"
	TypeImage       = "image"
)

// Known core fields that filters/sorts can reference. Each entry names the
// SQL expression used for filtering/sorting and its JSON type.
type FieldInfo struct {
	Type     string
	SQL      string // parameterized expression placeholder e.g. "position"
	Custom   bool
	DataType string // when custom, the property's data_type
}

// CoreFields enumerates filterable/sortable built-in fields.
var CoreFields = map[string]FieldInfo{
	"id":              {Type: TypeNumber, SQL: "a.id"},
	"company_name":    {Type: TypeText, SQL: "a.company_name"},
	"position":        {Type: TypeText, SQL: "a.position"},
	"job_url":         {Type: TypeURL, SQL: "a.job_url"},
	"location":        {Type: TypeText, SQL: "a.location"},
	"remote_policy":   {Type: TypeText, SQL: "a.remote_policy"},
	"employment_type": {Type: TypeText, SQL: "a.employment_type"},
	"salary_min":      {Type: TypeNumber, SQL: "a.salary_min"},
	"salary_max":      {Type: TypeNumber, SQL: "a.salary_max"},
	"salary_currency": {Type: TypeText, SQL: "a.salary_currency"},
	"channel":         {Type: TypeText, SQL: "a.channel"},
	"status":          {Type: TypeSelect, SQL: "a.status"},
	// substatus is the concrete progress inside a stage (方案 §3.2). It is
	// filterable so a board can ask for 「面试 · 已完成 · 等反馈」 directly.
	"substatus":          {Type: TypeSelect, SQL: "a.substatus"},
	"priority":           {Type: TypeSelect, SQL: "a.priority"},
	"tags":               {Type: TypeMultiSelect, SQL: "a.tags"},
	"notes":              {Type: TypeText, SQL: "a.notes"},
	"saved_at":           {Type: TypeDate, SQL: "a.saved_at"},
	"submitted_at":       {Type: TypeDate, SQL: "a.submitted_at"},
	"first_response_at":  {Type: TypeDate, SQL: "a.first_response_at"},
	"deadline":           {Type: TypeDate, SQL: "a.deadline"},
	"created_at":         {Type: TypeDate, SQL: "a.created_at"},
	"updated_at":         {Type: TypeDate, SQL: "a.updated_at"},
	"next_action":        {Type: TypeText, SQL: "a.next_action"},
	"next_action_due_at": {Type: TypeDate, SQL: "a.next_action_due_at"},
	// archived 是可见性旗标而不是状态（方案 §2.2）：归档过的岗位必须能被单独筛出来，
	// 而不是用「已结束状态」冒充——归档一个进行中的岗位后，它就该出现在「已归档」里，
	// 并从未归档视图消失。表达式是 boolean，与 TypeCheckbox 的 `= $n` 编译方式一致。
	"archived": {Type: TypeCheckbox, SQL: "(a.archived_at IS NOT NULL)"},
	// 「统一待办」口径（方案 §5.3）——与 home/repo.go 的统一清单**同一定义**：
	// 未完成的 actions 行是唯一真相；只有该岗位一条 action 都没有时，历史的
	// next_action 才作为派生待办参与（同一个 NOT EXISTS 守卫，否则已完成的待办
	// 会以「无法操作的旧记录」永远复活）。
	// 侧栏「待跟进」靠它判断「有没有下一步安排」，而不是读岗位行上的镜像：
	// 完成最后一个待办时镜像会被清空、reopen 不会复活，镜像还会因为 best-effort
	// 同步失败而过期——只看镜像会把「已排了未来一步」的岗位算成待跟进。
	"open_todo_count": {Type: TypeNumber, SQL: "(SELECT count(*) FROM actions ac WHERE ac.application_id = a.id AND ac.owner_id = a.owner_id AND ac.done_at IS NULL) + (CASE WHEN trim(a.next_action) <> '' AND NOT EXISTS (SELECT 1 FROM actions acx WHERE acx.application_id = a.id AND acx.owner_id = a.owner_id) THEN 1 ELSE 0 END)"},
	// 最早到期的那条未完成待办，折算成**用户时区**的日历日：date-only due_date
	// 本来就是一个日历日；due_ts 是瞬时，按用户时区折日（与 reminders / home 的
	// `due_date::timestamp AT TIME ZONE tz` 同一套口径，只是倒过来取日期）。
	// 没有任何未完成待办（或没有任何到期日）时为 NULL。
	"next_open_todo_due": {Type: TypeDate, SQL: "COALESCE((SELECT min(CASE WHEN ac.due_ts IS NOT NULL THEN (ac.due_ts AT TIME ZONE " + userDayTZ + ")::date ELSE ac.due_date END) FROM actions ac WHERE ac.application_id = a.id AND ac.owner_id = a.owner_id AND ac.done_at IS NULL), CASE WHEN trim(a.next_action) <> '' AND NOT EXISTS (SELECT 1 FROM actions acx WHERE acx.application_id = a.id AND acx.owner_id = a.owner_id) THEN a.next_action_due_at ELSE NULL END)"},
}

// userDayTZ is the owner's IANA timezone as a SQL expression, with the same
// fallbacks as timeutil.SafeLocation (” / 'Local' → Europe/Dublin) so a zone
// Go accepts cannot make `AT TIME ZONE` blow up at query time.
const userDayTZ = "COALESCE(NULLIF(NULLIF(trim((SELECT u.timezone FROM users u WHERE u.id = a.owner_id)), ''), 'Local'), 'Europe/Dublin')"

// FilterNode is one node of the filter tree: either a group with children or a
// condition {field, op, value}.
type FilterNode struct {
	Op         string       `json:"op,omitempty"` // and | or (group); eq|neq|contains|gt|lt|gte|lte|in|is_empty|is_not_empty (leaf)
	Conditions []FilterNode `json:"conditions,omitempty"`
	Field      string       `json:"field,omitempty"`
	Value      any          `json:"value,omitempty"`
}

// MaxDepth / MaxConditions bound filter complexity (plan §3.3).
const (
	MaxDepth      = 3
	MaxConditions = 30
)

type Compiled struct {
	Where string
	Args  []any
	Count int
}

// Resolver knows custom fields (property_definitions) for a user and merges
// them with CoreFields for validation/compilation.
type Resolver struct {
	custom map[string]FieldInfo
}

func NewResolver() *Resolver { return &Resolver{custom: map[string]FieldInfo{}} }

func (r *Resolver) SetCustom(key, dataType, sql string) {
	r.custom[key] = FieldInfo{Type: dataType, SQL: sql, Custom: true, DataType: dataType}
}

func (r *Resolver) Lookup(field string) (FieldInfo, bool) {
	if fi, ok := CoreFields[field]; ok {
		return fi, true
	}
	fi, ok := r.custom[field]
	return fi, ok
}

// Validate walks the tree checking fields, ops and depth. Returns a
// human-readable error.
func (r *Resolver) Validate(nodes []FilterNode) error {
	count := 0
	var walk func(children []FilterNode, depth int) error
	walk = func(children []FilterNode, depth int) error {
		if depth > MaxDepth {
			return fmt.Errorf("筛选嵌套深度不能超过 %d", MaxDepth)
		}
		for _, c := range children {
			if len(c.Conditions) > 0 {
				if c.Op != "and" && c.Op != "or" {
					return fmt.Errorf("组合节点必须使用 and/or")
				}
				if err := walk(c.Conditions, depth+1); err != nil {
					return err
				}
				continue
			}
			count++
			if count > MaxConditions {
				return fmt.Errorf("条件数不能超过 %d", MaxConditions)
			}
			fi, ok := r.Lookup(c.Field)
			if !ok {
				return fmt.Errorf("未知筛选字段 %q", c.Field)
			}
			if !validOp(c.Op, fi.Type) {
				return fmt.Errorf("字段 %s 不支持操作符 %s", c.Field, c.Op)
			}
		}
		return nil
	}
	return walk(nodes, 0)
}

func validOp(op, typ string) bool {
	switch op {
	case "eq", "neq":
		return true
	case "contains", "starts_with":
		return typ == TypeText || typ == TypeURL || typ == TypeMultiSelect
	case "gt", "lt", "gte", "lte":
		return typ == TypeNumber || typ == TypeDate
	case "in":
		return true
	case "is_empty", "is_not_empty":
		return true
	}
	return false
}

// Compile converts a validated tree into WHERE SQL + args. Conditions always
// refer to the applications alias `a`.
// Compile converts a validated tree into WHERE SQL + args. The SQL uses
// placeholders starting at $start so callers can reserve earlier positions
// (e.g. $1 for owner_id). Conditions refer to the applications alias `a`.
func (r *Resolver) Compile(nodes []FilterNode, start int) (*Compiled, error) {
	if err := r.Validate(nodes); err != nil {
		return nil, err
	}
	clauses, args, err := r.build(nodes, nil, start)
	if err != nil {
		return nil, err
	}
	if len(clauses) == 0 {
		return &Compiled{Where: "", Args: nil, Count: 0}, nil
	}
	return &Compiled{Where: "(" + strings.Join(clauses, ") AND (") + ")", Args: args, Count: len(nodes)}, nil
}

func (r *Resolver) build(nodes []FilterNode, args []any, base int) ([]string, []any, error) {
	var clauses []string
	for _, c := range nodes {
		if len(c.Conditions) > 0 {
			sub, newArgs, err := r.build(c.Conditions, args, base)
			if err != nil {
				return nil, nil, err
			}
			args = newArgs
			if len(sub) > 0 {
				join := " AND "
				if c.Op == "or" {
					join = " OR "
				}
				clauses = append(clauses, "("+strings.Join(sub, join)+")")
			}
			continue
		}
		cl, newArgs, err := r.compileLeaf(c, args, base)
		if err != nil {
			return nil, nil, err
		}
		args = newArgs
		if cl != "" {
			clauses = append(clauses, cl)
		}
	}
	return clauses, args, nil
}

func (r *Resolver) compileLeaf(c FilterNode, args []any, base int) (string, []any, error) {
	fi, ok := r.Lookup(c.Field)
	if !ok {
		return "", nil, fmt.Errorf("未知筛选字段 %q", c.Field)
	}
	// args appended later occupy indexes base+len(args)+1 ...
	nextIdx := func() int { return base + len(args) }
	addArg := func(v any) []any { return append(args, v) }
	// empty checks work on the raw SQL column
	switch c.Op {
	case "is_empty":
		return fmt.Sprintf("(%s IS NULL OR %s = '' OR %s = '{}')", fi.SQL, fi.SQL, fi.SQL), args, nil
	case "is_not_empty":
		return fmt.Sprintf("(%s IS NOT NULL AND %s <> '' AND %s <> '{}')", fi.SQL, fi.SQL, fi.SQL), args, nil
	}
	switch fi.Type {
	case TypeText, TypeURL, TypeSelect:
		switch c.Op {
		case "eq":
			idx := nextIdx()
			args = addArg(fmt.Sprint(c.Value))
			return fmt.Sprintf("%s = $%d::text", fi.SQL, idx), args, nil
		case "neq":
			idx := nextIdx()
			args = addArg(fmt.Sprint(c.Value))
			return fmt.Sprintf("%s <> $%d::text", fi.SQL, idx), args, nil
		case "contains":
			idx := nextIdx()
			args = addArg("%" + fmt.Sprint(c.Value) + "%")
			return fmt.Sprintf("%s ILIKE $%d", fi.SQL, idx), args, nil
		case "starts_with":
			idx := nextIdx()
			args = addArg(fmt.Sprint(c.Value) + "%")
			return fmt.Sprintf("%s ILIKE $%d", fi.SQL, idx), args, nil
		case "in":
			arr, _ := c.Value.([]any)
			ph := make([]string, 0, len(arr))
			for _, v := range arr {
				idx := nextIdx()
				args = addArg(v)
				ph = append(ph, fmt.Sprintf("$%d", idx))
			}
			return fmt.Sprintf("%s IN (%s)", fi.SQL, strings.Join(ph, ",")), args, nil
		}
	case TypeNumber:
		num, _ := toFloat(c.Value)
		switch c.Op {
		case "eq":
			idx := nextIdx()
			args = addArg(num)
			return fmt.Sprintf("%s = $%d", fi.SQL, idx), args, nil
		case "in":
			// 供「本周面试」这类由前端算出 id 集合的视图使用。空集合必须匹配
			// 不到任何行（而不是编译成 `IN ()` 让整条 SQL 语法报错）。
			arr, _ := c.Value.([]any)
			if len(arr) == 0 {
				return "FALSE", args, nil
			}
			ph := make([]string, 0, len(arr))
			for _, v := range arr {
				n, err := toFloat(v)
				if err != nil {
					return "", nil, fmt.Errorf("字段 %s 需要数值", c.Field)
				}
				idx := nextIdx()
				args = addArg(n)
				ph = append(ph, fmt.Sprintf("$%d", idx))
			}
			return fmt.Sprintf("%s IN (%s)", fi.SQL, strings.Join(ph, ",")), args, nil
		case "gt":
			idx := nextIdx()
			args = addArg(num)
			return fmt.Sprintf("%s > $%d", fi.SQL, idx), args, nil
		case "lt":
			idx := nextIdx()
			args = addArg(num)
			return fmt.Sprintf("%s < $%d", fi.SQL, idx), args, nil
		case "gte":
			idx := nextIdx()
			args = addArg(num)
			return fmt.Sprintf("%s >= $%d", fi.SQL, idx), args, nil
		case "lte":
			idx := nextIdx()
			args = addArg(num)
			return fmt.Sprintf("%s <= $%d", fi.SQL, idx), args, nil
		case "is_empty":
			return fmt.Sprintf("(%s IS NULL)", fi.SQL), args, nil
		}
	case TypeDate:
		var t any = c.Value
		switch v := c.Value.(type) {
		case string:
			t = v // SQL comparison works with ISO timestamps/dates
		case map[string]any:
			if d, ok := v["iso"]; ok {
				t = d
			}
		}
		switch c.Op {
		case "eq":
			idx := nextIdx()
			args = addArg(t)
			return fmt.Sprintf("%s::date = $%d::date", fi.SQL, idx), args, nil
		case "gt":
			idx := nextIdx()
			args = addArg(t)
			return fmt.Sprintf("%s > $%d", fi.SQL, idx), args, nil
		case "lt":
			idx := nextIdx()
			args = addArg(t)
			return fmt.Sprintf("%s < $%d", fi.SQL, idx), args, nil
		case "gte":
			idx := nextIdx()
			args = addArg(t)
			return fmt.Sprintf("%s >= $%d", fi.SQL, idx), args, nil
		case "lte":
			idx := nextIdx()
			args = addArg(t)
			return fmt.Sprintf("%s <= $%d", fi.SQL, idx), args, nil
		}
	case TypeMultiSelect:
		// tags overlap check
		switch c.Op {
		case "contains":
			idx := nextIdx()
			args = addArg(fmt.Sprint(c.Value))
			return fmt.Sprintf("$%d = ANY(%s)", idx, fi.SQL), args, nil
		case "in":
			arr, _ := c.Value.([]any)
			if len(arr) == 0 {
				return "", args, nil
			}
			ph := make([]string, 0, len(arr))
			for _, v := range arr {
				idx := nextIdx()
				args = addArg(v)
				ph = append(ph, fmt.Sprintf("$%d = ANY(%s)", idx, fi.SQL))
			}
			return "(" + strings.Join(ph, " OR ") + ")", args, nil
		}
	case TypeCheckbox:
		b, _ := c.Value.(bool)
		idx := nextIdx()
		args = addArg(b)
		return fmt.Sprintf("%s = $%d", fi.SQL, idx), args, nil
	}
	return "", args, fmt.Errorf("字段 %s 无法应用操作符 %s", c.Field, c.Op)
}

func toFloat(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%g", &f); err == nil {
			return f, nil
		}
	}
	return 0, fmt.Errorf("期望数值")
}

// SortItem describes one sort clause.
type SortItem struct {
	Field string `json:"field"`
	Dir   string `json:"dir"` // asc | desc
}

func (r *Resolver) ValidateSort(items []SortItem) error {
	for _, s := range items {
		if _, ok := r.Lookup(s.Field); !ok {
			return fmt.Errorf("未知排序字段 %q", s.Field)
		}
		if s.Dir != "asc" && s.Dir != "desc" {
			return fmt.Errorf("排序方向必须为 asc/desc")
		}
	}
	return nil
}

// OrderBy builds an ORDER BY clause from validated sorts. Nulls last.
func (r *Resolver) OrderBy(items []SortItem) (string, error) {
	if err := r.ValidateSort(items); err != nil {
		return "", err
	}
	var parts []string
	for _, s := range items {
		fi := CoreFields[s.Field]
		if fi.Custom {
			// custom JSONB values need an expression; handled by caller
			continue
		}
		nulls := "NULLS LAST"
		parts = append(parts, fmt.Sprintf("%s %s %s", fi.SQL, strings.ToUpper(s.Dir), nulls))
	}
	parts = append(parts, "a.id DESC")
	return strings.Join(parts, ", "), nil
}

var _ = time.Now
