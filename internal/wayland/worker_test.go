package wayland

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"github.com/Nomadcxx/sysc-wayland/client"
	"golang.org/x/sys/unix"
)

func newTestWaylandContext(t *testing.T) (*client.Context, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayland.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	display, err := client.Connect(path)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	compositor, err := listener.AcceptUnix()
	if err != nil {
		_ = display.Context().Close()
		_ = listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = display.Context().Close()
		_ = compositor.Close()
		_ = listener.Close()
	})
	return display.Context(), compositor
}

func TestOnFrameDoesNotCommitBetweenTicks(t *testing.T) {
	ctx, compositor := newTestWaylandContext(t)
	surface := client.NewSurface(ctx)
	lastTick := time.Unix(1000, 0)
	o := &Owner{ctx: ctx, surface: surface, fps: 20, lastTick: lastTick}

	o.onFrame(lastTick.Add(time.Second / 60))

	var readable int
	rawConn, err := compositor.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := rawConn.Control(func(fd uintptr) {
		readable, err = unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if readable != 0 {
		t.Fatal("owner sent Wayland requests before the next effect tick")
	}
}

func TestWaitAndDispatchSubmitsAtTickDeadline(t *testing.T) {
	for _, tc := range []struct {
		name           string
		framePending   bool
		wantTickSubmit bool
	}{
		{name: "deadline with no pending frame callback", wantTickSubmit: true},
		{name: "wait for a throttled frame callback", framePending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := newTestWaylandContext(t)
			wake := [2]int{0, 0}
			if err := unix.Pipe2(wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = unix.Close(wake[0])
				_ = unix.Close(wake[1])
			})
			w := &effectWorker{
				requests: make(chan workerRequest, 1),
				frames:   make(chan workerResult, 1),
				acks:     make(chan workerAck, 1),
				stop:     make(chan struct{}),
			}
			lastTick := time.Now().Add(-TargetFrameInterval + 20*time.Millisecond)
			o := &Owner{
				ctx: ctx, worker: w, wakeRead: wake[0], wakeWrite: wake[1],
				ready: true, fps: 20, lastTick: lastTick,
			}
			if tc.framePending {
				o.frameCallback = client.NewCallback(ctx)
			}
			fallback := time.AfterFunc(80*time.Millisecond, func() {
				_, _ = unix.Write(wake[1], []byte{1})
			})
			defer fallback.Stop()

			started := time.Now()
			if err := o.waitAndDispatch(time.Time{}); err != nil {
				t.Fatal(err)
			}
			if tc.framePending && time.Since(started) < 70*time.Millisecond {
				t.Fatal("owner polled at the tick deadline while a compositor frame callback was pending")
			}
			select {
			case req := <-w.requests:
				if req.op != workerTick {
					t.Fatalf("request op = %v, want workerTick", req.op)
				}
				if !tc.wantTickSubmit {
					t.Fatal("owner submitted a tick before the throttled frame callback arrived")
				}
			default:
				if tc.wantTickSubmit {
					t.Fatal("poll timeout did not submit the due effect tick")
				}
			}
		})
	}
}

