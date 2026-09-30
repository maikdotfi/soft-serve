package fake_test

import (
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth/adminauthtest"
	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth/fake"
)

func TestFake_SatisfiesContract(t *testing.T) {
	a := fake.New(
		fake.User{Username: "admin", Token: "admin-token", Admin: true},
		fake.User{Username: "bob", Token: "bob-token"},
	)
	adminauthtest.Run(t, a, adminauthtest.Fixture{
		AdminUsername: "admin", AdminToken: "admin-token",
		UserUsername: "bob", UserToken: "bob-token",
	})
}
