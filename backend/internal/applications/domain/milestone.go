// Milestone kinds: the event list the user picks from when adding a node to a
// job's timeline.
//
// 这是本次改版的核心映射。旧模型要求用户在一条规范流水线上挑「目标阶段 + 子状态」，
// 但不是每个岗位都有 OA、初筛或面试。新模型只问「发生了什么」，阶段由事件推导：
// 每个 kind 携带一个 StatusEffect，岗位的当前状态 = 时间线上最后一个带
// StatusEffect 的节点的那个值（见 repository.RecomputeStatus）。
//
// kind 本身是**开放集合**——用户可以存任意 slug，未知 kind 的 StatusEffect 是 ""
// （只记事，不改阶段）。下面这张表只是建议清单与默认名称的来源。
package domain

// MilestoneKind is one suggested event type: what the user picks, what it is
// called by default, and which stage recording it puts the application in.
type MilestoneKind struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// StatusEffect is the stage this event moves the application into.
	// "" means the event is recorded on the timeline without touching the
	// stage (电话沟通 / 自定义事件).
	StatusEffect string `json:"status_effect"`
	// Group buckets the option in the picker: flow / end / other.
	Group string `json:"group"`
}

// Milestone kind keys. Named after what HAPPENED (投递 / 面试), not after the
// state it produces (已投递 / 面试中) — the user is recording an event, and the
// state is our derivation, not their input.
const (
	MKSave      = "save"
	MKPrepare   = "prepare"
	MKApply     = "apply"
	MKScreen    = "screen"
	MKOA        = "oa"
	MKInterview = "interview"
	MKOffer     = "offer"
	MKAccept    = "accept"
	MKReject    = "reject"
	MKWithdraw  = "withdraw"
	MKClose     = "close"
	MKPhone     = "phone"
	MKCustom    = "custom"
)

// Milestone picker groups.
const (
	MGroupFlow  = "flow"  // 推进流程
	MGroupEnd   = "end"   // 结束
	MGroupOther = "other" // 其他（不改阶段）
)

// MilestoneKinds is the ordered suggestion list served to the client. The order
// is the recommended flow, which is also the order the reference flow chart
// draws — it guides, it never constrains: any kind may be added at any time.
var MilestoneKinds = []MilestoneKind{
	{Key: MKSave, Label: "收藏岗位", StatusEffect: StatusSaved, Group: MGroupFlow},
	{Key: MKPrepare, Label: "准备材料", StatusEffect: StatusPreparing, Group: MGroupFlow},
	{Key: MKApply, Label: "投递", StatusEffect: StatusApplied, Group: MGroupFlow},
	{Key: MKScreen, Label: "初筛", StatusEffect: StatusScreening, Group: MGroupFlow},
	{Key: MKOA, Label: "OA / 笔试", StatusEffect: StatusAssessment, Group: MGroupFlow},
	{Key: MKInterview, Label: "面试", StatusEffect: StatusInterviewing, Group: MGroupFlow},
	{Key: MKOffer, Label: "收到 Offer", StatusEffect: StatusOffer, Group: MGroupFlow},
	{Key: MKAccept, Label: "接受 Offer", StatusEffect: StatusAccepted, Group: MGroupEnd},
	{Key: MKReject, Label: "被拒绝", StatusEffect: StatusRejected, Group: MGroupEnd},
	{Key: MKWithdraw, Label: "撤回申请", StatusEffect: StatusWithdrawn, Group: MGroupEnd},
	{Key: MKClose, Label: "岗位关闭", StatusEffect: StatusClosed, Group: MGroupEnd},
	{Key: MKPhone, Label: "电话沟通", StatusEffect: "", Group: MGroupOther},
	{Key: MKCustom, Label: "自定义事件", StatusEffect: "", Group: MGroupOther},
}

// milestoneByKey indexes MilestoneKinds for lookup.
var milestoneByKey = func() map[string]MilestoneKind {
	m := make(map[string]MilestoneKind, len(MilestoneKinds))
	for _, k := range MilestoneKinds {
		m[k.Key] = k
	}
	return m
}()

// StatusEffectForKind returns the stage a milestone of this kind puts the
// application in. Unknown kinds (the open set the user may invent) carry no
// stage effect: an event we cannot interpret must not silently move the record.
func StatusEffectForKind(kind string) string {
	return milestoneByKey[kind].StatusEffect
}

// MilestoneLabelForKind is the default display name for a kind. Unknown kinds
// fall back to 自定义节点 — the caller normally keeps whatever the user typed.
func MilestoneLabelForKind(kind string) string {
	if k, ok := milestoneByKey[kind]; ok {
		return k.Label
	}
	return "自定义节点"
}

// KnownMilestoneKind reports whether kind is in the suggestion list. Used only
// for presentation decisions; an unknown kind is still perfectly storable.
func KnownMilestoneKind(kind string) bool {
	_, ok := milestoneByKey[kind]
	return ok
}
