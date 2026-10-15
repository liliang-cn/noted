package cortexsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/liliang-cn/cortexdb/v2/pkg/cortexdb"
	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrNotOptedIn: the user is not listed in [cortexdb] sync_users.
var ErrNotOptedIn = errors.New("user is not listed in [cortexdb] sync_users")

// Syncer sends opted-in users' items to a brain. Only users named in Users
// are ever read or sent.
type Syncer struct {
	Store *store.Store
	Brain Brain // may be nil for dry runs
	// Target names the brain in the ledger (endpoint and collection). A new
	// target starts from an empty ledger, so everything is sent again.
	Target   string
	Users    []string // user names
	Location *time.Location
	Lang     string
	Interval time.Duration // between runs of Run
	Timeout  time.Duration // per call; default 60s
	Log      *slog.Logger
}

// Options of one run.
type Options struct {
	DryRun bool // work out what would be sent, send nothing, record nothing
	Full   bool // send every item, not only those changed since they were sent
}

// Report says what a run did, by item key only.
type Report struct {
	User      string
	Upserts   []string // sent, or with DryRun to be sent
	Deletes   []string // removed, or with DryRun to be removed
	Unchanged int
	Failed    []Failure
	Stopped   bool // the brain could not be reached; the rest waits for the next run
}

// Failure is an item a run could not send or remove; the next run tries again.
type Failure struct {
	Key, Op string
	Err     error
}

// OptedIn reports whether a user name is in Users.
func (s *Syncer) OptedIn(name string) bool { return slices.Contains(s.Users, name) }

// SyncUser runs one sync for a user name.
func (s *Syncer) SyncUser(ctx context.Context, name string, opt Options) (Report, error) {
	rep := Report{User: name}
	if !s.OptedIn(name) {
		return rep, ErrNotOptedIn
	}
	if s.Brain == nil && !opt.DryRun {
		return rep, errors.New("no cortexdb connection")
	}
	userID, err := s.userID(ctx, name)
	if err != nil {
		return rep, err
	}
	now := time.Now()
	if !opt.DryRun {
		if err := s.Store.CortexBegin(ctx, userID, s.Target, now); err != nil {
			return rep, err
		}
	}
	ledger, err := s.Store.CortexLedger(ctx, userID, s.Target)
	if err != nil {
		return rep, err
	}
	loc := s.Location
	if loc == nil {
		loc = time.UTC
	}
	snap, err := Collect(ctx, s.Store, userID, name, loc, s.Lang, now)
	if err != nil {
		return rep, err
	}
	proj := Project(snap)
	ups, dels, unchanged := Diff(proj.Items, ledger, opt.Full)
	rep.Unchanged = unchanged
	if opt.DryRun {
		for _, it := range ups {
			rep.Upserts = append(rep.Upserts, it.Key)
		}
		rep.Deletes = dels
		return rep, nil
	}
	fail := func(k, op string, err error) bool {
		rep.Failed = append(rep.Failed, Failure{Key: k, Op: op, Err: err})
		rep.Stopped = unreachable(err) || ctx.Err() != nil
		return rep.Stopped
	}
	for _, k := range dels {
		if err := s.remove(ctx, name, k); err != nil {
			if fail(k, "delete", err) {
				return rep, nil
			}
			continue
		}
		if err := s.Store.CortexForget(ctx, userID, k); err != nil {
			return rep, err
		}
		rep.Deletes = append(rep.Deletes, k)
	}
	for _, it := range ups {
		if err := s.save(ctx, it, proj.Entities); err != nil {
			if fail(it.Key, "save", err) {
				return rep, nil
			}
			continue
		}
		if err := s.Store.CortexMark(ctx, userID, it.Key, it.Hash, time.Now()); err != nil {
			return rep, err
		}
		rep.Upserts = append(rep.Upserts, it.Key)
	}
	return rep, nil
}

