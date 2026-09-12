// Substatus model (docs/求职进度细化与状态回退方案.md §3): a large stage keeps
// its existing `status` key, and adds an optional `substatus` that names the
// concrete progress inside it. The legal combinations and the reachable targets
// are defined HERE and served to the frontend through
// GET /api/v1/meta/status-model, so there is exactly one whitelist instead of
// two hand-maintained ones.
package domain

// Substatus keys. They are scoped to their parent stage — the same word never
// means two different things across stages because ValidSubstatus checks the
// pair.
const (
	SubReady            = "ready"             // preparing: 材料就绪 · 待投递
	SubAwaitingSchedule = "awaiting_schedule" // screening/interviewing: 待安排
	SubPreparing        = "preparing"         // screening/assessment/interviewing: 准备中
	SubCompleted        = "completed"         // screening/assessment/interviewing: 已完成 · 等反馈
	SubPassed           = "passed"            // assessment: 已通过 · 等下一步
	SubReviewing        = "reviewing"         // offer: 待评估
	SubNegotiating      = "negotiating"       // offer: 协商
	SubReadyToAccept    = "ready_to_accept"   // offer: 待确认接受
)

// SubstatusMeta is one concrete progress option.
type SubstatusMeta struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// StageMeta describes a large stage for the frontend picker.
type StageMeta struct {
	Key       string          `json:"key"`
	Label     string          `json:"label"`
	Category  string          `json:"category"`
	Terminal  bool            `json:"terminal"`
	Substatus []SubstatusMeta `json:"substatus"`
	// DefaultSubstatus is used when entering the stage without an explicit
	// choice. "" means the stage carries no meaningful subdivision.
	DefaultSubstatus string `json:"default_substatus"`
}

// StageLabels are the large-stage display names. The UI owns its own copy for
// rendering; this one feeds the model payload and search results.
var StageLabels = map[string]string{
	StatusSaved:        "待投递",
	StatusPreparing:    "准备材料",
	StatusApplied:      "已投递",
	StatusScreening:    "初筛沟通",
	StatusAssessment:   "OA / 作业",
	StatusInterviewing: "面试",
	StatusOffer:        "Offer",
	StatusAccepted:     "已接受",
	StatusRejected:     "被拒绝",
	StatusWithdrawn:    "已撤回",
	StatusClosed:       "岗位关闭",
}

// substatusByStage lists the legal substatuses per stage. A stage missing from
// this map has no subdivision (only the un-subdivided "" is valid).
var substatusByStage = map[string][]SubstatusMeta{
	StatusPreparing: {
		{Key: SubReady, Label: "材料就绪 · 待投递"},
	},
	StatusScreening: {
		{Key: SubAwaitingSchedule, Label: "待安排初筛"},
		{Key: SubPreparing, Label: "准备初筛"},
		{Key: SubCompleted, Label: "已完成初筛 · 等反馈"},
	},
	StatusAssessment: {
		{Key: SubPreparing, Label: "准备 OA"},
		{Key: SubCompleted, Label: "已完成 OA · 等结果"},
		{Key: SubPassed, Label: "OA 已通过 · 等下一步"},
	},
	StatusInterviewing: {
		{Key: SubAwaitingSchedule, Label: "待安排面试"},
		{Key: SubPreparing, Label: "准备面试"},
		{Key: SubCompleted, Label: "已完成面试 · 等反馈"},
	},
	StatusOffer: {
		{Key: SubReviewing, Label: "待评估 Offer"},
		{Key: SubNegotiating, Label: "协商 Offer"},
		{Key: SubReadyToAccept, Label: "待确认接受"},
	},
}

// comboLabels maps (stage, substatus) → the exact label the list/detail shows.
// "" is the un-subdivided state (legacy rows and stages without subdivision).
var comboLabels = map[[2]string]string{
	{StatusSaved, ""}:                         "待投递",
	{StatusPreparing, ""}:                     "准备材料",
	{StatusPreparing, SubReady}:               "材料就绪 · 待投递",
	{StatusApplied, ""}:                       "已投递 · 等回复",
	{StatusScreening, ""}:                     "初筛沟通 · 未细分",
	{StatusScreening, SubAwaitingSchedule}:    "待安排初筛",
	{StatusScreening, SubPreparing}:           "准备初筛",
	{StatusScreening, SubCompleted}:           "已完成初筛 · 等反馈",
	{StatusAssessment, ""}:                    "OA / 作业 · 进度未细分",
	{StatusAssessment, SubPreparing}:          "准备 OA",
	{StatusAssessment, SubCompleted}:          "已完成 OA · 等结果",
	{StatusAssessment, SubPassed}:             "OA 已通过 · 等下一步",
	{StatusInterviewing, ""}:                  "面试中 · 未细分",
	{StatusInterviewing, SubAwaitingSchedule}: "待安排面试",
	{StatusInterviewing, SubPreparing}:        "准备面试",
	{StatusInterviewing, SubCompleted}:        "已完成面试 · 等反馈",
	{StatusOffer, ""}:                         "收到 Offer · 未细分",
	{StatusOffer, SubReviewing}:               "待评估 Offer",
	{StatusOffer, SubNegotiating}:             "协商 Offer",
	{StatusOffer, SubReadyToAccept}:           "待确认接受",
	{StatusAccepted, ""}:                      "已接受",
	{StatusRejected, ""}:                      "被拒绝",
	{StatusWithdrawn, ""}:                     "已撤回",
	{StatusClosed, ""}:                        "岗位关闭",
}

