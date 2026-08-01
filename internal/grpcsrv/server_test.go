package grpcsrv

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestValidBearer covers the token comparison: correct token, wrong token,
// wrong scheme, empty header, and case-insensitivity of the scheme.
func TestValidBearer(t *testing.T) {
	// Pin the expected token for the test; restore after.
	saved := expectedToken
	t.Cleanup(func() { expectedToken = saved })
	expectedToken = "secret-test-token"

	cases := []struct {
		name string
		hdr  string
		want bool
	}{
		{"correct", "Bearer secret-test-token", true},
		{"wrong-token", "Bearer nope", false},
		{"missing-scheme", "secret-test-token", false},
		{"wrong-scheme", "Basic secret-test-token", false},
		{"empty", "", false},
		{"case-insensitive-scheme", "bearer secret-test-token", true},
		{"extra-space", "Bearer  secret-test-token", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validBearer(c.hdr); got != c.want {
				t.Fatalf("validBearer(%q)=%v want %v", c.hdr, got, c.want)
			}
		})
	}
}

// TestSubtleEqual ensures the constant-time comparison is correct for equal,
// differing, and length-mismatched inputs.
func TestSubtleEqual(t *testing.T) {
	if subtleEqual("abc", "abc") != 1 {
		t.Fatal("equal strings should return 1")
	}
	if subtleEqual("abc", "abd") != 0 {
		t.Fatal("differing strings should return 0")
	}
	if subtleEqual("abc", "ab") != 0 {
		t.Fatal("length-mismatched strings should return 0")
	}
	if subtleEqual("", "") != 1 {
		t.Fatal("empty strings should return 1")
	}
}

// TestAuthInterceptorRejectsMissingMetadata verifies a request with no metadata
// is rejected with codes.Unauthenticated and never reaches the handler.
func TestAuthInterceptorRejectsMissingMetadata(t *testing.T) {
	called := false
	handler := func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	}
	_, err := AuthInterceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, handler)
	if called {
		t.Fatal("handler must not be called on auth failure")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected a grpc status error, got %T: %v", err, err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Fatalf("code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

// TestAuthInterceptorRejectsMissingHeader verifies metadata with no
// authorization header is rejected.
func TestAuthInterceptorRejectsMissingHeader(t *testing.T) {
	called := false
	handler := func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{}))
	_, err := AuthInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	if called {
		t.Fatal("handler must not be called on missing auth header")
	}
	if st, _ := status.FromError(err); st.Code() != codes.Unauthenticated {
		t.Fatalf("code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

// TestAuthInterceptorRejectsBadToken verifies a bad token is rejected.
func TestAuthInterceptorRejectsBadToken(t *testing.T) {
	saved := expectedToken
	t.Cleanup(func() { expectedToken = saved })
	expectedToken = "secret-test-token"

	called := false
	handler := func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer wrong"))
	_, err := AuthInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	if called {
		t.Fatal("handler must not be called on bad token")
	}
	if st, _ := status.FromError(err); st.Code() != codes.Unauthenticated {
		t.Fatalf("code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

// TestAuthInterceptorAllowsGoodToken verifies a valid token lets the request
// through to the handler, which sees the return value/error unchanged.
func TestAuthInterceptorAllowsGoodToken(t *testing.T) {
	saved := expectedToken
	t.Cleanup(func() { expectedToken = saved })
	expectedToken = "secret-test-token"

	wantResp := "ok"
	wantErr := errors.New("handler-err")
	handler := func(context.Context, any) (any, error) {
		return wantResp, wantErr
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer secret-test-token"))
	gotResp, gotErr := AuthInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	if gotResp != wantResp {
		t.Fatalf("response = %v, want %v", gotResp, wantResp)
	}
	if gotErr != wantErr {
		t.Fatalf("error = %v, want %v", gotErr, wantErr)
	}
}