// Diff splits a projection against the ledger (key -> hash as sent): the
// items to send, the keys to remove, and how many are unchanged. full sends
// every item.
func Diff(items []Item, ledger map[string]string, full bool) (ups []Item, dels []string, unchanged int) {
	have := make(map[string]bool, len(items))
	for _, it := range items {
		have[it.Key] = true
		if h, ok := ledger[it.Key]; ok && h == it.Hash && !full {
			unchanged++
			continue
		}
		ups = append(ups, it)
	}
	for k := range ledger {
		if !have[k] {
			dels = append(dels, k)
		}
	}
	sort.Strings(dels)
	return ups, dels, unchanged
}

func (s *Syncer) userID(ctx context.Context, name string) (string, error) {
	users, err := s.Store.ListUsers(ctx)
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u.Name == name {
			return u.ID, nil
		}
	}
	return "", fmt.Errorf("no user named %q", name)
}

func (s *Syncer) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	d := s.Timeout
	if d <= 0 {
		d = 60 * time.Second
	}
	return context.WithTimeout(ctx, d)
}

// save sends one item, with the entities its relations point at so the
// edges have both ends whatever order items arrive in.
func (s *Syncer) save(ctx context.Context, it Item, entities map[string]cortexdb.ToolEntityInput) error {
	req := it.Save
	req.Entities = slices.Clone(req.Entities)
	for _, t := range it.Targets {
		if e, ok := entities[t]; ok {
			req.Entities = append(req.Entities, e)
		}
	}
	cctx, cancel := s.callCtx(ctx)
	defer cancel()
	return s.Brain.Call(cctx, "knowledge_save", req, nil)
}

// remove takes an item out of the brain: its knowledge record (text, chunks
// and the relations it asserted), then its entity and every edge that still
// touches it.
func (s *Syncer) remove(ctx context.Context, user, key string) error {
	cctx, cancel := s.callCtx(ctx)
	defer cancel()
	err := s.Brain.Call(cctx, "knowledge_delete", cortexdb.KnowledgeDeleteRequest{KnowledgeID: KnowledgeID(user, key)}, nil)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.Brain.Call(cctx, "delete_entities", cortexdb.ToolDeleteEntitiesRequest{Names: []string{EntityID(user, key)}}, nil)
}

// unreachable reports an error that will hit every other call too.
func unreachable(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unavailable, codes.Unauthenticated, codes.PermissionDenied, codes.Canceled:
		return true
	}
	return false
}

// Run syncs every opted-in user now and then every Interval until ctx ends.
// A failed run is logged and tried again next time; it never stops the server.
func (s *Syncer) Run(ctx context.Context) {
	every := s.Interval
	if every <= 0 {
		every = 15 * time.Minute
	}
	t := time.NewTimer(20 * time.Second) // let the server finish starting
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, name := range s.Users {
			if ctx.Err() != nil {
				return
			}
			s.runUser(ctx, name, every)
		}
		t.Reset(every)
	}
}

func (s *Syncer) runUser(ctx context.Context, name string, budget time.Duration) {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	rctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	start := time.Now()
	rep, err := s.SyncUser(rctx, name, Options{})
	if err != nil {
		log.Warn("cortexdb sync failed", "user", name, "err", err)
		return
	}
	log.Info("cortexdb sync", "user", name, "sent", len(rep.Upserts), "deleted", len(rep.Deletes), "unchanged", rep.Unchanged,
		"failed", len(rep.Failed), "stopped", rep.Stopped, "took", time.Since(start).Round(time.Millisecond))
	for i, f := range rep.Failed {
		if i == 10 {
			log.Warn("cortexdb sync: more failures", "user", name, "count", len(rep.Failed)-i)
			break
		}
		// The code only: a message can quote the item.
		log.Warn("cortexdb sync item failed", "user", name, "item", f.Key, "op", f.Op, "code", errCode(f.Err))
	}
}
