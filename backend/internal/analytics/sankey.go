package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"offerlog/backend/internal/applications/domain"
)

// Node and Link are ECharts sankey primitives enriched with labels.
type Node struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
}

type Link struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Value  int64  `json:"value"`
}

// Sankey is the API payload: nodes, links, cohort count, as_of, version.
type Sankey struct {
	Nodes             []Node `json:"nodes"`
	Links             []Link `json:"links"`
	CohortCount       int64  `json:"cohort_count"`
	AsOf              string `json:"as_of"`
	DefinitionVersion int    `json:"definition_version"`
	Mode              string `json:"mode"`
	Notes             string `json:"notes"`
	DrilldownToken    string `json:"drilldown_token,omitempty"`
}

// v2: 未投递 moved from "submitted_at IS NULL" to the 待投递 metric
// definition (repo.Counts) so the chart cannot contradict the cards or the
// list rendered on the same page — 内推 / 猎头 rows in screening/assessment/
// interviewing legitimately carry no submitted_at but are past 未投递.
//
// v3: the submitted branch is no longer a flat split by current status.
// Each submitted row walks later process stages it actually reached
// (笔试 / 初筛 / 面试 / Offer), then hangs a terminal off the last stage —
// so 「笔试后被拒」flows through 笔试作业. 已投递 itself is the waiting
// column (applied / 准备材料回退 stay there as residual). Skipped stages
// and still_<status> leaves are not invented.
const DefinitionVersion = 3

