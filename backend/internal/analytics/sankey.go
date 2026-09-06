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

const DefinitionVersion = 1

// SankeyA builds the fixed three-layer "current progress" graph:
//
//	全部机会 → 是否已投递 → 当前状态
//
// Every application appears once per layer; the middle layer keys on
// submitted_at presence so withdrawn-before-submit never merges with
// withdrawn-after-submit (§5.3).
func (r *Repo) SankeyA(ctx context.Context, req *SnapshotRequest) (*Sankey, error) {
	where, args := r.whereClause(req)
	rows, err := r.db.Pool().Query(ctx, `SELECT id, status,
		CASE WHEN submitted_at IS NOT NULL THEN true ELSE false END
		FROM applications WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type bucket struct{ notSubmitted, submitted []string }
	byStatus := map[string]*bucket{}
	var cohort int64
	seen := map[string]bool{}
	for rows.Next() {
		var id int64
		var st string
		var sub bool
		if err := rows.Scan(&id, &st, &sub); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d", id)
		if seen[key] {
			continue
		}
		seen[key] = true
		cohort++
		b := byStatus[st]
		if b == nil {
			b = &bucket{}
			byStatus[st] = b
		}
		if sub {
			b.submitted = append(b.submitted, key)
		} else {
			b.notSubmitted = append(b.notSubmitted, key)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Count not-submitted and submitted totals.
	var notSub, sub int64
	for _, b := range byStatus {
		notSub += int64(len(b.notSubmitted))
		sub += int64(len(b.submitted))
	}

	nodes := []Node{{Name: "all", Label: "全部机会"}}
	links := []Link{}
	nsName, sName := "not_submitted", "submitted"
	submittedNodes := map[string]bool{}
	// "尚未投递" statuses are in PreparingStatuses, but defensively: any
	// application in a preparing status counts as not submitted.
	submittedNodeName := map[string]string{
		domain.StatusApplied: "applied", domain.StatusScreening: "screening",
		domain.StatusAssessment: "assessment", domain.StatusInterviewing: "interviewing",
		domain.StatusOffer: "offer", domain.StatusAccepted: "accepted",
		domain.StatusRejected: "rejected", domain.StatusWithdrawn: "withdrawn_post",
		domain.StatusClosed: "closed",
	}
	notSubmittedNodeName := map[string]string{
		domain.StatusSaved: "saved", domain.StatusPreparing: "preparing",
		domain.StatusWithdrawn: "withdrawn_pre", domain.StatusClosed: "closed_pre",
	}

	// layer 1 edges
	if notSub > 0 {
		links = append(links, Link{Source: "all", Target: nsName, Value: notSub})
	}
	if sub > 0 {
		links = append(links, Link{Source: "all", Target: sName, Value: sub})
	}
	// layer 2 edges + layer 3 nodes
	type sn struct{ name, label string }
	grouped := map[sn]int64{}
	for st, b := range byStatus {
		for _, k := range b.notSubmitted {
			label := StatusName[st]
			if label == "" {
				label = st
			}
			grouped[sn{name: "ns_" + st, label: label}]++
			_ = k
		}
		for _, k := range b.submitted {
			label := StatusName[st]
			if label == "" {
				label = st
			}
			grouped[sn{name: "s_" + st, label: label}]++
			_ = k
		}
	}
	// emit layer-3 nodes sorted
	names := make([]sn, 0, len(grouped))
	for n := range grouped {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return names[i].name < names[j].name })
	for _, n := range names {
		nodes = append(nodes, Node{Name: n.name, Label: n.label})
		prefix := nsName
		if len(n.name) > 3 && n.name[:2] == "s_" {
			prefix = sName
		}
		links = append(links, Link{Source: prefix, Target: n.name, Value: grouped[n]})
	}
	// only add middle nodes when non-empty
	if notSub > 0 {
		nodes = append(nodes, Node{Name: nsName, Label: "尚未投递"})
	} else {
		// still include for visual consistency with 0 value handled by ECharts
		nodes = append(nodes, Node{Name: nsName, Label: "尚未投递"})
		links = append(links, Link{Source: "all", Target: nsName, Value: 0})
	}
	if sub > 0 {
		nodes = append(nodes, Node{Name: sName, Label: "已投递"})
	} else {
		nodes = append(nodes, Node{Name: sName, Label: "已投递"})
		links = append(links, Link{Source: "all", Target: sName, Value: 0})
	}
	_ = submittedNodes
	_ = submittedNodeName
	_ = notSubmittedNodeName

	return &Sankey{
		Nodes: nodes, Links: links, CohortCount: cohort,
		AsOf: req.Now.UTC().Format(time.RFC3339), DefinitionVersion: DefinitionVersion,
		Mode:  "current",
		Notes: "当前快照：全部机会按是否已投递分组，再按当前状态细分。不声称展示历史顺序。",
	}, nil
}

// --- Sankey B: real historical paths ---

type pathStep struct {
	Status string
	Time   time.Time
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
		CASE WHEN EXISTS (SELECT 1 FROM application_events e WHERE e.application_id=a.id) THEN 'events' ELSE 'none' END,
		a.status
		FROM applications a
		LEFT JOIN application_events e ON e.application_id = a.id AND e.event_type <> 'correction'
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
		evs, err := r.eventsFor(ctx, ai.id)
		if err != nil {
			return nil, err
		}
		// Compress consecutive same-status events into single steps; each step
		// gets (stepIndex,status). The initial created event (FromStatus nil)
		// contributes the first status so saved-only records still show a path.
		var steps []pathStep
		for _, e := range evs {
			if e.ToStatus == nil {
				continue
			}
			if e.FromStatus == nil && e.EventType == "created" {
				// created → initial state is the anchor of the path
				if len(steps) == 0 {
					steps = append(steps, pathStep{Status: *e.ToStatus, Time: e.OccurredAt})
				}
				continue
			}
			if e.FromStatus == nil || e.ToStatus == nil {
				continue
			}
			if len(steps) > 0 && steps[len(steps)-1].Status == *e.ToStatus {
				continue // same status repetition is not a transition
			}
			steps = append(steps, pathStep{Status: *e.ToStatus, Time: e.OccurredAt})
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

type eventRow struct {
	FromStatus *string
	ToStatus   *string
	EventType  string
	OccurredAt time.Time
}

func (r *Repo) eventsFor(ctx context.Context, appID int64) ([]eventRow, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT from_status, to_status, event_type, occurred_at FROM application_events
		WHERE application_id=$1 AND event_type <> 'correction' ORDER BY occurred_at, sequence`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var e eventRow
		if err := rows.Scan(&e.FromStatus, &e.ToStatus, &e.EventType, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
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