// ValidSubstatus reports whether (status, substatus) is a legal pair. "" is
// always legal: it is the explicit 「未细分」 state that every legacy row keeps
// until the user refines it.
func ValidSubstatus(status, substatus string) bool {
	if substatus == "" {
		return ValidStatus(status)
	}
	for _, s := range substatusByStage[status] {
		if s.Key == substatus {
			return true
		}
	}
	return false
}

// SubstatusOptions returns the legal substatuses for a stage (empty when the
// stage has no subdivision — never nil, so the JSON payload says [] not null).
func SubstatusOptions(status string) []SubstatusMeta {
	if opts, ok := substatusByStage[status]; ok {
		return opts
	}
	return []SubstatusMeta{}
}

// DefaultSubstatus is the substatus a stage starts with when the caller does
// not supply one. Deliberately "" everywhere: the product never invents a
// refinement the user did not choose (old rows must stay 未细分).
func DefaultSubstatus(string) string { return "" }

// ComboLabel renders the user-facing progress label for a (stage, substatus).
func ComboLabel(status, substatus string) string {
	if l, ok := comboLabels[[2]string{status, substatus}]; ok {
		return l
	}
	if ValidSubstatus(status, substatus) && substatus != "" {
		return StageLabels[status] + " · " + substatus
	}
	if l, ok := StageLabels[status]; ok {
		return l
	}
	return status
}

// ComboLabelForKind is ComboLabel with the assessment kind substituted:
// a take-home shows 准备作业 / 已提交作业 instead of OA wording.
func ComboLabelForKind(status, substatus, kind string) string {
	label := ComboLabel(status, substatus)
	if status != StatusAssessment || kind == "" || kind == "online_test" {
		return label
	}
	switch kind {
	case "take_home":
		switch substatus {
		case SubPreparing:
			return "准备作业"
		case SubCompleted:
			return "已提交作业 · 等结果"
		case SubPassed:
			return "作业已通过 · 等下一步"
		}
	}
	return label
}

// StatusModel is the payload the frontend consumes to render the progress
// picker without maintaining its own whitelist.
type StatusModel struct {
	Stages  []StageMeta              `json:"stages"`
	Targets map[string][]TargetCombo `json:"targets"`
	// MilestoneKinds is the event list the 「添加事件」 picker offers, and the
	// reference flow chart draws. Served from here so the client never keeps a
	// second copy of the kind → stage table (迁移 00006).
	MilestoneKinds []MilestoneKind `json:"milestone_kinds"`
}

// TargetCombo is one selectable target for a stage, including the substatuses
// that are legal there and whether a reason is mandatory.
type TargetCombo struct {
	Status           string          `json:"status"`
	Label            string          `json:"label"`
	Substatus        []SubstatusMeta `json:"substatus"`
	DefaultSubstatus string          `json:"default_substatus"`
	RequiresReason   bool            `json:"requires_reason"`
	// Bucket is the picker group: advance / backward / reopen / end.
	Bucket string `json:"bucket"`
}

// Bucket names for the target picker groups.
const (
	BucketAdvance  = "advance"
	BucketBackward = "backward"
	BucketReopen   = "reopen"
	BucketEnd      = "end"
)

// flowOrder is the recommended pipeline order; it only decides how targets are
// grouped (推进 vs 回退), never what is permitted.
//
// 初筛沟通 sits AFTER OA / 作业 — the common path is 投递 → 自动收到 OA, with the
// HR call happening once the test is passed (方案 §2：不要求先经过初筛).
var flowOrder = []string{
	StatusSaved, StatusPreparing, StatusApplied, StatusAssessment,
	StatusScreening, StatusInterviewing, StatusOffer, StatusAccepted,
}

func rank(s string) int {
	for i, k := range flowOrder {
		if k == s {
			return i
		}
	}
	return len(flowOrder) + 1
}

