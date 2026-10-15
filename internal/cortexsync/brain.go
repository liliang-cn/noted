package cortexsync

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/liliang-cn/cortexdb/v2/pkg/core"
	"github.com/liliang-cn/cortexdb/v2/pkg/cortexdb"
	rpcv1 "github.com/liliang-cn/cortexdb/v2/pkg/rpc/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Brain runs CortexDB tools by name: the same tools (knowledge_save,
// knowledge_delete, delete_entities) the shared brain's MCP server offers, so
// what the sync writes is what the recall tools read.
type Brain interface {
	Call(ctx context.Context, tool string, args, out any) error
}

// ErrNotFound is what a Brain returns when the thing a tool was asked about
// does not exist, such as deleting a knowledge item that is already gone.
var ErrNotFound = errors.New("not found in cortexdb")

// Remote is a Brain on a CortexDB gRPC server, called through its
// ToolsService the way agent-go's shared-memory backend and cortexdb's own
// live view do.
type Remote struct {
	conn *grpc.ClientConn
	tool rpcv1.ToolsServiceClient
}

// Dial connects to a CortexDB gRPC server at addr (host:port). A non-empty
// token is sent as a bearer token on every call; it is never logged.
func Dial(addr, token string, useTLS bool) (*Remote, error) {
	creds := insecure.NewCredentials()
	if useTLS {
		creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	opts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	if token != "" {
		opts = append(opts, grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn,
			invoker grpc.UnaryInvoker, callOpts ...grpc.CallOption) error {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
			return invoker(ctx, method, req, reply, cc, callOpts...)
		}))
	}
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("cortexdb %s: %w", addr, err)
	}
	return &Remote{conn: conn, tool: rpcv1.NewToolsServiceClient(conn)}, nil
}

func (r *Remote) Close() error { return r.conn.Close() }

func (r *Remote) Call(ctx context.Context, tool string, args, out any) error {
	body, err := json.Marshal(args)
	if err != nil {
		return err
	}
	resp, err := r.tool.CallTool(ctx, &rpcv1.CallToolRequest{Name: tool, ArgsJson: string(body)})
	if err != nil {
		// The server answers an unknown tool with NotFound too; that one is a
		// real error, not "already gone".
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound && !strings.Contains(strings.ToLower(st.Message()), "unknown tool") {
			return fmt.Errorf("%s: %w", tool, ErrNotFound)
		}
		return fmt.Errorf("%s: %w", tool, err)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(resp.GetResultJson()), out)
}

// Local is a Brain on an open CortexDB, the same toolbox the gRPC server runs
// its tools on. Tests use it with a temporary database.
func Local(db *cortexdb.DB) Brain { return local{db} }

type local struct{ db *cortexdb.DB }

func (l local) Call(ctx context.Context, tool string, args, out any) error {
	body, err := json.Marshal(args)
	if err != nil {
		return err
	}
	res, err := l.db.GraphRAGTools().Call(ctx, tool, body)
	if errors.Is(err, core.ErrNotFound) {
		return fmt.Errorf("%s: %w", tool, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	if out == nil {
		return nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// errCode is what a log line may say about a failed call: the gRPC code, not
// the message, which can quote the item it was about.
func errCode(err error) string {
	if errors.Is(err, ErrNotFound) {
		return "NotFound"
	}
	if st, ok := status.FromError(err); ok {
		return st.Code().String()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "DeadlineExceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "Canceled"
	}
	return "error"
}
