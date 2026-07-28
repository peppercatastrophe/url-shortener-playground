package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
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

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Minute)

	if err := initSchema(db); err != nil {
		log.Fatalf("cannot init schema: %v", err)
	}

	app := fiber.New(fiber.Config{ErrorHandler: jsonErrorHandler})
	app.Use(logger.New())

	h := &Handler{db: db}
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

// initSchema makes the urls table if it does not exist.
func initSchema(db *sql.DB) error {
	const q = `
CREATE TABLE IF NOT EXISTS urls (
    code        VARCHAR(16) PRIMARY KEY,
    original_url TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);`
	_, err := db.Exec(q)
	return err
}

// Handler holds the shared database connection.
type Handler struct {
	db *sql.DB
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

	const q = `INSERT INTO urls (code, original_url) VALUES ($1, $2)`
	if _, err := h.db.ExecContext(c.Context(), q, code, req.URL); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "cannot store url")
	}

	return c.JSON(ShortenResponse{Code: code, URL: req.URL})
}

// Redirect reads the original URL for the given code and sends the client there.
func (h *Handler) Redirect(c *fiber.Ctx) error {
	code := c.Params("code")
	if code == "" {
		return fiber.NewError(fiber.StatusBadRequest, "code is required")
	}

	var original string
	const q = `SELECT original_url FROM urls WHERE code = $1`
	err := h.db.QueryRowContext(c.Context(), q, code).Scan(&original)
	if err == sql.ErrNoRows {
		return fiber.NewError(fiber.StatusNotFound, "code not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "cannot read url")
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