// BucketFor classifies a candidate target for the picker.
func BucketFor(from, to string) string {
	if TerminalStatuses[to] {
		return BucketEnd
	}
	if TerminalStatuses[from] {
		return BucketReopen
	}
	if rank(to) > rank(from) {
		return BucketAdvance
	}
	return BucketBackward
}

// TargetCombos returns every legal (target status, substatuses) for `from`,
// in the order the picker should present them.
func TargetCombos(from string) []TargetCombo {
	if !ValidStatus(from) {
		return nil
	}
	out := []TargetCombo{}
	for _, to := range AllStatuses {
		if !allowedTarget(from, to) {
			continue
		}
		if from == to && len(SubstatusOptions(to)) == 0 {
			// Same stage with no subdivision has nothing to change.
			continue
		}
		out = append(out, TargetCombo{
			Status:           to,
			Label:            StageLabels[to],
			Substatus:        SubstatusOptions(to),
			DefaultSubstatus: DefaultSubstatus(to),
			RequiresReason:   ReasonRequired(from, to),
			Bucket:           BucketFor(from, to),
		})
	}
	return out
}

// StatusModelFor builds the whole payload.
func StatusModelFor() StatusModel {
	m := StatusModel{Stages: []StageMeta{}, Targets: map[string][]TargetCombo{}, MilestoneKinds: MilestoneKinds}
	for _, s := range AllStatuses {
		m.Stages = append(m.Stages, StageMeta{
			Key: s, Label: StageLabels[s], Category: StatusCategories[s],
			Terminal: TerminalStatuses[s], Substatus: SubstatusOptions(s),
			DefaultSubstatus: DefaultSubstatus(s),
		})
	}
	for _, s := range AllStatuses {
		m.Targets[s] = TargetCombos(s)
	}
	return m
}

// Rank exposes the recommended pipeline position of a stage. It only orders
// suggestions; allowedTarget never consults it for permission.
func Rank(s string) int { return rank(s) }

// Activity kinds referenced by application_events.activity_kind and by
// applications.focus_activity_kind.
const (
	ActivityAssessment = "assessment"
	ActivityInterview  = "interview"
)

// Activity progress values shared by OA rounds and interview rounds.
const (
	ActProgressAwaiting  = "awaiting_schedule"
	ActProgressPreparing = "preparing"
	ActProgressCompleted = "completed"
	ActProgressCancelled = "cancelled"
)

// Activity results.
const (
	ActResultUnknown = "unknown"
	ActResultPassed  = "passed"
	ActResultFailed  = "failed"
)

// ValidActivityKind reports whether kind is a first-class activity.
func ValidActivityKind(kind string) bool {
	return kind == ActivityAssessment || kind == ActivityInterview
}

// ValidActivityProgress validates a progress value for a kind.
func ValidActivityProgress(kind, progress string) bool {
	switch kind {
	case ActivityAssessment:
		return progress == ActProgressPreparing || progress == ActProgressCompleted || progress == ActProgressCancelled
	case ActivityInterview:
		return progress == ActProgressAwaiting || progress == ActProgressPreparing ||
			progress == ActProgressCompleted || progress == ActProgressCancelled
	}
	return false
}

// ValidActivityResult validates a result value (shared by both kinds).
func ValidActivityResult(result string) bool {
	return result == ActResultUnknown || result == ActResultPassed || result == ActResultFailed
}

// SubstatusForActivity derives the application substatus from an activity's
// progress (方案 §6.1: OA 和面试的子状态由选中的活动进度派生). A completed
// assessment that passed maps to 已通过 · 等下一步; a completed round with an
// unknown result stays 已完成 · 等反馈 — finishing is never reported as passing.
func SubstatusForActivity(kind, progress, result string) string {
	switch kind {
	case ActivityAssessment:
		switch progress {
		case ActProgressCompleted:
			if result == ActResultPassed {
				return SubPassed
			}
			return SubCompleted
		case ActProgressCancelled:
			return ""
		default:
			return SubPreparing
		}
	case ActivityInterview:
		switch progress {
		case ActProgressCompleted:
			return SubCompleted
		case ActProgressPreparing:
			return SubPreparing
		case ActProgressAwaiting:
			return SubAwaitingSchedule
		case ActProgressCancelled:
			return ""
		}
	}
	// "" 表示未细分（旧数据没有明确证据）：保留未知，不默认成「准备中」
	// 或任意其他子状态（方案 §7）。
	return ""
}

// DeriveChangeType classifies a transition for the audit trail. It never
// affects whether the transition is allowed — only how the timeline labels it.
func DeriveChangeType(from, to string) string {
	if from == to {
		return ChangeAdvance
	}
	if TerminalStatuses[from] && !TerminalStatuses[to] {
		return ChangeReopen
	}
	if rank(to) < rank(from) {
		return ChangeRollback
	}
	return ChangeAdvance
}
