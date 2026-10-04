package wayland

import (
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/effect"
)

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
