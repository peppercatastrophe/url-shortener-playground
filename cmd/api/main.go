package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"

	"url-shortener/internal/cache"
	"url-shortener/internal/queue"
	"url-shortener/internal/store"
)

// shortCodeLen is the number of characters in a generated short code.
const shortCodeLen = 6

// codeAlphabet is the set of characters used to build a short code.
const codeAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://shortener:shortener@localhost:5432/shortener?sslmode=disable"
	}

	st, err := store.New(dsn)
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}
	defer st.Close()

	if err := st.InitSchema(); err != nil {
		log.Fatalf("cannot init schema: %v", err)
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	cache := cache.New(redisAddr)
	if err := cache.Ping(context.Background()); err != nil {
		log.Printf("WARNING: cache disabled, will fall back to database: %v", err)
	} else {
		log.Println("cache connected")
	}

	rabbitAddr := os.Getenv("RABBITMQ_ADDR")
	if rabbitAddr == "" {
		rabbitAddr = "amqp://guest:guest@localhost:5672/"
	}
	pub, err := queue.NewPublisher(rabbitAddr)
	if err != nil {
		log.Printf("WARNING: click queue disabled, events will be lost: %v", err)
	} else if rabbitAddr != "" {
		log.Println("queue connected")
	}
	defer pub.Close()

	app := fiber.New(fiber.Config{ErrorHandler: jsonErrorHandler})
	app.Use(logger.New())

	h := &Handler{store: st, cache: cache, pub: pub}
	app.Post("/shorten", h.Shorten)
	app.Get("/healthz", func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) })
	app.Get("/:code", h.Redirect)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Printf("listening on :%s", port)
	log.Fatal(app.Listen(":" + port))
}

// Handler holds the shared dependencies for the HTTP handlers.
type Handler struct {
	store *store.Store
	cache *cache.Cache
	pub   *queue.Publisher
}

// ShortenRequest is the body sent to POST /shorten.
type ShortenRequest struct {
	URL string `json:"url"`
}

// ShortenResponse is the body returned by POST /shorten.
type ShortenResponse struct {
	Code string `json:"code"`
	URL  string `json:"url"`
}

// Shorten makes a short code for the given URL and stores the pair.
func (h *Handler) Shorten(c *fiber.Ctx) error {
	var req ShortenRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	if req.URL == "" {
		return fiber.NewError(fiber.StatusBadRequest, "url is required")
	}

	code, err := newCode()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "cannot make code")
	}

	if err := h.store.InsertURL(c.Context(), code, req.URL); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "cannot store url")
	}

	return c.JSON(ShortenResponse{Code: code, URL: req.URL})
}

// Redirect reads the original URL for the given code and sends the client there.
// It reads from the Redis cache first. On a cache miss, it reads from PostgreSQL
// and back-fills the cache so the next read is a hit.
// After a successful redirect, it publishes a click event to the queue.
// The publish is best-effort: a queue failure does not fail the redirect.
func (h *Handler) Redirect(c *fiber.Ctx) error {
	code := c.Params("code")
	if code == "" {
		return fiber.NewError(fiber.StatusBadRequest, "code is required")
	}

	var original string
	if url, ok := h.cache.GetURL(c.Context(), code); ok {
		log.Printf("cache HIT for %s", code)
		original = url
	} else {
		log.Printf("cache MISS for %s", code)
		var err error
		original, err = h.store.GetURL(c.Context(), code)
		if err != nil {
			return fiber.NewError(fiber.StatusNotFound, "code not found")
		}
		h.cache.SetURL(c.Context(), code, original)
	}

	// Best-effort: publish the click event without blocking the redirect.
	evt := queue.ClickEvent{Code: code, ClickedAt: time.Now()}
	if err := h.pub.Publish(c.Context(), evt); err != nil {
		log.Printf("click publish %s: %v", code, err)
	}

	return c.Redirect(original, http.StatusMovedPermanently)
}

// newCode returns a random short code of shortCodeLen characters.
func newCode() (string, error) {
	buf := make([]byte, shortCodeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(buf), nil
}

// jsonErrorHandler formats error responses as JSON.
func jsonErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}
