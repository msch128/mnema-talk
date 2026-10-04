package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserContextKey contextKey = "authenticated_user"

type Claims struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	jwt.RegisteredClaims
}

type User struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	Role        string    `json:"role"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func GenerateToken(user User, secret string, expiryHours int) (string, error) {
	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expiryHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   user.ID.String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// Login verifies credentials against PostgreSQL and returns the user object
func Login(ctx context.Context, p *db.Pool, username, password string) (*User, error) {
	var user User
	var passwordHash string
	var avatarS3Key *string

	query := `SELECT id, username, display_name, COALESCE(bio, ''), password_hash, role, avatar_s3_key, created_at FROM users WHERE LOWER(username) = LOWER($1)`
	err := p.QueryRow(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.DisplayName, &user.Bio, &passwordHash, &user.Role, &avatarS3Key, &user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("invalid username or password")
		}
		return nil, fmt.Errorf("database query error: %w", err)
	}

	if !CheckPassword(password, passwordHash) {
		return nil, errors.New("invalid username or password")
	}

	if avatarS3Key != nil && *avatarS3Key != "" {
		user.AvatarURL = fmt.Sprintf("/api/media/%s", *avatarS3Key)
	}

	return &user, nil
}

// MinPasswordLength applies to registration and password changes
const MinPasswordLength = 10

// ErrWrongPassword is returned by ChangePassword when the current password does not match
var ErrWrongPassword = errors.New("current password is incorrect")

// ChangePassword verifies the current password and stores a new bcrypt hash
func ChangePassword(ctx context.Context, p *db.Pool, userID uuid.UUID, currentPassword, newPassword string) error {
	var passwordHash string
	if err := p.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&passwordHash); err != nil {
		return fmt.Errorf("user lookup error: %w", err)
	}
	if !CheckPassword(currentPassword, passwordHash) {
		return ErrWrongPassword
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	_, err = p.Exec(ctx, `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, hash, userID)
	return err
}

// Register creates a new user via invite code validation
func Register(ctx context.Context, p *db.Pool, username, displayName, password, inviteCode string) (*User, error) {
	tx, err := p.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Validate invite code
	var inviteID uuid.UUID
	var maxUses *int
	var usesCount int
	var expiresAt *time.Time

	inviteQuery := `SELECT id, max_uses, uses_count, expires_at FROM invites WHERE code = $1 FOR UPDATE`
	err = tx.QueryRow(ctx, inviteQuery, inviteCode).Scan(&inviteID, &maxUses, &usesCount, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("invalid invite code")
		}
		return nil, fmt.Errorf("invite lookup error: %w", err)
	}

	if expiresAt != nil && time.Now().After(*expiresAt) {
		return nil, errors.New("invite code has expired")
	}

	if maxUses != nil && usesCount >= *maxUses {
		return nil, errors.New("invite code usage limit reached")
	}

	// Hash password
	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Insert user
	var newUser User
	userInsertQuery := `
		INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $2, $3, 'user')
		RETURNING id, username, display_name, role, created_at
	`
	err = tx.QueryRow(ctx, userInsertQuery, username, displayName, hash).Scan(
		&newUser.ID, &newUser.Username, &newUser.DisplayName, &newUser.Role, &newUser.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			return nil, errors.New("username is already taken")
		}
		return nil, fmt.Errorf("user creation error: %w", err)
	}

	// Increment invite uses count
	_, err = tx.Exec(ctx, `UPDATE invites SET uses_count = uses_count + 1 WHERE id = $1`, inviteID)
	if err != nil {
		return nil, fmt.Errorf("failed to update invite usage: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &newUser, nil
}

// Middleware extracts JWT from Authorization Bearer header or auth_token cookie
func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := ""

			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
			} else if cookie, err := r.Cookie("auth_token"); err == nil {
				tokenStr = cookie.Value
			}

			if tokenStr == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			claims, err := ValidateToken(tokenStr, secret)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			user := User{
				ID:       claims.UserID,
				Username: claims.Username,
				Role:     claims.Role,
			}

			ctx := context.WithValue(r.Context(), UserContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminOnlyMiddleware ensures that only users with role 'admin' can access the route
func AdminOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(UserContextKey).(User)
		if !ok || user.Role != "admin" {
			http.Error(w, `{"error":"forbidden: admin access required"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
