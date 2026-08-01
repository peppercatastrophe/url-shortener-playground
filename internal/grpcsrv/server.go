// Package grpcsrv implements the gRPC Shortener service.
// It wraps the same store/cache/queue dependencies as the REST API so the
// two transports share a single backend: gRPC is for internal
// service-to-service calls (lower overhead, strong typing, deadline
// propagation), REST is for human/client-facing traffic.
package grpcsrv

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"url-shortener/internal/cache"
	"url-shortener/internal/proto/shortenerpb"
	"url-shortener/internal/queue"
	"url-shortener/internal/store"
)

// expectedToken is the bearer token clients must present. In production this
// would come from a secret store / OIDC introspection; for this playground it
// is configured via the GRPC_AUTH_TOKEN env var, defaulting to a known value.
// A real deployment must NOT rely on the default.
var expectedToken = "shortener-secret-token"

// SetAuthToken overrides the expected bearer token at startup (called from
// cmd/grpc/main.go from the GRPC_AUTH_TOKEN env var).
func SetAuthToken(t string) {
	if t != "" {
		expectedToken = t
	}
}

// Server implements shortenerpb.ShortenerServer.
type Server struct {
	shortenerpb.UnimplementedShortenerServer
	store *store.Store
	cache *cache.Cache
	pub   *queue.Publisher
}

// NewServer returns a ShortenerServer backed by the given dependencies.
func NewServer(st *store.Store, c *cache.Cache, pub *queue.Publisher) *Server {
	return &Server{store: st, cache: c, pub: pub}
}

// Resolve looks up the original URL for a short code, reading from the Redis
// cache first and back-filling from PostgreSQL on a miss — the same read-through
// path as the REST Redirect handler, minus the HTTP redirect semantics.
func (s *Server) Resolve(ctx context.Context, req *shortenerpb.ResolveRequest) (*shortenerpb.ResolveResponse, error) {
	code := req.GetCode()
	if code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	var original string
	fromCache := false
	if url, ok := s.cache.GetURL(ctx, code); ok {
		fromCache = true
		original = url
	} else {
		var err error
		original, err = s.store.GetURL(ctx, code)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "code not found: %v", err)
		}
		s.cache.SetURL(ctx, code, original)
	}

	return &shortenerpb.ResolveResponse{OriginalUrl: original, FromCache: fromCache}, nil
}

// ReportClick accepts a click event and publishes it to the RabbitMQ clicks
// queue. It translates the timestamp from unix-nano int64 to time.Time, the
// canonical Go representation. Publish is best-effort at the API layer but the
// gRPC call surfaces queue errors so callers can decide on retry.
func (s *Server) ReportClick(ctx context.Context, evt *shortenerpb.ClickEvent) (*shortenerpb.ClickResponse, error) {
	code := evt.GetCode()
	if code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	var clickedAt time.Time
	if n := evt.GetClickedAtUnixNano(); n > 0 {
		clickedAt = time.Unix(0, n)
	} else {
		clickedAt = time.Now()
	}

	if err := s.pub.Publish(ctx, queue.ClickEvent{Code: code, ClickedAt: clickedAt}); err != nil {
		return nil, status.Errorf(codes.Unavailable, "publish click: %v", err)
	}
	return &shortenerpb.ClickResponse{}, nil
}

// AuthInterceptor is a unary server interceptor that validates the bearer token
// in the "authorization" metadata header. Calls without a valid token are
// rejected with codes.Unauthenticated before reaching the handler.
func AuthInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "authorization token required")
	}
	if !validBearer(values[0]) {
		return nil, status.Error(codes.Unauthenticated, "invalid auth token")
	}
	return handler(ctx, req)
}

// validBearer checks the "Bearer <token>" form against expectedToken.
func validBearer(header string) bool {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	token := strings.TrimSpace(header[len(prefix):])
	// Constant-time compare to avoid timing side channels on token equality.
	if subtleEqual(token, expectedToken) != 0 {
		return true
	}
	return false
}

// subtleEqual returns 1 if a and b are equal, 0 otherwise, in constant time
// w.r.t. their common prefix length. We compare the full byte slices; differing
// lengths still leak length but not secret contents, which is acceptable here
// since the token is a config value, not a per-user secret.
func subtleEqual(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	if v == 0 {
		return 1
	}
	return 0
}