func TestDeferredTickRetriesAfterWorkerWake(t *testing.T) {
	ctx, _ := newTestWaylandContext(t)
	wake := [2]int{0, 0}
	if err := unix.Pipe2(wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = unix.Close(wake[0])
		_ = unix.Close(wake[1])
	})
	worker := &effectWorker{
		requests: make(chan workerRequest, 1),
		frames:   make(chan workerResult, 1),
		acks:     make(chan workerAck, 1),
		stop:     make(chan struct{}),
	}
	reply := make(chan workerAck, 1)
	worker.requests <- workerRequest{op: workerReset, reply: reply}
	o := &Owner{ctx: ctx, worker: worker, wakeRead: wake[0], wakeWrite: wake[1], ready: true, fps: 20}
	o.onFrame(time.Now())
	if !o.tickDeferred {
		t.Fatal("full worker queue did not defer the due tick")
	}

	<-worker.requests // the worker consumed its previous request
	worker.acks <- workerAck{op: workerReset, reply: reply}
	if _, err := unix.Write(wake[1], []byte{1}); err != nil {
		t.Fatal(err)
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	loopDone := make(chan error, 1)
	go func() { loopDone <- o.loop(loopCtx, nil) }()
	loopStopped := false
	defer func() {
		if !loopStopped {
			cancel()
			_, _ = unix.Write(wake[1], []byte{1})
			select {
			case err := <-loopDone:
				if err != nil {
					t.Errorf("owner loop: %v", err)
				}
			case <-time.After(time.Second):
				t.Error("owner loop did not stop after cancellation")
			}
		}
	}()
	select {
	case req := <-worker.requests:
		if req.op != workerTick {
			t.Fatalf("retried request op = %v, want workerTick", req.op)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not retry the deferred tick after the worker wake")
	}
	cancel()
	if _, err := unix.Write(wake[1], []byte{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatal(err)
		}
		loopStopped = true
	case <-time.After(time.Second):
		t.Fatal("owner loop did not stop after cancellation")
	}
	if !o.tickPending || o.tickDeferred {
		t.Fatalf("tick pending=%t deferred=%t after retry, want pending only", o.tickPending, o.tickDeferred)
	}
}

func TestArtworkElapsedWorker(t *testing.T) {
	e, err := effect.New("great-wave", "kanagawa", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	w := newEffectWorker(e, "great-wave", "kanagawa", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)
	if !w.submit(workerRequest{op: workerTick, elapsed: 120 * time.Millisecond}) {
		t.Fatal("tick rejected")
	}
	_ = receiveAck(t, w.acks)
	got := receiveFrame(t, w.frames)
	want, _ := effect.New("great-wave", "kanagawa", 80, 24, "")
	want.Tick()
	want.TickElapsed(120 * time.Millisecond)
	if !reflect.DeepEqual(got.grid, want.Grid()) {
		t.Fatal("worker dropped elapsed time")
	}
}

func TestArtworkElapsedOwnerPause(t *testing.T) {
	w := &effectWorker{requests: make(chan workerRequest, 2), frames: make(chan workerResult, 1), acks: make(chan workerAck, 2), stop: make(chan struct{})}
	baseline := time.Unix(1000, 0)
	o := &Owner{worker: w, lastTick: baseline, pending: make(map[chan workerAck]chan ControlReply)}
	for _, paused := range []bool{true, false} {
		reply := make(chan workerAck, 1)
		o.pending[reply] = nil
		w.acks <- workerAck{op: workerPause, paused: paused, reply: reply}
		if err := o.drainWorker(); err != nil {
			t.Fatal(err)
		}
		if !o.lastTick.IsZero() {
			t.Fatal("pause transition retained elapsed-time baseline")
		}
	}
	o.onFrame(baseline.Add(time.Hour))
	select {
	case req := <-w.requests:
		if req.elapsed != 50*time.Millisecond {
			t.Fatalf("resumed tick includes paused time: %s", req.elapsed)
		}
	default:
		t.Fatal("resume did not request a tick")
	}
}

func TestArtworkWorkerRetainsCellAspect(t *testing.T) {
	e, err := effect.New("great-wave", "kanagawa", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	w := newEffectWorker(e, "great-wave", "kanagawa", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)
	for _, req := range []workerRequest{
		{op: workerResize, cols: 100, rows: 30, cellW: 7, cellH: 16},
		{op: workerChange, id: "wasteland", theme: "dracula"},
		{op: workerReset},
	} {
		req.reply = make(chan workerAck, 1)
		if !w.submit(req) {
			t.Fatal("request rejected")
		}
		ack := receiveAck(t, w.acks)
		if ack.err != nil {
			t.Fatal(ack.err)
		}
		got := receiveFrame(t, w.frames)
		want, err := effect.New(got.id, got.theme, 100, 30, "")
		if err != nil {
			t.Fatal(err)
		}
		want.SetCellAspect(7, 16)
		if err = want.Tick(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.grid, want.Grid()) {
			t.Fatalf("operation %d lost cell aspect", req.op)
		}
	}
}

func TestEffectWorkerTicksAndHonorsPause(t *testing.T) {
	e, err := effect.New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new effect: %v", err)
	}
	w := newEffectWorker(e, "fire", "nord", func() {})
	defer w.close()

	first := receiveFrame(t, w.frames)
	if first.grid == nil || first.generation != 1 {
		t.Fatalf("initial frame = %+v", first)
	}

	pausedReply := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerPause, paused: true, reply: pausedReply}) {
		t.Fatal("pause request was rejected")
	}
	paused := receiveAck(t, w.acks)
	if !paused.paused || paused.reply != pausedReply {
		t.Fatalf("pause result = %+v", paused)
	}

	tickReply := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerTick, reply: tickReply}) {
		t.Fatal("tick request was rejected")
	}
	if tick := receiveAck(t, w.acks); tick.err != nil || !tick.paused {
		t.Fatalf("paused tick result = %+v", tick)
	}
	select {
	case frame := <-w.frames:
		t.Fatalf("paused worker published a frame: %+v", frame)
	default:
	}
}

