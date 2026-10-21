package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
)

func BenchmarkWindowLimiter(b *testing.B) {
	for _, size := range []int{1, 1000, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			l := auth.NewWindowLimiter(time.Hour, int(^uint(0)>>1))
			for i := range size {
				if !l.Allow(fmt.Sprint(i)) {
					b.Fatal("setup rejected")
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if !l.Allow("0") {
					b.Fatal("request rejected")
				}
			}
		})
	}
}

func BenchmarkWindowLimiterHTTP(b *testing.B) {
	l := auth.NewWindowLimiter(time.Hour, int(^uint(0)>>1))
	for i := range 10000 {
		if !l.Allow(fmt.Sprint(i)) {
			b.Fatal("setup rejected")
		}
	}
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		if !l.Allow("0") {
			return c.SendStatus(http.StatusTooManyRequests)
		}
		return c.SendStatus(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		resp, err := app.Test(req)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("status = %d", resp.StatusCode)
		}
	}
}
