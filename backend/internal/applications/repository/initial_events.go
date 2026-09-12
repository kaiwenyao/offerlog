package repository

import (
	"context"
	"time"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
)

// InitialEvents builds the timeline a brand-new record starts with — one 建档
// row, plus the rows that keep its 投递时间 on the timeline.
//
// 为什么建档不能只写一行：迁移 00006 之后 applications.submitted_at 是**派生**
// 列，RecomputeStatus 从时间线上第一个「已投递」落点回填它。任何「只写列、不留
// 落点」的建档路径都埋了一颗地雷——用户下一次加／改／删一个时间线节点，真实
// 投递时间就会被建档时刻顶掉，甚至直接清空（CSV 导入原先正是这样丢时间的）。
//
// 所以契约是：submittedAt 非空 ⇒ 时间线上一定有一个业务时间等于它的投递落点。
// submittedAt 必须是写进列里的那个值（预投递阶段先用 domain.SubmissionTimeFor
// 归零），列和时间线才不会各说一套。
func InitialEvents(appID, ownerID int64, status string, savedAt time.Time, submittedAt *time.Time, createdNote string) []*Event {
	// 建档那一格自己就是投递落点，时间也对得上：一行足够（「今天建档、今天投递」
	// 是最常见的一条，不该在时间线上多出一格重复的「已投递」）。
	soloCreated := submittedAt == nil || (status == domain.StatusApplied && submittedAt.Equal(savedAt))
	// 否则建档那一格必须描述投递**之前**的状态：它在时间线上被钉在最前面，而回放
	// 取第一个投递落点——让建档也声称「已投递」就等于把建档时刻当成投递时间。
	createdTo := status
	if !soloCreated {
		createdTo = domain.StatusSaved
	}
	created := &Event{
		ApplicationID: appID, OwnerID: ownerID, EventType: "created",
		ToStatus: &createdTo, Note: createdNote, OccurredAt: savedAt, ActorID: &ownerID,
	}
	if soloCreated {
		return []*Event{created}
	}
	applied := domain.StatusApplied
	out := []*Event{created, &Event{
		ApplicationID: appID, OwnerID: ownerID, EventType: "status_change",
		FromStatus: &createdTo, ToStatus: &applied, OccurredAt: *submittedAt, ActorID: &ownerID,
	}}
	if status == domain.StatusApplied {
		return out
	}
	// 建在更靠后的阶段（导入一条已经在面试的记录）：只知道「现在在哪一阶段」，
	// 不知道哪天进去的，于是用建档时刻——但不能早于投递时间，否则时间线的最后
	// 一格会变成投递、阶段被退回去。
	stageAt := savedAt
	if stageAt.Before(*submittedAt) {
		stageAt = *submittedAt
	}
	stageTo := status
	return append(out, &Event{
		ApplicationID: appID, OwnerID: ownerID, EventType: "status_change",
		FromStatus: &applied, ToStatus: &stageTo, OccurredAt: stageAt, ActorID: &ownerID,
	})
}

// InsertInitialEvents appends InitialEvents in order (sequence follows).
func (r *Repo) InsertInitialEvents(ctx context.Context, q database.Querier, events []*Event) error {
	for _, ev := range events {
		if err := r.InsertEvent(ctx, q, ev); err != nil {
			return err
		}
	}
	return nil
}
