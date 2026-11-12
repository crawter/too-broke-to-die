// Copyright (C) 2026 crawter
// All rights reserved.

package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"too-broke-to-die/internal/tbtd/platform"
	"too-broke-to-die/internal/tbtd/session"
)

// Store owns the profile lock and one serialized worker. Call Run under the
// application supervisor, and Close before releasing any related resources.
// Poll consumes a completed result, allowing the original request ID to be
// reused for its retry, resolution, or confirmed discard.
type Store struct {
	mu              sync.Mutex
	files           revisionFiles
	root            *platform.Root
	listing         *os.Root
	lock            *platform.ProfileLock
	codec           *Codec
	queue           chan work
	done            chan struct{}
	capacity        time.Duration
	started         bool
	stopping        bool
	finished        bool
	drain           context.Context
	cancelDrain     context.CancelFunc
	closeErr        error
	drainErr        error
	progress        map[session.RequestID]session.ProgressResult
	settings        map[session.RequestID]session.SettingsResult
	ids             map[session.RequestID]bool
	revisions       map[session.CampaignID]*Revisions
	pending         *session.ProgressInput
	pendingResolved bool
	selectorPending *pendingSelector
}

type work struct {
	ctx      context.Context
	progress *session.ProgressRequest
	settings *session.SettingsRequest
	discard  bool
}

// Open acquires the exclusive profile lock but starts no worker. path is the
// trusted profile directory; all subsequent file access is root-confined.
// The finite budget bounds each operation and the entire shutdown drain.
func Open(ctx context.Context, path string, codec *Codec, queueSize int, budget time.Duration) (_ *Store, err error) {
	defer mon.Task()(&ctx)(&err)
	if codec == nil || queueSize < 1 || budget <= 0 {
		return nil, fmt.Errorf("invalid store configuration")
	}
	root, err := platform.OpenRoot(ctx, path)
	if err != nil {
		return nil, err
	}
	lock, err := root.LockProfile(ctx)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	listing, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.Join(err, lock.Close(), root.Close())
	}
	return &Store{files: root, root: root, lock: lock, listing: listing, codec: codec,
		queue: make(chan work, queueSize), done: make(chan struct{}), capacity: budget,
		progress: make(map[session.RequestID]session.ProgressResult), settings: make(map[session.RequestID]session.SettingsResult), ids: make(map[session.RequestID]bool), revisions: make(map[session.CampaignID]*Revisions)}, nil
}

// Submit transfers a progress request to the worker without waiting for I/O.
// CreateCampaign requires the contract's syntactically valid Campaign field,
// but the store generates the new identity and returns it in the result; it
// never initializes or selects the identity supplied in that field.
func (store *Store) Submit(ctx context.Context, request session.ProgressRequest) (err error) {
	defer mon.Task()(&ctx)(&err)
	input := request.Data()
	return store.submit(ctx, input.ID, work{ctx: ctx, progress: &request})
}

// DiscardProgress submits a confirmed discard of the original resolved request.
// It rechecks both slots and reports an installed result instead of discarding it.
// The request must be the original commit's Resolution. Poll the previous
// result before reusing its request ID.
func (store *Store) DiscardProgress(ctx context.Context, request session.ProgressRequest) (err error) {
	defer mon.Task()(&ctx)(&err)
	input := request.Data()
	if input.Kind != session.ResolveProgress {
		return session.InvalidAction
	}
	return store.submit(ctx, input.ID, work{ctx: ctx, progress: &request, discard: true})
}