// SankeyA builds the "current progress" graph:
//
//	全部机会 → 已投递 / 未投递 → 已投递按实际走过的后段展开 → 终态挂在链尾
//
// People still waiting after submit sit on 已投递 (inflow > outflow). The
// middle layer reuses the 待投递 metric definition verbatim (toApplyFactSQL
// in repo.go): 未投递 = still saved/preparing with neither a submission nor
// a response fact. Records past the preparing phase ride the submitted
// branch even when submitted_at is NULL (内推 / 猎头直接约面). 未投递 is a
// leaf — only the submitted branch expands.
func (r *Repo) SankeyA(ctx context.Context, req *SnapshotRequest) (*Sankey, error) {
	where, args := r.whereClause(req)
	rows, err := r.db.Pool().Query(ctx, `SELECT id, status,
		`+toApplyFactSQL+`
		FROM applications WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type appRow struct {
		id     int64
		status string
	}
	var submitted []appRow
	var cohort, notSub int64
	seen := map[int64]bool{}
	for rows.Next() {
		var id int64
		var st string
		var notSubRow bool
		if err := rows.Scan(&id, &st, &notSubRow); err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		cohort++
		if notSubRow {
			notSub++
			continue
		}
		submitted = append(submitted, appRow{id: id, status: st})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sub := cohort - notSub

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	ids := make([]int64, len(submitted))
	for i, a := range submitted {
		ids[i] = a.id
	}
	pointsByApp, err := r.happenedStagesFor(ctx, ids, now)
	if err != nil {
		return nil, err
	}

	const nsName, sName = "not_submitted", "submitted"
	linkCount := map[[2]string]int64{
		// layer 1: 全部机会 → 未投递 / 已投递. Zero-value edges keep an empty
		// branch visible instead of dropping its node.
		{"all", nsName}: notSub,
		{"all", sName}:  sub,
	}
	for _, a := range submitted {
		path := currentProgressPath(pointsByApp[a.id], a.status)
		prev := sName
		for _, st := range path {
			next := "s_" + st
			linkCount[[2]string{prev, next}]++
			prev = next
		}
	}

	nodes := []Node{
		{Name: "all", Label: "全部机会"},
		{Name: nsName, Label: "未投递"},
		{Name: sName, Label: "已投递"},
	}
	nodeSeen := map[string]bool{"all": true, nsName: true, sName: true}
	var extra []string
	for k := range linkCount {
		for _, name := range []string{k[0], k[1]} {
			if nodeSeen[name] {
				continue
			}
			nodeSeen[name] = true
			extra = append(extra, name)
		}
	}
	sort.Slice(extra, func(i, j int) bool {
		ri, rj := stageSortKey(extra[i]), stageSortKey(extra[j])
		if ri != rj {
			return ri < rj
		}
		return extra[i] < extra[j]
	})
	for _, name := range extra {
		nodes = append(nodes, stageNode(name))
	}

	links := make([]Link, 0, len(linkCount))
	for k, v := range linkCount {
		links = append(links, Link{Source: k[0], Target: k[1], Value: v})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Source == links[j].Source {
			return links[i].Target < links[j].Target
		}
		return links[i].Source < links[j].Source
	})

	return &Sankey{
		Nodes: nodes, Links: links, CohortCount: cohort,
		AsOf: now.UTC().Format(time.RFC3339), DefinitionVersion: DefinitionVersion,
		Mode:  "current",
		Notes: "当前快照：全部机会先分为已投递 / 未投递（与「待投递」指标同口径）；已投递即等待反馈，后续只展开真正走过的笔试 / 初筛 / 面试 / Offer，终态挂在最后一程。未走过的阶段不出现。未投递不再细分。",
	}, nil
}

// pipelineProcessRank is the current-progress column order after 已投递.
// saved / preparing / applied are absorbed into the submitted node;
// accepted is a sink. Rank-order edges are a DAG.
var pipelineProcessRank = map[string]int{
	domain.StatusAssessment:   1,
	domain.StatusScreening:    2,
	domain.StatusInterviewing: 3,
	domain.StatusOffer:        4,
}

func happened(now time.Time, occurred *time.Time) bool {
	return occurred == nil || !occurred.After(now)
}

// happenedStagesFor loads already-happened stage points for the submitted
// cohort in one round-trip. Future-dated plans are skipped so the path
// cannot contradict applications.status (RecomputeStatus uses the same
// hasHappened rule).
func (r *Repo) happenedStagesFor(ctx context.Context, ids []int64, now time.Time) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Pool().Query(ctx, `SELECT application_id, status, occurred_at
		FROM application_stage_points
		WHERE application_id = ANY($1::bigint[])
		ORDER BY application_id, pinned_first DESC, occurred_at ASC NULLS LAST, (source = 'milestone') ASC, source_id ASC`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var st string
		var occurred *time.Time
		if err := rows.Scan(&id, &st, &occurred); err != nil {
			return nil, err
		}
		if !happened(now, occurred) {
			continue
		}
		out[id] = append(out[id], st)
	}
	return out, rows.Err()
}

// currentProgressPath turns one application's happened statuses into the
// walk after 已投递: unique later process stages in pipeline order, then
// the current terminal (if any). applied / preparing / saved never become
// their own nodes. In-progress rows stop at the current later stage
// (later-rank visits from a rollback are dropped — those belong on 历史路径).
func currentProgressPath(stages []string, current string) []string {
	seen := map[string]bool{}
	var process []string
	var prev string
	for _, st := range stages {
		if st == "" || st == prev {
			continue
		}
		prev = st
		if _, ok := pipelineProcessRank[st]; !ok {
			continue
		}
		if seen[st] {
			continue
		}
		seen[st] = true
		process = append(process, st)
	}
	sort.Slice(process, func(i, j int) bool {
		return pipelineProcessRank[process[i]] < pipelineProcessRank[process[j]]
	})

	if domain.IsTerminal(current) {
		if len(process) == 0 {
			return []string{current}
		}
		return append(process, current)
	}

	maxRank, ok := pipelineProcessRank[current]
	if !ok {
		// still at 已投递 / 准备材料: no later-stage columns
		return nil
	}
	var out []string
	for _, st := range process {
		if pipelineProcessRank[st] <= maxRank {
			out = append(out, st)
		}
	}
	if !seen[current] {
		out = append(out, current)
		sort.Slice(out, func(i, j int) bool {
			return pipelineProcessRank[out[i]] < pipelineProcessRank[out[j]]
		})
	}
	return out
}

func stageNode(name string) Node {
	st := strings.TrimPrefix(name, "s_")
	label := StatusName[st]
	if label == "" {
		label = st
	}
	return Node{Name: name, Label: label}
}

func stageSortKey(name string) int {
	st := strings.TrimPrefix(name, "s_")
	if r, ok := pipelineProcessRank[st]; ok {
		return r
	}
	switch st {
	case domain.StatusAccepted:
		return 10
	case domain.StatusRejected:
		return 11
	case domain.StatusWithdrawn:
		return 12
	case domain.StatusClosed:
		return 13
	}
	return 20
}

// --- Sankey B: real historical paths ---

type pathStep struct {
	Status string
}

// SankeyB reconstructs the actual path for every application from its events.
// (step_index, status) is the node id; edges only connect adjacent steps, so
// "interviewing → screening → interviewing" cannot create a cycle (§5.4).
func (r *Repo) SankeyB(ctx context.Context, req *SnapshotRequest, maxSteps int) (*Sankey, error) {
	if maxSteps <= 0 {
		maxSteps = 12
	}
	where, args := r.whereClause(req)
	// qualify owner columns with a. to disambiguate from events e
	where = strings.ReplaceAll(where, "owner_id = $", "a.owner_id = $")
	where = strings.ReplaceAll(where, "saved_at", "a.saved_at")
	where = strings.ReplaceAll(where, "status = $", "a.status = $")
	where = strings.ReplaceAll(where, "submitted_at", "a.submitted_at")
	where = strings.ReplaceAll(where, "company_name = $", "a.company_name = $")
	rows, err := r.db.Pool().Query(ctx, `SELECT a.id,
		CASE WHEN EXISTS (SELECT 1 FROM application_stage_points p WHERE p.application_id=a.id) THEN 'events' ELSE 'none' END,
		a.status
		FROM applications a
		WHERE `+where+` GROUP BY a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type appInfo struct {
		id     int64
		status string
		hasEv  bool
	}
	var apps []appInfo
	for rows.Next() {
		var ai appInfo
		var marker string
		if err := rows.Scan(&ai.id, &marker, &ai.status); err != nil {
			return nil, err
		}
		ai.hasEv = marker == "events"
		apps = append(apps, ai)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	nodeCount := map[string]int64{}
	linkCount := map[[2]string]int64{}
	var cohort int64
	// "导入起点 → 已知当前状态" for records without events
	const importStart = "import_start"
	nodeCount["root"] = 0
	nodeCount[importStart] = 0
	_ = nodeCount

	for _, ai := range apps {
		cohort++
		if !ai.hasEv {
			// imported history: no path reconstruction
			k := "import_start"
			key := [2]string{"root", k}
			linkCount[key]++
			k2 := [2]string{k, "cur_" + ai.status}
			linkCount[k2]++
			nodeCount["root"]++
			continue
		}
		stages, err := r.stagePathFor(ctx, ai.id)
		if err != nil {
			return nil, err
		}
		// Compress consecutive same-status points into single steps; each step
		// gets (stepIndex,status). The first point (建档) anchors the path, so
		// saved-only records still show one.
		var steps []pathStep
		for _, st := range stages {
			if len(steps) > 0 && steps[len(steps)-1].Status == st {
				continue // same status repetition is not a transition
			}
			steps = append(steps, pathStep{Status: st})
		}
		// Steps beyond maxSteps collapse into a terminal "更多步骤已折叠" node.
		folded := len(steps) > maxSteps
		if folded {
			steps = steps[:maxSteps]
		}
		prev := "root"
		for i, s := range steps {
			key := fmt.Sprintf("%d:%s", i+1, s.Status)
			linkCount[[2]string{prev, key}]++
			prev = key
		}
		// terminal: 截至当前：<status>
		term := "end_" + ai.status
		linkCount[[2]string{prev, term}]++
		if folded {
			linkCount[[2]string{term, "folded"}]++
		}
	}

	// emit nodes/links
	nodes := []Node{{Name: "root", Label: "起点"}}
	nodes = append(nodes, Node{Name: "folded", Label: "后续步骤已折叠"})
	linkList := []Link{}
	linkSeen := map[[2]string]int64{}
	for k, v := range linkCount {
		linkSeen[k] += v
	}
	_ = linkSeen
	// collect node names from links to include counts
	nodeSeen := map[string]bool{}
	for k := range linkSeen {
		nodeSeen[k[0]] = true
		nodeSeen[k[1]] = true
	}
	nodeKeys := make([]string, 0, len(nodeSeen))
	for k := range nodeSeen {
		// root and folded are already seeded as explicit nodes
		if k == "root" || k == "folded" {
			continue
		}
		nodeKeys = append(nodeKeys, k)
	}
	sort.Strings(nodeKeys)
	for _, k := range nodeKeys {
		label := labelFor(k)
		nodes = append(nodes, Node{Name: k, Label: label})
	}
	for k, v := range linkSeen {
		linkList = append(linkList, Link{Source: k[0], Target: k[1], Value: v})
	}
	sort.Slice(linkList, func(i, j int) bool {
		if linkList[i].Source == linkList[j].Source {
			return linkList[i].Target < linkList[j].Target
		}
		return linkList[i].Source < linkList[j].Source
	})

	return &Sankey{
		Nodes: nodes, Links: linkList, CohortCount: cohort,
		AsOf: req.Now.UTC().Format(time.RFC3339), DefinitionVersion: DefinitionVersion,
		Mode:  "history",
		Notes: "实际历史路径：由有效状态事件重建，只画真实走过的阶段；终点标注截至当前状态。",
	}, nil
}

// stagePathFor returns the application's stage points in timeline order — the
// statuses it actually walked through. It reads application_stage_points
// (迁移 00006), so the diagram covers both legacy state-machine events (with a
// later correction overriding the event it points at, since the raw target
// would keep a mis-click in the picture forever) and the timeline nodes the
// user added themselves.
//
// Ordering mirrors what the user sees in the timeline panel: 建档 first, then
// business time ascending, nodes with no time last, ties resolved event-first.
func (r *Repo) stagePathFor(ctx context.Context, appID int64) ([]string, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT status FROM application_stage_points
		WHERE application_id=$1
		ORDER BY pinned_first DESC, occurred_at ASC NULLS LAST, (source = 'milestone') ASC, source_id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func labelFor(key string) string {
	if key == "root" {
		return "起点"
	}
	if key == "folded" {
		return "后续步骤已折叠"
	}
	if key == "import_start" {
		return "导入起点"
	}
	if strings.HasPrefix(key, "end_") {
		st := strings.TrimPrefix(key, "end_")
		return "截至当前：" + StatusName[st]
	}
	if strings.HasPrefix(key, "cur_") {
		st := strings.TrimPrefix(key, "cur_")
		return StatusName[st]
	}
	parts := strings.SplitN(key, ":", 2)
	if len(parts) == 2 {
		st := parts[1]
		return fmt.Sprintf("第%s步 · %s", parts[0], StatusName[st])
	}
	return key
}

var _ = json.Marshal
