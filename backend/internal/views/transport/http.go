package transport

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/views"
	vrepo "offerlog/backend/internal/views/repository"
	vservice "offerlog/backend/internal/views/service"
)

type Handler struct {
	svc *vservice.Service
}

func New(svc *vservice.Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the views + shared-query routes on a group at /views.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.listViews)
	g.POST("", h.createView)
	g.PATCH("/:view_id", h.updateView)
	g.DELETE("/:view_id", h.deleteView)
	g.POST("/query", h.query)
}

// PropertiesRoutes mounts the property-definition CRUD on a group at
// /properties.
func (h *Handler) PropertiesRoutes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.listProps)
	g.POST("", h.createProp)
	g.PATCH("/:prop_id", h.updateProp)
	g.DELETE("/:prop_id", h.deleteProp)
}

func (h *Handler) listViews(c *gin.Context) {
	user := httpx.UserFrom(c)
	views, err := h.svc.ListViews(c.Request.Context(), user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": views})
}

type viewDTO struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Layout        string          `json:"layout"`
	Columns       json.RawMessage `json:"columns"`
	FilterAST     json.RawMessage `json:"filter_ast"`
	Sort          json.RawMessage `json:"sort"`
	GroupBy       json.RawMessage `json:"group_by"`
	IsBuiltin     bool            `json:"is_builtin"`
	SchemaVersion int             `json:"schema_version"`
}

func (h *Handler) saveView(c *gin.Context) {
	user := httpx.UserFrom(c)
	var req viewDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	v := &vrepo.SavedView{
		ID: req.ID, OwnerID: user.ID, Name: req.Name, Layout: req.Layout,
		Columns: req.Columns, FilterAST: req.FilterAST, Sort: req.Sort, GroupBy: req.GroupBy,
		IsBuiltin: req.IsBuiltin, SchemaVersion: req.SchemaVersion,
	}
	out, err := h.svc.SaveView(c.Request.Context(), user.ID, v)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) createView(c *gin.Context) { h.saveView(c) }

func (h *Handler) updateView(c *gin.Context) {
	id, _ := httpx.PathID(c, "view_id")
	user := httpx.UserFrom(c)
	var req viewDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	req.ID = id
	v := &vrepo.SavedView{
		ID: id, OwnerID: user.ID, Name: req.Name, Layout: req.Layout,
		Columns: req.Columns, FilterAST: req.FilterAST, Sort: req.Sort, GroupBy: req.GroupBy,
		IsBuiltin: req.IsBuiltin, SchemaVersion: req.SchemaVersion,
	}
	out, err := h.svc.SaveView(c.Request.Context(), user.ID, v)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) deleteView(c *gin.Context) {
	user := httpx.UserFrom(c)
	id, _ := httpx.PathID(c, "view_id")
	if err := h.svc.DeleteView(c.Request.Context(), user.ID, id); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	httpx.Ok(c)
}

// -- properties --

type propDTO struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Key      string          `json:"key"`
	DataType string          `json:"data_type"`
	Options  json.RawMessage `json:"options"`
	Required bool            `json:"required"`
	Order    int             `json:"order"`
}

func (h *Handler) listProps(c *gin.Context) {
	user := httpx.UserFrom(c)
	props, err := h.svc.ListProperties(c.Request.Context(), user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": props})
}

func (h *Handler) createProp(c *gin.Context) {
	user := httpx.UserFrom(c)
	var req propDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	var opts []any
	_ = json.Unmarshal(req.Options, &opts)
	p, err := h.svc.CreateProperty(c.Request.Context(), user.ID, req.Name, req.Key, req.DataType, opts, req.Required)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (h *Handler) updateProp(c *gin.Context) {
	user := httpx.UserFrom(c)
	id, _ := httpx.PathID(c, "prop_id")
	var req propDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	var opts []any
	_ = json.Unmarshal(req.Options, &opts)
	p, err := h.svc.UpdateProperty(c.Request.Context(), user.ID, id, req.Name, req.DataType, opts, req.Required)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handler) deleteProp(c *gin.Context) {
	user := httpx.UserFrom(c)
	id, _ := httpx.PathID(c, "prop_id")
	if err := h.svc.DeleteProperty(c.Request.Context(), user.ID, id); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	httpx.Ok(c)
}

// query executes the shared view/filter DSL over applications.
func (h *Handler) query(c *gin.Context) {
	user := httpx.UserFrom(c)
	var req struct {
		Filters  []map[string]any `json:"filters"`
		Sort     []map[string]any `json:"sort"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	filters, sorts, err := parseFilterSort(req.Filters, req.Sort)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("bad_filter", err.Error()))
		return
	}
	items, total, applied, err := h.svc.RunQuery(c.Request.Context(), user.ID, filters, sorts, req.Page, req.PageSize)
	if err != nil {
		// filter DSL errors are client problems: 400, not 500
		code := "bad_filter"
		if errors.Is(err, vservice.ErrBadFilter) {
			code = "bad_filter"
		}
		httpx.WriteErr(c, httpx.BadRequest(code, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "applied_conditions": applied, "page": req.Page})
}

// parseFilterSort converts JSON maps into the typed filter/sort structures.
func parseFilterSort(rawF []map[string]any, rawS []map[string]any) ([]views.FilterNode, []views.SortItem, error) {
	return vservice.ParseFilters(rawF, rawS)
}