func TestEffectWorkerChangeAndResize(t *testing.T) {
	e, err := effect.New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new effect: %v", err)
	}
	w := newEffectWorker(e, "fire", "nord", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)

	change := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerChange, id: "rain", theme: "dracula", reply: change}) {
		t.Fatal("change request was rejected")
	}
	ack := receiveAck(t, w.acks)
	if ack.err != nil || ack.id != "rain" || ack.theme != "dracula" {
		t.Fatalf("change result = %+v", ack)
	}
	frame := receiveFrame(t, w.frames)
	if frame.id != "rain" || frame.theme != "dracula" || frame.grid == nil {
		t.Fatalf("changed frame = %+v", frame)
	}

	resize := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerResize, cols: 100, rows: 30, reply: resize}) {
		t.Fatal("resize request was rejected")
	}
	ack = receiveAck(t, w.acks)
	if ack.err != nil {
		t.Fatalf("resize result = %+v", ack)
	}
	frame = receiveFrame(t, w.frames)
	if frame.grid == nil || frame.grid.Cols != 100 || frame.grid.Rows != 30 {
		t.Fatalf("resized frame grid = %+v", frame.grid)
	}
}

func TestEffectWorkerPreservesANSILoveBackendOnChange(t *testing.T) {
	e, err := effect.NewWithOptions("ansi-love", "nord", 80, 24, "", effect.Options{ANSILoveBackend: "quad"})
	if err != nil {
		t.Fatal(err)
	}
	w := newEffectWorker(e, "ansi-love", "nord", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)

	change := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerChange, id: "ansi-love", theme: "nord", reply: change}) {
		t.Fatal("change request was rejected")
	}
	if ack := receiveAck(t, w.acks); ack.err != nil {
		t.Fatalf("change result = %+v", ack)
	}
	frame := receiveFrame(t, w.frames)
	for y := 0; y < frame.grid.Rows; y++ {
		for x := 0; x < frame.grid.Cols; x++ {
			if c := frame.grid.At(x, y); c.Ch >= 0x1FB00 && c.Ch <= 0x1FB3B {
				t.Fatal("effect change discarded the configured Quad backend")
			}
		}
	}
}

func TestEffectWorkerResizeWhilePausedPublishesFrame(t *testing.T) {
	e, err := effect.New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new effect: %v", err)
	}
	w := newEffectWorker(e, "fire", "nord", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)

	pausedReply := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerPause, paused: true, reply: pausedReply}) {
		t.Fatal("pause request was rejected")
	}
	if paused := receiveAck(t, w.acks); paused.err != nil {
		t.Fatalf("pause result = %+v", paused)
	}

	resize := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerResize, cols: 100, rows: 30, reply: resize}) {
		t.Fatal("resize request was rejected")
	}
	if ack := receiveAck(t, w.acks); ack.err != nil || !ack.paused {
		t.Fatalf("paused resize result = %+v", ack)
	}
	frame := receiveFrame(t, w.frames)
	if frame.grid == nil || frame.grid.Cols != 100 || frame.grid.Rows != 30 {
		t.Fatalf("paused resized frame grid = %+v", frame.grid)
	}

	tickReply := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerTick, reply: tickReply}) {
		t.Fatal("tick request was rejected")
	}
	if tick := receiveAck(t, w.acks); tick.err != nil {
		t.Fatalf("paused tick result = %+v", tick)
	}
	select {
	case frame := <-w.frames:
		t.Fatalf("paused worker published a second frame: %+v", frame)
	default:
	}
}

func TestOwnerWakeDoesNotWriteToReusedDescriptorAfterFinish(t *testing.T) {
	for round := 0; round < 10000; round++ {
		wake := [2]int{}
		if err := unix.Pipe2(wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
			t.Fatal(err)
		}
		o := &Owner{done: make(chan struct{}), wakeRead: wake[0], wakeWrite: wake[1]}
		start, stop := make(chan struct{}), make(chan struct{})
		var writers sync.WaitGroup
		for i := 0; i < 8; i++ {
			writers.Add(1)
			go func() {
				defer writers.Done()
				<-start
				for {
					select {
					case <-stop:
						return
					default:
						o.Wake()
						runtime.Gosched()
					}
				}
			}()
		}
		close(start)
		for i := 0; i < 4; i++ {
			runtime.Gosched()
		}
		o.finish()
		reused := [2]int{}
		if err := unix.Pipe2(reused[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			runtime.Gosched()
		}
		close(stop)
		writers.Wait()
		var b [8]byte
		n, err := unix.Read(reused[0], b[:])
		_ = unix.Close(reused[0])
		_ = unix.Close(reused[1])
		if n > 0 || (err != nil && !errors.Is(err, unix.EAGAIN)) {
			t.Fatalf("round %d: stale wake wrote %d bytes to a reused descriptor (err %v)", round, n, err)
		}
	}
}

func receiveFrame(t *testing.T, frames <-chan workerResult) workerResult {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for effect worker")
		return workerResult{}
	}
}

func receiveAck(t *testing.T, acks <-chan workerAck) workerAck {
	t.Helper()
	select {
	case ack := <-acks:
		return ack
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for effect worker command")
		return workerAck{}
	}
}
