package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestAdminUserEmailLength(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for _, length := range []int{254, 255} {
			t.Run(fmt.Sprintf("%s/%d", method, length), func(t *testing.T) {
				s := newAuthzTestServer(t)
				admin := mkTestUser(t, s.store, "admin@example.com", models.UserRoleAdmin)
				target := mkTestUser(t, s.store, "existing@example.com", models.UserRoleMember)
				path := "/api/v1/admin/users"
				want := fiber.StatusCreated
				if method == http.MethodPut {
					path += fmt.Sprintf("/%d", target.ID)
					want = fiber.StatusOK
				}
				email := strings.Repeat("a", length-len("@example.com")) + "@example.com"
				body, err := json.Marshal(map[string]string{"email": email, "full_name": "Test User", "role": "member", "status": "active"})
				if err != nil {
					t.Fatal(err)
				}
				if length > 254 {
					want = fiber.StatusBadRequest
				}
				resp := doAuthzRequest(t, s, method, path, string(body), withSession(sessionCookie(t, s.store, admin)))
				if resp.status != want {
					t.Fatalf("status = %d, want %d", resp.status, want)
				}
				if length > 254 && resp.message != "email: email must be at most 254 bytes" {
					t.Fatalf("validation message = %q", resp.message)
				}
			})
		}
	}
}
