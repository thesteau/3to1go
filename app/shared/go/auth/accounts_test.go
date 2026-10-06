package auth

import (
	"context"
	"testing"
)

type flagRepository struct {
	updates int
}

func (r *flagRepository) GetUserByUsername(context.Context, string) (*User, error) { return nil, nil }
func (r *flagRepository) GetUserByID(context.Context, int) (*User, error)          { return nil, nil }
func (r *flagRepository) UpdateUser(_ context.Context, id int, _, _ *string, _, mustChange *bool) (*User, error) {
	r.updates++
	return &User{ID: id, MustChangePassword: *mustChange}, nil
}

func TestDefaultPasswordCheckIsWorkedOutOncePerHash(t *testing.T) {
	defaultHash, err := HashPassword(DefaultAdminPassword)
	if err != nil {
		t.Fatal(err)
	}
	otherHash, err := HashPassword("a-real-password")
	if err != nil {
		t.Fatal(err)
	}
	repo := &flagRepository{}
	for i := 0; i < 3; i++ {
		user, _ := WithDefaultPasswordChangeRequired(repo, context.Background(), &User{ID: 1, PasswordHash: defaultHash})
		if !user.MustChangePassword {
			t.Fatal("the default password must require a change")
		}
		user, _ = WithDefaultPasswordChangeRequired(repo, context.Background(), &User{ID: 2, PasswordHash: otherHash})
		if user.MustChangePassword {
			t.Fatal("a real password must not require a change")
		}
	}
	for _, hash := range []string{defaultHash, otherHash} {
		if _, ok := defaultPasswordHashes.Load(hash); !ok {
			t.Errorf("hash %q was not remembered", hash[:12])
		}
	}
	if repo.updates != 3 {
		t.Errorf("updates = %d, want one per default-password lookup", repo.updates)
	}
}
