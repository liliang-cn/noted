package service

import (
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/export"
	"github.com/liliang-cn/noted/internal/store"
)

// Export hands a user their data as one zip.
type Export struct {
	pb.UnimplementedExportServiceServer
	Store *store.Store
}

type chunkWriter struct {
	srv   pb.ExportService_ExportServer
	first string
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		k := min(len(p), 256<<10)
		if err := w.srv.Send(&pb.ExportChunk{Filename: w.first, Data: p[:k]}); err != nil {
			return 0, err
		}
		w.first = ""
		p = p[k:]
	}
	return n, nil
}

func (s *Export) Export(_ *pb.ExportRequest, srv pb.ExportService_ExportServer) error {
	ctx := srv.Context()
	u, err := uid(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	name, err := s.Store.UserName(ctx, u)
	if err != nil {
		return toStatus(err)
	}
	w := &chunkWriter{srv: srv, first: "noted-" + now.Format("20060102") + ".zip"}
	return toStatus(export.Write(ctx, s.Store, u, name, now, w))
}
