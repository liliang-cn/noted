// Package service implements the noted.v1 gRPC services on top of the store
// and the optional AI engine.
package service

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// uid returns the caller's user id. The auth interceptor guarantees one.
func uid(ctx context.Context) (string, error) {
	u, ok := auth.User(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "not authenticated")
	}
	return u.ID, nil
}

// toStatus maps domain errors onto gRPC codes; anything unexpected is logged
// and returned as an opaque Internal so no SQL text leaks to clients.
func toStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return status.Error(codes.NotFound, "not found")
	case errors.Is(err, store.ErrInvalid):
		msg := strings.TrimPrefix(err.Error(), store.ErrInvalid.Error()+": ")
		return status.Error(codes.InvalidArgument, msg)
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	slog.Error("internal error", "err", err)
	return status.Error(codes.Internal, "internal error")
}

// page resolves a page size and opaque token into LIMIT/OFFSET.
func page(size int32, token string) (limit, offset int, err error) {
	limit = int(size)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if token == "" {
		return limit, 0, nil
	}
	raw, derr := base64.RawURLEncoding.DecodeString(token)
	if derr == nil {
		offset, derr = strconv.Atoi(string(raw))
	}
	if derr != nil || offset < 0 {
		return 0, 0, status.Error(codes.InvalidArgument, "invalid page_token")
	}
	return limit, offset, nil
}

func nextToken(offset, returned, limit int) string {
	if returned <= limit {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset + limit)))
}

// maskPaths validates an update mask against the allowed field names.
func maskPaths(m *fieldmaskpb.FieldMask, allowed ...string) (map[string]bool, error) {
	if len(m.GetPaths()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "update_mask is required")
	}
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	out := map[string]bool{}
	for _, p := range m.GetPaths() {
		if !ok[p] {
			return nil, status.Errorf(codes.InvalidArgument, "update_mask: cannot update %q (allowed: %s)", p, strings.Join(allowed, ", "))
		}
		out[p] = true
	}
	return out, nil
}
