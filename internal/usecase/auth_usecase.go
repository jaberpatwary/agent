package usecase

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"bangla-news-aggregator/internal/config"
	"bangla-news-aggregator/internal/repository"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

// Claims is the JWT payload used for admin sessions.
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type AuthUsecase interface {
	Login(username, password string) (token string, err error)
	ParseToken(tokenString string) (*Claims, error)
	ChangePassword(username, currentPassword, newPassword string) error
}

type authUsecase struct {
	adminRepo repository.AdminRepository
	cfg       *config.Config
}

func NewAuthUsecase(adminRepo repository.AdminRepository, cfg *config.Config) AuthUsecase {
	return &authUsecase{adminRepo: adminRepo, cfg: cfg}
}

func (u *authUsecase) Login(username, password string) (string, error) {
	admin, err := u.adminRepo.GetByUsername(username)
	if err != nil {
		return "", err
	}
	if admin == nil {
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	claims := Claims{
		Username: admin.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(u.cfg.JWTExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(u.cfg.JWTSecret))
}

func (u *authUsecase) ParseToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(u.cfg.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired token")
	}
	return claims, nil
}

func (u *authUsecase) ChangePassword(username, currentPassword, newPassword string) error {
	if len(newPassword) < 12 {
		return errors.New("new password must be at least 12 characters")
	}
	admin, err := u.adminRepo.GetByUsername(username)
	if err != nil {
		return err
	}
	if admin == nil || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(currentPassword)) != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return u.adminRepo.UpdatePassword(username, string(hash))
}
