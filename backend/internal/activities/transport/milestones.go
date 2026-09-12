// Milestone endpoints: user-added timeline nodes (迁移 00005 / 00006).
//
// 这是记录求职进度的**主入口**。用户不再在一条规范流水线上挑「目标阶段」，
// 而是直接说「发生了什么」：选一个事件（OA / 初筛 / 面试 / Offer / 任意自定义），
// 选一个时间（也可以留空 = 时间未定），时间线自己排序。岗位的阶段由此推导——
// 每次写入都在同一个事务里以 recomputeStage 结尾，重新得出
// 「状态 = 时间线上最后一个带阶段效果的节点」。
//
// status_effect 由服务端按 kind 解析（appdomain.StatusEffectForKind），请求体
// 不接受这个字段：否则调用方可以绕过 kind 表把岗位设成任意阶段。
package transport

import (
	"context"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	actrepo "offerlog/backend/internal/activities/repository"
	"offerlog/backend/internal/platform/httpx"
)

type milestoneDTO struct {
	ID            int64  `json:"id"`
	ApplicationID int64  `json:"application_id"`
	Kind          string `json:"kind"`
	Label         string `json:"label"`
	// StatusEffect is derived from Kind, never accepted from the client. The UI
	// uses it to colour the node and to say 「这一步把状态改成了 X」.
	StatusEffect string     `json:"status_effect"`
	OccurredAt   *time.Time `json:"occurred_at"`
	Note         string     `json:"note"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// milestoneReq is the request body for create/PATCH. It mirrors milestoneDTO
// plus the request-only clear_occurred_at (explicitly unset the time → 时间
// 未定): the wire shape cannot distinguish 「null vs omitted」 for occurred_at
// itself, and the field must never leak into responses.
type milestoneReq struct {
	Kind            string     `json:"kind"`
	Label           string     `json:"label"`
	OccurredAt      *time.Time `json:"occurred_at"`
	ClearOccurredAt bool       `json:"clear_occurred_at"`
	Note            string     `json:"note"`
}

func milestoneToDTO(m *actrepo.Milestone) milestoneDTO {
	return milestoneDTO{
		ID: m.ID, ApplicationID: m.ApplicationID, Kind: m.Kind, Label: m.Label,
		StatusEffect: m.StatusEffect,
		OccurredAt:   m.OccurredAt, Note: m.Note, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func writeMilestoneErr(c *gin.Context, err error) {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, actrepo.ErrNotFound) {
		httpx.WriteErr(c, httpx.NotFound("节点不存在"))
		return
	}
	httpx.WriteErr(c, err)
}

// validateMilestoneKind normalizes the kind: empty → custom; kept short and
// slug-ish so it can key colors/icons, but the set is open — the whole point
// of milestones is that not every position follows the same event list.
func validateMilestoneKind(kind string) (string, bool) {
	if kind == "" {
		return actrepo.MilestoneKindCustom, true
	}
	if utf8.RuneCountInString(kind) > 40 {
		return "", false
	}
	for _, ch := range kind {
		if ch <= ' ' || ch == '·' {
			return "", false
		}
	}
	return kind, true
}

func (h *Handler) listMilestones(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	items, err := h.repo.ListMilestones(c.Request.Context(), appID, user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	out := make([]milestoneDTO, 0, len(items))
	for _, m := range items {
		out = append(out, milestoneToDTO(m))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// milestonePayload builds + validates the row from the request body.
func milestonePayload(c *gin.Context, req *milestoneReq) (*actrepo.Milestone, bool) {
	kind, ok := validateMilestoneKind(req.Kind)
	if !ok {
		httpx.WriteErr(c, httpx.BadRequest("invalid_kind", "事件类型不合法"))
		return nil, false
	}
	label := req.Label
	if utf8.RuneCountInString(label) > 100 {
		httpx.WriteErr(c, httpx.BadRequest("invalid_label", "名称过长（≤100 字）"))
		return nil, false
	}
	if utf8.RuneCountInString(req.Note) > 2000 {
		httpx.WriteErr(c, httpx.BadRequest("invalid_note", "备注过长（≤2000 字）"))
		return nil, false
	}
	return &actrepo.Milestone{
		Kind: kind, Label: label,
		OccurredAt: req.OccurredAt, Note: req.Note,
	}, true
}

func (h *Handler) createMilestone(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	var req milestoneReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if err := h.repo.AppOwnedBy(c.Request.Context(), h.repo.Pool(), appID, user.ID); err != nil {
		httpx.WriteErr(c, ownershipErr(err))
		return
	}
	m, ok := milestonePayload(c, &req)
	if !ok {
		return
	}
	m.ApplicationID, m.OwnerID = appID, user.ID
	// 建节点和重算阶段必须同一个事务提交：否则中途失败会留下一个时间线上看得见、
	// 状态却没跟上的节点，而用户没有任何办法让它们重新对齐。
	err = h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := h.repo.CreateMilestone(ctx, tx, m); err != nil {
			return err
		}
		return h.recomputeStage(ctx, tx, user.ID, appID)
	})
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, milestoneToDTO(m))
}

func (h *Handler) updateMilestone(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	mid, err := httpx.PathID(c, "milestone_id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的节点 ID"))
		return
	}
	var req milestoneReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Ownership gate before mutating (same shape as updateAssessment).
	if _, err := h.repo.GetMilestone(c.Request.Context(), h.repo.Pool(), appID, user.ID, mid); err != nil {
		writeMilestoneErr(c, err)
		return
	}
	m, ok := milestonePayload(c, &req)
	if !ok {
		return
	}
	m.ID, m.ApplicationID, m.OwnerID = mid, appID, user.ID
	// The wire shape cannot distinguish 「omitted」 from 「null」 for the
	// timestamp, so a PATCH that does not mention the time keeps it (same
	// rule as assessments). Clearing is explicit via clear_occurred_at.
	var fresh *actrepo.Milestone
	err = h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		existing, err := h.repo.GetMilestoneForUpdate(ctx, tx, appID, user.ID, mid)
		if err != nil {
			return err
		}
		if req.Kind == "" {
			m.Kind = existing.Kind
		}
		if req.Label == "" {
			m.Label = existing.Label
		}
		if req.OccurredAt == nil {
			m.OccurredAt = existing.OccurredAt
		}
		if req.ClearOccurredAt {
			m.OccurredAt = nil
		}
		if err := h.repo.UpdateMilestone(ctx, tx, m); err != nil {
			return err
		}
		// 改时间会重排时间线，改类型会换掉阶段效果——两者都可能换掉「最后一格」，
		// 所以编辑和新增一样要重算。
		if err := h.recomputeStage(ctx, tx, user.ID, appID); err != nil {
			return err
		}
		// Re-read so the response carries the stored timestamps instead of
		// zero values (updated_at is what the client renders on stale refetch).
		fresh, err = h.repo.GetMilestone(ctx, tx, appID, user.ID, mid)
		return err
	})
	if err != nil {
		writeMilestoneErr(c, err)
		return
	}
	c.JSON(http.StatusOK, milestoneToDTO(fresh))
}

func (h *Handler) deleteMilestone(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	mid, err := httpx.PathID(c, "milestone_id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的节点 ID"))
		return
	}
	if _, err := h.repo.GetMilestone(c.Request.Context(), h.repo.Pool(), appID, user.ID, mid); err != nil {
		writeMilestoneErr(c, err)
		return
	}
	// 删掉最后一个节点必须让状态退回上一格，否则岗位会停在一个时间线上已经不存在
	// 的阶段里。
	err = h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := h.repo.DeleteMilestone(ctx, tx, appID, user.ID, mid); err != nil {
			return err
		}
		return h.recomputeStage(ctx, tx, user.ID, appID)
	})
	if err != nil {
		writeMilestoneErr(c, err)
		return
	}
	httpx.Ok(c)
}
