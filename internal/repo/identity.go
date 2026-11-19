package repo

import (
	"context"
	"strings"
)

type Identity struct {
	Name, Email string
}

func ReadIdentity(ctx context.Context, path string) (Identity, error) {
	raw, err := Run(ctx, path, "git", "config", "--null", "--list")
	var identity Identity
	if err != nil {
		return identity, err
	}
	for _, entry := range strings.Split(raw, "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		switch key {
		case "user.name":
			identity.Name = value
		case "user.email":
			identity.Email = value
		}
	}
	return identity, nil
}

func WriteIdentity(ctx context.Context, path string, identity Identity) error {
	if _, err := Run(ctx, path, "git", "config", "--local", "--replace-all", "--", "user.name", identity.Name); err != nil {
		return err
	}
	_, err := Run(ctx, path, "git", "config", "--local", "--replace-all", "--", "user.email", identity.Email)
	return err
}