func (store *Store) submit(ctx context.Context, id session.RequestID, item work) (err error) {
	defer mon.Task()(&ctx)(&err)
	if err = ctx.Err(); err != nil {
		return err
	}
	if id == "" {
		return session.InvalidAction
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.stopping || store.finished {
		return session.ErrStoreClosed
	}
	if _, exists := store.ids[id]; exists {
		return session.StorageConflict
	}
	select {
	case store.queue <- item:
		store.ids[id] = item.progress != nil
		if item.progress != nil {
			input := item.progress.Data()
			store.progress[id] = session.NewProgressResult(ctx, session.ProgressResultData{ID: id, Campaign: input.Campaign, Operation: input.Operation, Status: session.StoragePending})
		} else {
			store.settings[id] = session.NewSettingsResult(ctx, session.SettingsResultData{ID: id, Status: session.StoragePending})
		}
		return nil
	default:
		return session.ErrStoreBusy
	}
}

// Poll returns pending until the identified progress request has a result.
// A completed result is consumed; an unknown or already consumed ID fails.
func (store *Store) Poll(ctx context.Context, id session.RequestID) (_ session.ProgressResult, err error) {
	defer mon.Task()(&ctx)(&err)
	store.mu.Lock()
	defer store.mu.Unlock()
	if result, ok := store.progress[id]; ok {
		if result.Data(ctx).Status != session.StoragePending {
			delete(store.progress, id)
			delete(store.ids, id)
		}
		return result, nil
	}
	if isProgress, ok := store.ids[id]; !ok || !isProgress {
		return session.ProgressResult{}, session.ErrUnknownRequest
	}
	return session.NewProgressResult(ctx, session.ProgressResultData{ID: id, Status: session.StoragePending}), nil
}

// Settings exposes the separate typed settings facade on the same writer.
func (store *Store) Settings() *SettingsStore { return &SettingsStore{store: store} }

// SettingsStore is the settings view of the profile's serialized writer.
type SettingsStore struct{ store *Store }

// Submit transfers a settings request without waiting for disk I/O.
func (view *SettingsStore) Submit(ctx context.Context, request session.SettingsRequest) (err error) {
	defer mon.Task()(&ctx)(&err)
	input := request.Data(ctx)
	return view.store.submit(ctx, input.ID, work{ctx: ctx, settings: &request})
}

// Poll returns the typed settings outcome without blocking. It consumes a
// completed result, just like the progress view.
func (view *SettingsStore) Poll(ctx context.Context, id session.RequestID) (_ session.SettingsResult, err error) {
	defer mon.Task()(&ctx)(&err)
	view.store.mu.Lock()
	defer view.store.mu.Unlock()
	if result, ok := view.store.settings[id]; ok {
		if result.Data(ctx).Status != session.StoragePending {
			delete(view.store.settings, id)
			delete(view.store.ids, id)
		}
		return result, nil
	}
	if isProgress, ok := view.store.ids[id]; !ok || isProgress {
		return session.SettingsResult{}, session.ErrUnknownRequest
	}
	return session.NewSettingsResult(ctx, session.SettingsResultData{ID: id, Status: session.StoragePending}), nil
}

func (store *Store) stop() {
	ctx := context.Background()
	defer mon.Task()(&ctx)(nil)
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.stopping {
		store.stopping = true
		store.drain, store.cancelDrain = context.WithTimeout(context.Background(), store.capacity)
		close(store.queue)
	}
}

// Run executes the worker until shutdown or cancellation, draining accepted
// requests. A second Run is rejected; the supervisor must treat early exit as
// a failure. Accepted work retains initiating context values but not ordinary
// cancellation, and is bounded both individually and by the shutdown budget.
func (store *Store) Run(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	store.mu.Lock()
	if store.started || store.finished || store.stopping {
		store.mu.Unlock()
		return session.ErrStoreClosed
	}
	store.started = true
	store.mu.Unlock()
	return store.serve(ctx)
}

func (store *Store) serve(ctx context.Context) (err error) {
	defer mon.Task()(&ctx)(&err)
	stopOnCancel := context.AfterFunc(ctx, store.stop)
	defer stopOnCancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Join(err, fmt.Errorf("storage worker panic: %v", recovered))
		}
		store.stop()
		for item := range store.queue {
			store.failWork(item, false)
		}
		if store.pending != nil {
			err = errors.Join(err, fmt.Errorf("unresolved storage operation %s", store.pending.Operation.Event))
		}
		if store.selectorPending != nil {
			err = errors.Join(err, errors.New("unresolved selector installation"))
		}
		store.mu.Lock()
		store.closeErr = errors.Join(store.closeErr, err)
		store.mu.Unlock()
		err = store.finish()
	}()
	done := ctx.Done()
	for {
		select {
		case <-done:
			store.stop()
			done = nil
		case item, ok := <-store.queue:
			if !ok {
				return nil
			}
			store.execute(item)
		}
	}
}

