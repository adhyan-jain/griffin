package handlers

import (
	"net/http"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// AliasesHandler serves the owner's aliases ("mummy" means "mom") so every client can show and use them.
type AliasesHandler struct{ repo repository.AliasRepository }

// NewAliasesHandler builds an AliasesHandler.
func NewAliasesHandler(repo repository.AliasRepository) *AliasesHandler {
	return &AliasesHandler{repo: repo}
}

// List returns every alias as {"aliases": {"mummy": "mom"}}.
func (h *AliasesHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	m, err := h.repo.List(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"aliases": m})
}

type putAliasRequest struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
}

// Put adds or replaces one alias.
func (h *AliasesHandler) Put(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req putAliasRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	alias, err := domain.NormalizeAlias(req.Alias, 40)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	target, err := domain.NormalizeAlias(req.Target, 80)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if alias == target {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}
	if err := h.repo.Put(r.Context(), userID, alias, target); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"alias": alias, "target": target})
}

// Delete removes one alias.
func (h *AliasesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	alias, err := domain.NormalizeAlias(r.PathValue("alias"), 40)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	ok, err := h.repo.Delete(r.Context(), userID, alias)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if !ok {
		httputil.ErrorJSON(w, domain.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
