package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/database"
)

type AuthHandler struct {
	db        *database.DB
	jwtSecret string
}

func NewAuthHandler(db *database.DB, jwtSecret string) *AuthHandler {
	return &AuthHandler{
		db:        db,
		jwtSecret: jwtSecret,
	}
}

type LoginRequest struct {
	Email string `json:"email"`
}

type LoginResponse struct {
	Token string      `json:"token"`
	User  UserProfile `json:"user"`
}

type UserProfile struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Login handles local / dev authentication by verifying or provisioning the user in Supabase auth tables.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// allow empty body defaulting to admin
		req.Email = "admin@griffin.internal"
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		req.Email = "admin@griffin.internal"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var userID uuid.UUID
	err := h.db.Pool.QueryRow(ctx, "SELECT id FROM auth.users WHERE email = $1 LIMIT 1", req.Email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		userID = uuid.New()
		_, err = h.db.Pool.Exec(ctx, "INSERT INTO auth.users (id, email) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING", userID, req.Email)
		if err != nil {
			httputil.ErrorJSON(w, fmt.Errorf("provision auth user: %w", err))
			return
		}
		_, err = h.db.Pool.Exec(ctx, "INSERT INTO profiles (id, email, full_name, role) VALUES ($1, $2, 'Griffin Operator', 'OWNER') ON CONFLICT (id) DO NOTHING", userID, req.Email)
		if err != nil {
			httputil.ErrorJSON(w, fmt.Errorf("provision user profile: %w", err))
			return
		}
	} else if err != nil {
		httputil.ErrorJSON(w, fmt.Errorf("query user: %w", err))
		return
	}

	// Generate JWT
	claims := jwt.MapClaims{
		"sub":   userID.String(),
		"email": req.Email,
		"role":  "authenticated",
		"exp":   time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(h.jwtSecret))
	if err != nil {
		httputil.ErrorJSON(w, fmt.Errorf("sign token: %w", err))
		return
	}

	httputil.JSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User: UserProfile{
			ID:    userID.String(),
			Email: req.Email,
			Role:  "OWNER",
		},
	})
}