func (store *Store) execute(item work) {
	taskCtx := item.ctx
	defer mon.Task()(&taskCtx)(nil)
	defer func() {
		if recovered := recover(); recovered != nil {
			if item.progress != nil {
				input := item.progress.Data()
				if input.Kind == session.CommitProgress || input.Kind == session.ResolveProgress {
					store.pending = &input
				}
			}
			store.failWork(item, true)
			panic(recovered)
		}
	}()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(taskCtx), store.capacity)
	defer cancel()
	store.mu.Lock()
	drain := store.drain
	store.mu.Unlock()
	var stop func() bool
	if drain != nil {
		if drain.Err() != nil {
			cancel()
		} else {
			stop = context.AfterFunc(drain, cancel)
			defer stop()
		}
	}
	if item.progress != nil {
		input := item.progress.Data()
		var result session.ProgressResultData
		if item.discard {
			result = store.discardProgress(ctx, input)
		} else {
			result = store.processProgress(ctx, input)
		}
		store.mu.Lock()
		store.progress[input.ID] = session.NewProgressResult(ctx, result)
		if store.stopping && result.Status != session.StorageSucceeded && result.Status != session.StorageNotInstalled {
			store.closeErr = errors.Join(store.closeErr, fmt.Errorf("draining request %s (%s): %s", input.ID, input.Operation.Event, result.Status))
		}
		store.mu.Unlock()
	} else {
		input := item.settings.Data(ctx)
		result := store.processSettings(ctx, input)
		store.mu.Lock()
		store.settings[input.ID] = session.NewSettingsResult(ctx, result)
		if store.stopping && result.Status != session.StorageSucceeded {
			store.closeErr = errors.Join(store.closeErr, fmt.Errorf("draining settings request %s: %s", input.ID, result.Status))
		}
		store.mu.Unlock()
	}
}

func (store *Store) failWork(item work, interrupted bool) {
	ctx := context.WithoutCancel(item.ctx)
	defer mon.Task()(&ctx)(nil)
	store.mu.Lock()
	defer store.mu.Unlock()
	if item.progress != nil {
		input := item.progress.Data()
		status := session.StorageFailed
		if interrupted && (input.Kind == session.CommitProgress || input.Kind == session.ResolveProgress || input.Kind == session.CreateCampaign || input.Kind == session.SelectCampaign) {
			status = session.StorageAmbiguous
		}
		store.progress[input.ID] = session.NewProgressResult(ctx, session.ProgressResultData{ID: input.ID, Campaign: input.Campaign, Operation: input.Operation, Status: status, Code: session.Internal})
	} else {
		input := item.settings.Data(ctx)
		store.settings[input.ID] = session.NewSettingsResult(ctx, session.SettingsResultData{ID: input.ID, Status: session.StorageFailed, Code: session.Internal})
	}
}

func (store *Store) finish() (err error) {
	ctx := context.Background()
	defer mon.Task()(&ctx)(&err)
	closeErr := errors.Join(store.listing.Close(), store.lock.Close(), store.root.Close())
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.drain != nil && errors.Is(store.drain.Err(), context.DeadlineExceeded) && store.drainErr == nil {
		store.drainErr = fmt.Errorf("storage drain: %w", store.drain.Err())
		store.closeErr = errors.Join(store.closeErr, store.drainErr)
	}
	store.closeErr = errors.Join(store.closeErr, closeErr)
	store.finished = true
	close(store.done)
	if store.cancelDrain != nil {
		store.cancelDrain()
	}
	return store.closeErr
}

// Close stops acceptance and joins the worker. Before Run, it starts an owned
// drain of queued requests. On deadline expiry resources remain owned until
// the worker exits; callers must not release borrowed resources prematurely.
func (store *Store) Close() (err error) {
	ctx := context.Background()
	defer mon.Task()(&ctx)(&err)
	store.stop()
	store.mu.Lock()
	if !store.started {
		store.started = true
		go func() { _ = store.serve(context.Background()) }()
	}
	drain := store.drain
	store.mu.Unlock()
	select {
	case <-store.done:
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.closeErr
	case <-drain.Done():
		store.mu.Lock()
		defer store.mu.Unlock()
		if store.finished {
			return store.closeErr
		}
		if store.drainErr == nil {
			store.drainErr = fmt.Errorf("storage drain: %w", drain.Err())
			store.closeErr = errors.Join(store.closeErr, store.drainErr)
		}
		return store.closeErr
	}
}

var _ session.ProgressStore = (*Store)(nil)
var _ session.SettingsStore = (*SettingsStore)(nil)
